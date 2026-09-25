package app

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mike-akdeniz/flowcore"
)

// agentPrefix marks an assignee this application dispatches automatically.
//
// It is this client's convention and nothing more. FlowCore stores
// "agent:diff-risk@v1" exactly as it stores "group:security" — an opaque string
// it compares for equality and never parses. Deciding that one of them means
// "a machine handles this" is a decision made here, in eleven characters.
const agentPrefix = "agent:"

// IsAgent reports whether an assignee is one this application will dispatch.
func IsAgent(assignee string) bool { return strings.HasPrefix(assignee, agentPrefix) }

// workItem is enough to find the work again. Deliberately not the visit itself:
// by the time a worker picks this up the run may have moved on, so the worker
// re-reads the state and checks that this visit is still the open one.
type workItem struct {
	SessionID        string
	SubjectReference string
	DefinitionID     uuid.UUID
	VisitID          uuid.UUID
}

// Dispatcher runs agent steps off the web request.
//
// This is decision 4's case-4 dispatch. The alternative — calling the model
// inline, inside the HTTP handler — would complete the whole loop before the
// response was written, and a reader could fairly say that is a function call
// chain rather than something needing a workflow engine.
//
// Here the request returns as soon as the run reaches an agent step. The run then
// sits in the database, open, assigned, with nothing attending it, until a worker
// gets to it. That pause is the thing a workflow engine exists to survive, and it
// is only visible because nothing is holding it open.
type Dispatcher struct {
	app     *App
	checker Checker
	logger  *slog.Logger
	work    chan workItem

	// queued guards against dispatching the same visit twice — once from the
	// response that opened it and once from the sweep below.
	mutex  sync.Mutex
	queued map[uuid.UUID]bool
}

func NewDispatcher(application *App, checker Checker, logger *slog.Logger) *Dispatcher {
	return &Dispatcher{
		app:     application,
		checker: checker,
		logger:  logger,
		work:    make(chan workItem, 64),
		queued:  make(map[uuid.UUID]bool),
	}
}

func (d *Dispatcher) Mode() string { return d.checker.Mode() }

// Start runs one worker and a periodic sweep.
//
// One worker, not a pool: the demonstration gains nothing from throughput, and a
// single consumer keeps the log readable.
func (d *Dispatcher) Start(ctx context.Context) {
	go d.consume(ctx)
	go d.sweep(ctx)
}

// Dispatch enqueues the run's current step if an agent owns it.
//
// Called with the state returned by Start and CompleteStep — which is the whole
// point of case 4. Whoever advanced the run is already holding the answer to "is
// the next step an agent's", so nothing has to poll to find out.
func (d *Dispatcher) Dispatch(session *Session, definitionID uuid.UUID, state flowcore.WorkflowState) {
	if state.CurrentStep == nil || !IsAgent(state.CurrentStep.AssigneeID) {
		return
	}

	d.enqueue(workItem{
		SessionID:        session.ID,
		SubjectReference: state.SubjectReference,
		DefinitionID:     definitionID,
		VisitID:          state.CurrentStep.VisitID,
	})
}

func (d *Dispatcher) enqueue(item workItem) {
	d.mutex.Lock()
	if d.queued[item.VisitID] {
		d.mutex.Unlock()

		return
	}

	d.queued[item.VisitID] = true
	d.mutex.Unlock()

	select {
	case d.work <- item:
	default:
		// A full queue means the worker is behind. Drop it rather than block a web
		// request; the sweep will find it again.
		d.forget(item.VisitID)
		d.logger.Warn("dispatch queue full", "visit", item.VisitID)
	}
}

func (d *Dispatcher) forget(visitID uuid.UUID) {
	d.mutex.Lock()
	delete(d.queued, visitID)
	d.mutex.Unlock()
}

func (d *Dispatcher) consume(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case item := <-d.work:
			d.run(ctx, item)
			d.forget(item.VisitID)
		}
	}
}

// sweep finds agent work nobody enqueued.
//
// The queue lives in memory, so a restart loses whatever was in it and those runs
// would sit open forever. This is the recovery path decision 43 described: the
// worklist as a sweeper rather than the dispatch mechanism, asking the same
// question a person's queue asks, with an agent's references instead of a
// person's.
func (d *Dispatcher) sweep(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.sweepOnce(ctx)
		}
	}
}

func (d *Dispatcher) sweepOnce(ctx context.Context) {
	assigned, err := d.app.Engine.ListAssignedSteps(ctx, d.app.AgentReferences())
	if err != nil {
		d.logger.Warn("sweep", "err", err)

		return
	}

	for _, step := range assigned {
		session, ok := d.app.Sessions.Get(sessionOf(step.SubjectReference))
		if !ok || !session.Owns(step.WorkflowDefinitionID) {
			continue
		}

		d.enqueue(workItem{
			SessionID:        session.ID,
			SubjectReference: step.SubjectReference,
			DefinitionID:     step.WorkflowDefinitionID,
			VisitID:          step.VisitID,
		})
	}
}

// sessionOf recovers the session id from a subject reference this application
// wrote. FlowCore stores the whole string and never looks inside it; knowing that
// the first segment is a session is knowledge that lives only here.
func sessionOf(subjectReference string) string {
	session, _, _ := strings.Cut(subjectReference, ":")

	return session
}

// run does one agent step: read where the work stands, assemble what the checker
// needs from both halves, decide, and record.
func (d *Dispatcher) run(ctx context.Context, item workItem) {
	session, ok := d.app.Sessions.Get(item.SessionID)
	if !ok {
		return
	}

	state, err := d.app.Engine.GetState(ctx, item.SubjectReference, item.DefinitionID)
	if err != nil {
		d.logger.Warn("agent step: reading state", "visit", item.VisitID, "err", err)

		return
	}

	// The run may have moved since this was queued — a person can complete an
	// agent's step, which is the override the library allows by never requiring
	// the completer to be the assignee. If so, there is nothing to do.
	if state.CurrentStep == nil || state.CurrentStep.VisitID != item.VisitID {
		d.logger.Info("agent step: already handled", "visit", item.VisitID)

		return
	}

	run, ok := session.RunFor(subjectOf(item.SubjectReference))
	if !ok {
		d.logger.Warn("agent step: no subject", "subject", item.SubjectReference)

		return
	}

	verdict, err := d.checker.Check(ctx, CheckRequest{
		Agent:    state.CurrentStep.AssigneeID,
		StepName: state.CurrentStep.Name,
		Subject:  run.Subject,
		Actions:  state.CurrentStep.Actions,
	})
	if err != nil {
		// The visit stays open, so the sweep will try again and a person can step
		// in and complete it by hand. A failed agent does not strand a run.
		d.logger.Warn("agent step: check failed", "visit", item.VisitID, "err", err)

		return
	}

	next, err := d.app.CompleteStep(ctx, Identity{Reference: state.CurrentStep.AssigneeID},
		CompleteRequest{
			VisitID:             item.VisitID,
			ActionID:            verdict.ActionID,
			Remark:              verdict.Remark,
			SubjectVersionToken: run.Subject.VersionToken(),
		})
	if err != nil {
		d.logger.Warn("agent step: completing", "visit", item.VisitID, "err", err)

		return
	}

	d.logger.Info("agent step completed",
		"step", state.CurrentStep.Name, "by", state.CurrentStep.AssigneeID, "mode", d.checker.Mode())

	// The next step may be another agent's, which is how two agent steps run back
	// to back without anything polling.
	d.Dispatch(session, item.DefinitionID, next)
}

// subjectOf strips the session prefix, leaving the subject's own reference:
// "s7f3a2:claim:C-1042" becomes "claim:C-1042".
//
// Knowing that the first segment is a session and the rest identifies a subject
// is knowledge that lives only here. FlowCore stores the whole string and never
// looks inside it.
func subjectOf(subjectReference string) string {
	_, subject, found := strings.Cut(subjectReference, ":")
	if !found {
		return subjectReference
	}

	return subject
}
