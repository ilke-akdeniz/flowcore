package flowcore

import (
	"time"

	"github.com/google/uuid"
)

// The instance-side read surface: what the Engine hands back about a running or
// finished workflow.
//
// These types are projections, not table mirrors, and that is the one thing to
// understand before reading them. The definition-side types map one-to-one onto
// rows because a definition's read shape is its row shape. A running workflow's
// is not: "where does this run stand" spans the workflow, its snapshot step, and
// that step's actions, and a visit record needs its step's name, which lives on
// another table. So each type here is assembled from a join and carries fields
// from several tables.
//
// Nothing here mirrors the snapshot tables (flowcore.step, flowcore.action) as
// such. A client never receives a raw snapshot row, because there is no question
// in the current API whose answer is one.
//
// Every field is frozen at the moment the run passed through it. A definition
// edited or deleted after the run started changes none of it.

// WorkflowState is where a run stands: the workflow's own state, plus the step it
// is waiting on and the choices available there.
//
// CurrentStep is nil exactly when the run has finished. That is deliberately a
// pointer rather than a value beside an "is complete" flag: a zero-valued step
// would let a caller read an empty name off a finished run and believe it, where a
// nil pointer cannot be read by accident.
//
// WorkflowStatusName is the client's own label, frozen at the transition that
// stamped it, and the library never interprets it. Whether a run is finished is
// answered by CompletedAt alone.
type WorkflowState struct {
	ID                  uuid.UUID
	Name                string
	SubjectReference    string
	SubjectVersionToken *string
	// WorkflowStatusDefinitionID is the definition status this label came from.
	// It survives a rename of that status, and keeps identifying it after the
	// definition row is gone, which is what makes it usable across runs.
	WorkflowStatusDefinitionID uuid.UUID
	WorkflowStatusName         string
	StartedAt                  time.Time
	// CompletedAt is nil while the run is open. It is the structural
	// open-or-finished marker; a status name is never used to decide that.
	CompletedAt *time.Time
	// CurrentStep is nil once the run is complete.
	CurrentStep *CurrentStep
}

// CurrentStep is the step a run is waiting on, and the actions that leave it.
type CurrentStep struct {
	// ID identifies the snapshot step, which is stable across every visit to it.
	ID uuid.UUID
	// VisitID identifies this particular visit, and is what Complete acts on.
	// It is not interchangeable with ID: a loop can bring a run back to the same
	// step, so the step id alone cannot distinguish this visit from an earlier
	// one, and passing a stale visit id is how a caller learns the run moved on.
	VisitID uuid.UUID
	Name    string
	// AssigneeID is the live assignee for this visit, seeded from the step's
	// frozen default when the run entered it. Opaque: nil means unassigned, and
	// the library never interprets a non-nil value or checks it against whoever
	// completes the step.
	AssigneeID *string
	EnteredAt  time.Time
	// Actions is the set of choices available here, frozen at start. Empty means
	// the run cannot advance — a dead end in the definition it started from.
	Actions []Action
}

// Action is a choice available on a step. It carries only what a caller needs to
// present the choice and then name it back to Complete; where the action leads is
// the Engine's business, not the caller's.
type Action struct {
	ID   uuid.UUID
	Name string
}

// StepVisit is one entry into a step: who was expected to act, when the run
// arrived, and — once it has happened — what they decided.
//
// A run has one visit per entry, so a step reached twice by a loop appears twice,
// and no visit is ever rewritten once closed. That is what makes the history
// answer "which revision did they approve, and who were they" for every decision
// in the run, rather than only the most recent one.
type StepVisit struct {
	ID uuid.UUID
	// StepID is the snapshot step; StepName is its name, frozen at start.
	StepID     uuid.UUID
	StepName   string
	AssigneeID *string
	EnteredAt  time.Time
	// Completion is nil while the visit is open, and set once. Grouping these
	// fields behind one pointer mirrors the schema, where completion time,
	// completer, and selected action are written together or not at all — so a
	// half-completed visit is unrepresentable here as well as in the database.
	Completion *Completion
}

// Completion is what happened when a visit was closed.
//
// By is not a pointer: a completed visit always records who completed it, which
// the schema enforces. The library records the identity and never decides whether
// that actor was permitted to act — group membership and authorization live in
// client code.
type Completion struct {
	At time.Time
	By string
	// ActionID and ActionName are the action that was selected, frozen at start.
	ActionID   uuid.UUID
	ActionName string
	// SubjectVersionToken is the subject revision this decision was made against,
	// as supplied by the caller. Nil when the client does not version its
	// subjects; the library never compares or interprets it.
	SubjectVersionToken *string
}
