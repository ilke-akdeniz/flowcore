package flowcore

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// startRun is the common opening of most tests here: author a definition, start a
// run on it, and hand back the state.
func startRun(t *testing.T, engine *Engine, catalog *Catalog, definition WorkflowDefinition, subject string) WorkflowState {
	t.Helper()
	mustCreate(t, catalog, definition)

	state, err := engine.Start(context.Background(), StartParams{
		WorkflowDefinitionID: definition.ID,
		SubjectReference:     subject,
		SubjectVersionToken:  ptr("v1"),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	return state
}

func TestStartOpensAtTheEntryStep(t *testing.T) {
	engine, catalog := newEngine(t)
	definition, _ := twoStepDefinition("expense approval")

	state := startRun(t, engine, catalog, definition, "doc-1")

	if state.CurrentStep == nil {
		t.Fatal("a new run must have a current step")
	}

	if state.CurrentStep.Name != "manager review" {
		t.Errorf("started at %q, want the entry step %q", state.CurrentStep.Name, "manager review")
	}

	if state.WorkflowStatusName != "in progress" || state.CompletedAt != nil {
		t.Errorf("status=%q completedAt=%v, want the entry step's status and an open run",
			state.WorkflowStatusName, state.CompletedAt)
	}

	if state.SubjectReference != "doc-1" || state.SubjectVersionToken == nil || *state.SubjectVersionToken != "v1" {
		t.Errorf("subject=%q token=%v, want doc-1/v1", state.SubjectReference, state.SubjectVersionToken)
	}

	if len(state.CurrentStep.Actions) != 2 {
		t.Errorf("entry step offers %d actions, want 2", len(state.CurrentStep.Actions))
	}
}

func TestStartRejects(t *testing.T) {
	engine, catalog := newEngine(t)
	ctx := context.Background()

	t.Run("unknown definition", func(t *testing.T) {
		_, err := engine.Start(ctx, StartParams{
			WorkflowDefinitionID: uuid.Must(uuid.NewV7()),
			SubjectReference:     "doc-x",
		})
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("got %v, want ErrNotFound", err)
		}
	})

	t.Run("definition with no entry step", func(t *testing.T) {
		definition, _ := twoStepDefinition("no entry step")
		created := mustCreate(t, catalog, definition)

		// Clear the entry step behind Catalog's back: its own API cannot produce
		// this state, but a definition row can hold it, and Start must refuse it.
		if _, err := testPool.Exec(ctx,
			`update flowcore.workflow_definition set initial_step_definition_id = null where id = $1`,
			created.ID); err != nil {
			t.Fatalf("clearing entry step: %v", err)
		}

		_, err := engine.Start(ctx, StartParams{WorkflowDefinitionID: created.ID, SubjectReference: "doc-2"})
		if !errors.Is(err, ErrDefinitionHasNoInitialStep) {
			t.Errorf("got %v, want ErrDefinitionHasNoInitialStep", err)
		}
	})
}

func TestStartOneActiveRunPerSubjectAndDefinition(t *testing.T) {
	engine, catalog := newEngine(t)
	ctx := context.Background()
	definition, _ := twoStepDefinition("expense approval")

	startRun(t, engine, catalog, definition, "doc-1")

	_, err := engine.Start(ctx, StartParams{WorkflowDefinitionID: definition.ID, SubjectReference: "doc-1"})
	if !errors.Is(err, ErrActiveWorkflowExists) {
		t.Fatalf("second run: got %v, want ErrActiveWorkflowExists", err)
	}

	var activeErr *ActiveWorkflowExistsError
	if !errors.As(err, &activeErr) || activeErr.SubjectReference != "doc-1" {
		t.Errorf("errors.As did not recover the subject from %v", err)
	}

	// A different subject is unaffected, and so is a different definition.
	if _, err := engine.Start(ctx, StartParams{WorkflowDefinitionID: definition.ID, SubjectReference: "doc-2"}); err != nil {
		t.Errorf("a different subject must be allowed: %v", err)
	}
}

func TestStartAllowedAgainOnceTheRunFinishes(t *testing.T) {
	engine, catalog := newEngine(t)
	ctx := context.Background()
	definition, _ := twoStepDefinition("expense approval")

	state := startRun(t, engine, catalog, definition, "doc-1")

	// "reject" on the entry step is terminal, so this closes the run outright.
	state, err := engine.CompleteStep(ctx, CompleteParams{
		VisitID:     state.CurrentStep.VisitID,
		ActionID:    actionNamed(t, state, "reject"),
		CompletedBy: "user:mike",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if state.CompletedAt == nil {
		t.Fatal("a terminal action must close the run")
	}

	if _, err := engine.Start(ctx, StartParams{WorkflowDefinitionID: definition.ID, SubjectReference: "doc-1"}); err != nil {
		t.Errorf("restart after completion must be allowed: %v", err)
	}
}

func TestStartWritesTheWholeSnapshotWithProvenance(t *testing.T) {
	engine, catalog := newEngine(t)
	ctx := context.Background()
	definition, ids := twoStepDefinition("expense approval")

	state := startRun(t, engine, catalog, definition, "doc-1")

	// Raw SQL on purpose. The provenance ids are read by nothing until the
	// worklist slice, and they cannot be reconstructed later — a run that
	// recorded the wrong id is wrong permanently — so they are asserted here
	// rather than left until something consumes them.
	var stepCount, actionCount int
	if err := testPool.QueryRow(ctx,
		`select (select count(*) from flowcore.step where workflow_id = $1),
		        (select count(*) from flowcore.action where workflow_id = $1)`,
		state.ID).Scan(&stepCount, &actionCount); err != nil {
		t.Fatalf("counting snapshot rows: %v", err)
	}

	if stepCount != 2 || actionCount != 3 {
		t.Errorf("snapshot has %d steps and %d actions, want 2 and 3", stepCount, actionCount)
	}

	var definitionID uuid.UUID
	if err := testPool.QueryRow(ctx,
		`select workflow_definition_id from flowcore.workflow where id = $1`, state.ID).Scan(&definitionID); err != nil {
		t.Fatalf("reading workflow provenance: %v", err)
	}

	if definitionID != definition.ID {
		t.Errorf("workflow records definition %s, want %s", definitionID, definition.ID)
	}

	var stepDefinitionID, statusDefinitionID uuid.UUID
	var statusName string
	if err := testPool.QueryRow(ctx,
		`select step_definition_id, workflow_status_definition_id, workflow_status_name
		 from flowcore.step where workflow_id = $1 and name = 'manager review'`,
		state.ID).Scan(&stepDefinitionID, &statusDefinitionID, &statusName); err != nil {
		t.Fatalf("reading step provenance: %v", err)
	}

	if stepDefinitionID != ids.managerStep {
		t.Errorf("snapshot step records definition step %s, want %s", stepDefinitionID, ids.managerStep)
	}

	if statusDefinitionID != ids.status || statusName != "in progress" {
		t.Errorf("snapshot step status = %s/%q, want %s/%q",
			statusDefinitionID, statusName, ids.status, "in progress")
	}

	var nullProvenance int
	if err := testPool.QueryRow(ctx,
		`select count(*) from flowcore.action
		 where workflow_id = $1 and action_definition_id = '00000000-0000-0000-0000-000000000000'`,
		state.ID).Scan(&nullProvenance); err != nil {
		t.Fatalf("checking action provenance: %v", err)
	}

	if nullProvenance != 0 {
		t.Errorf("%d actions recorded a zero action_definition_id", nullProvenance)
	}
}

func TestCompleteRoutesToTheNextStep(t *testing.T) {
	engine, catalog := newEngine(t)
	definition, _ := twoStepDefinition("expense approval")

	state := startRun(t, engine, catalog, definition, "doc-1")
	firstVisit := state.CurrentStep.VisitID

	state, err := engine.CompleteStep(context.Background(), CompleteParams{
		VisitID:             firstVisit,
		ActionID:            actionNamed(t, state, "approve"),
		CompletedBy:         "user:mike",
		SubjectVersionToken: ptr("v2"),
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if state.CurrentStep == nil {
		t.Fatal("a routing action must leave the run on a step")
	}

	if state.CurrentStep.Name != "director review" {
		t.Errorf("routed to %q, want director review", state.CurrentStep.Name)
	}

	if state.CurrentStep.VisitID == firstVisit {
		t.Error("advancing must open a new visit, not reuse the closed one")
	}

	if state.CompletedAt != nil {
		t.Error("the run is still open after a routing action")
	}
}

func TestCompleteTerminalActionClosesTheRun(t *testing.T) {
	engine, catalog := newEngine(t)
	definition, ids := twoStepDefinition("expense approval")

	state := startRun(t, engine, catalog, definition, "doc-1")

	state, err := engine.CompleteStep(context.Background(), CompleteParams{
		VisitID:     state.CurrentStep.VisitID,
		ActionID:    actionNamed(t, state, "reject"),
		CompletedBy: "user:mike",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if state.CurrentStep != nil {
		t.Errorf("a finished run has no current step, got %q", state.CurrentStep.Name)
	}

	if state.CompletedAt == nil {
		t.Error("a terminal action must stamp completedAt")
	}

	// The terminating action ends in "rejected", which is neither the status the
	// run carried while in progress nor the other terminal status — so this
	// fails if the terminal status is stamped wrongly or not stamped at all.
	if state.WorkflowStatusName != "rejected" {
		t.Errorf("terminal status = %q, want %q from the terminating action",
			state.WorkflowStatusName, "rejected")
	}

	if state.WorkflowStatusDefinitionID != ids.rejectedStatus {
		t.Errorf("terminal status id = %s, want %s", state.WorkflowStatusDefinitionID, ids.rejectedStatus)
	}
}

func TestCompleteRejects(t *testing.T) {
	engine, catalog := newEngine(t)
	ctx := context.Background()
	definition, _ := twoStepDefinition("expense approval")

	state := startRun(t, engine, catalog, definition, "doc-1")
	visitID := state.CurrentStep.VisitID
	approve := actionNamed(t, state, "approve")

	t.Run("action belonging to another step", func(t *testing.T) {
		// The director step's action is not available while the run sits on the
		// manager step.
		var foreignAction uuid.UUID
		if err := testPool.QueryRow(ctx,
			`select a.id from flowcore.action a
			 join flowcore.step s on s.id = a.step_id
			 where s.workflow_id = $1 and s.name = 'director review'`,
			state.ID).Scan(&foreignAction); err != nil {
			t.Fatalf("finding a foreign action: %v", err)
		}

		_, err := engine.CompleteStep(ctx, CompleteParams{
			VisitID: visitID, ActionID: foreignAction, CompletedBy: "user:mike",
		})
		if !errors.Is(err, ErrActionNotAvailable) {
			t.Errorf("got %v, want ErrActionNotAvailable", err)
		}
	})

	t.Run("unknown visit id is not-found, not stale", func(t *testing.T) {
		_, err := engine.CompleteStep(ctx, CompleteParams{
			VisitID: uuid.Must(uuid.NewV7()), ActionID: approve, CompletedBy: "user:mike",
		})
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("got %v, want ErrNotFound", err)
		}

		if errors.Is(err, ErrVisitNotOpen) {
			t.Error("an unknown visit must not be reported as already completed")
		}
	})

	t.Run("already completed visit is stale, not not-found", func(t *testing.T) {
		if _, err := engine.CompleteStep(ctx, CompleteParams{
			VisitID: visitID, ActionID: approve, CompletedBy: "user:mike",
		}); err != nil {
			t.Fatalf("first completion: %v", err)
		}

		_, err := engine.CompleteStep(ctx, CompleteParams{
			VisitID: visitID, ActionID: approve, CompletedBy: "user:other",
		})
		if !errors.Is(err, ErrVisitNotOpen) {
			t.Errorf("got %v, want ErrVisitNotOpen", err)
		}
	})
}

func TestCompleteStampsTheDecision(t *testing.T) {
	engine, catalog := newEngine(t)
	ctx := context.Background()
	definition, _ := twoStepDefinition("expense approval")

	state := startRun(t, engine, catalog, definition, "doc-1")
	if _, err := engine.CompleteStep(ctx, CompleteParams{
		VisitID:             state.CurrentStep.VisitID,
		ActionID:            actionNamed(t, state, "approve"),
		CompletedBy:         "user:mike",
		SubjectVersionToken: ptr("rev-77"),
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	history, err := engine.GetHistory(ctx, "doc-1", definition.ID)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}

	if len(history) != 2 {
		t.Fatalf("history has %d visits, want 2", len(history))
	}

	closed := history[0]
	if closed.Completion == nil {
		t.Fatal("the first visit should be closed")
	}

	if closed.Completion.By != "user:mike" {
		t.Errorf("completedBy = %q, want user:mike", closed.Completion.By)
	}

	if closed.Completion.ActionName != "approve" {
		t.Errorf("selected action = %q, want approve", closed.Completion.ActionName)
	}

	if closed.Completion.SubjectVersionToken == nil || *closed.Completion.SubjectVersionToken != "rev-77" {
		t.Errorf("token = %v, want rev-77", closed.Completion.SubjectVersionToken)
	}

	if history[1].Completion != nil {
		t.Error("the open visit must have no completion")
	}
}

func TestCompleteLoopRevisitsAStepAndKeepsBothVisits(t *testing.T) {
	engine, catalog := newEngine(t)
	ctx := context.Background()
	definition, _ := loopingDefinition("rework loop")

	state := startRun(t, engine, catalog, definition, "doc-1")

	// manager review -> approve -> director review
	state, err := engine.CompleteStep(ctx, CompleteParams{
		VisitID: state.CurrentStep.VisitID, ActionID: actionNamed(t, state, "approve"),
		CompletedBy: "user:mike", SubjectVersionToken: ptr("v1"),
	})
	if err != nil {
		t.Fatalf("first completion: %v", err)
	}

	// director review -> reject -> back to manager review
	state, err = engine.CompleteStep(ctx, CompleteParams{
		VisitID: state.CurrentStep.VisitID, ActionID: actionNamed(t, state, "reject"),
		CompletedBy: "user:dana", SubjectVersionToken: ptr("v2"),
	})
	if err != nil {
		t.Fatalf("second completion: %v", err)
	}

	if state.CurrentStep == nil || state.CurrentStep.Name != "manager review" {
		t.Fatalf("expected to be back on manager review, got %+v", state.CurrentStep)
	}

	history, err := engine.GetHistory(ctx, "doc-1", definition.ID)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}

	want := []string{"manager review", "director review", "manager review"}
	if len(history) != len(want) {
		t.Fatalf("history has %d visits, want %d", len(history), len(want))
	}

	for i, visit := range history {
		if visit.StepName != want[i] {
			t.Errorf("visit %d is %q, want %q", i, visit.StepName, want[i])
		}
	}

	// The first visit's record must survive the revisit untouched.
	if history[0].Completion == nil || history[0].Completion.By != "user:mike" {
		t.Errorf("the first visit lost its completion: %+v", history[0].Completion)
	}

	if history[0].ID == history[2].ID {
		t.Error("a revisit must be a new row, not the earlier visit reopened")
	}

	if history[2].Completion != nil {
		t.Error("the current visit must still be open")
	}
}

func TestCompleteSeedsTheAssigneeFromTheSnapshotDefault(t *testing.T) {
	engine, catalog := newEngine(t)
	ctx := context.Background()
	definition, _ := loopingDefinition("rework loop")

	state := startRun(t, engine, catalog, definition, "doc-1")
	if state.CurrentStep.AssigneeID == nil || *state.CurrentStep.AssigneeID != "group:manager" {
		t.Fatalf("entry visit assignee = %v, want group:manager", state.CurrentStep.AssigneeID)
	}

	state, err := engine.CompleteStep(ctx, CompleteParams{
		VisitID: state.CurrentStep.VisitID, ActionID: actionNamed(t, state, "approve"),
		CompletedBy: "user:mike",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if state.CurrentStep.AssigneeID == nil || *state.CurrentStep.AssigneeID != "group:director" {
		t.Errorf("next visit assignee = %v, want group:director", state.CurrentStep.AssigneeID)
	}
}

func TestGetState(t *testing.T) {
	engine, catalog := newEngine(t)
	ctx := context.Background()
	definition, _ := twoStepDefinition("expense approval")

	started := startRun(t, engine, catalog, definition, "doc-1")

	t.Run("open run", func(t *testing.T) {
		state, err := engine.GetState(ctx, "doc-1", definition.ID)
		if err != nil {
			t.Fatalf("GetState: %v", err)
		}

		if state.ID != started.ID {
			t.Errorf("returned run %s, want %s", state.ID, started.ID)
		}

		if state.CurrentStep == nil || state.CurrentStep.Name != "manager review" {
			t.Fatalf("current step = %+v, want manager review", state.CurrentStep)
		}

		if len(state.CurrentStep.Actions) != 2 {
			t.Errorf("actions = %+v, want 2", state.CurrentStep.Actions)
		}
	})

	t.Run("unknown subject", func(t *testing.T) {
		_, err := engine.GetState(ctx, "doc-nope", definition.ID)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("got %v, want ErrNotFound", err)
		}

		var notFound *WorkflowNotFoundError
		if !errors.As(err, &notFound) || notFound.SubjectReference != "doc-nope" {
			t.Errorf("errors.As did not recover the subject from %v", err)
		}
	})

	t.Run("finished run", func(t *testing.T) {
		if _, err := engine.CompleteStep(ctx, CompleteParams{
			VisitID: started.CurrentStep.VisitID, ActionID: actionNamed(t, started, "reject"),
			CompletedBy: "user:mike",
		}); err != nil {
			t.Fatalf("Complete: %v", err)
		}

		state, err := engine.GetState(ctx, "doc-1", definition.ID)
		if err != nil {
			t.Fatalf("GetState on a finished run: %v", err)
		}

		if state.CurrentStep != nil {
			t.Errorf("finished run has a current step: %+v", state.CurrentStep)
		}

		if state.CompletedAt == nil {
			t.Error("finished run has no completedAt")
		}
	})
}

// A {subject, definition} pair accumulates one row per run, because
// ux_workflow_active constrains only open rows. Matching on the pair alone would
// return whichever row came first — an old finished run rather than the live one.
func TestGetStateReturnsTheLiveRunNotAnOldOne(t *testing.T) {
	engine, catalog := newEngine(t)
	ctx := context.Background()
	definition, _ := twoStepDefinition("expense approval")

	first := startRun(t, engine, catalog, definition, "doc-1")
	if _, err := engine.CompleteStep(ctx, CompleteParams{
		VisitID: first.CurrentStep.VisitID, ActionID: actionNamed(t, first, "reject"),
		CompletedBy: "user:mike",
	}); err != nil {
		t.Fatalf("finishing the first run: %v", err)
	}

	second, err := engine.Start(ctx, StartParams{
		WorkflowDefinitionID: definition.ID, SubjectReference: "doc-1",
	})
	if err != nil {
		t.Fatalf("starting the second run: %v", err)
	}

	state, err := engine.GetState(ctx, "doc-1", definition.ID)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}

	if state.ID != second.ID {
		t.Errorf("GetState returned run %s, want the live run %s", state.ID, second.ID)
	}

	if state.CurrentStep == nil {
		t.Error("the live run is open and must have a current step")
	}

	history, err := engine.GetHistory(ctx, "doc-1", definition.ID)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}

	if len(history) != 1 {
		t.Errorf("history has %d visits, want only the live run's 1", len(history))
	}
}

// Principle 2: a run is a snapshot, so editing the definition it started from
// must not reach it. Nothing else in the suite covers this.
func TestRunIsUnaffectedByLaterDefinitionEdits(t *testing.T) {
	engine, catalog := newEngine(t)
	ctx := context.Background()
	definition, ids := twoStepDefinition("expense approval")

	state := startRun(t, engine, catalog, definition, "doc-1")

	// Rename the step the run is sitting on, and delete an action it is offering.
	stored, err := catalog.Get(ctx, definition.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	var managerStep StepDefinition
	for _, step := range stored.Steps {
		if step.ID == ids.managerStep {
			managerStep = step
		}
	}

	params := managerStep.ToUpdate()
	params.Name = "RENAMED AFTER START"
	if _, err := catalog.UpdateStep(ctx, ids.managerStep, params); err != nil {
		t.Fatalf("UpdateStep: %v", err)
	}

	for _, action := range managerStep.Actions {
		if action.Name == "reject" {
			if err := catalog.DeleteAction(ctx, action.ID); err != nil {
				t.Fatalf("DeleteAction: %v", err)
			}
		}
	}

	after, err := engine.GetState(ctx, "doc-1", definition.ID)
	if err != nil {
		t.Fatalf("GetState after edits: %v", err)
	}

	if after.CurrentStep.Name != "manager review" {
		t.Errorf("run shows %q; a definition rename must not reach a running instance", after.CurrentStep.Name)
	}

	if len(after.CurrentStep.Actions) != 2 {
		t.Errorf("run offers %d actions, want the 2 frozen at start", len(after.CurrentStep.Actions))
	}

	// And the frozen action still works, though the definition no longer has it.
	if _, err := engine.CompleteStep(ctx, CompleteParams{
		VisitID: state.CurrentStep.VisitID, ActionID: actionNamed(t, after, "reject"),
		CompletedBy: "user:mike",
	}); err != nil {
		t.Errorf("a snapshot action deleted from the definition must still complete: %v", err)
	}
}

// Two callers completing the same visit at once: one wins, the other is told its
// view is stale rather than silently overwriting the winner's decision.
func TestCompleteIsSafeUnderConcurrency(t *testing.T) {
	engine, catalog := newEngine(t)
	ctx := context.Background()
	definition, _ := twoStepDefinition("expense approval")

	state := startRun(t, engine, catalog, definition, "doc-1")
	visitID := state.CurrentStep.VisitID
	approve := actionNamed(t, state, "approve")

	var (
		waitGroup sync.WaitGroup
		mutex     sync.Mutex
		errs      []error
	)

	// A release barrier, so both calls are in flight together. Without it the
	// goroutines can run one after the other and the test silently degenerates
	// into the sequential double-completion case, which is already covered
	// elsewhere. Overlap still is not guaranteed on any single run — the
	// block-then-recheck behaviour underneath was established by probe — but the
	// assertion below holds either way.
	release := make(chan struct{})

	for _, actor := range []string{"user:mike", "user:dana"} {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-release

			_, err := engine.CompleteStep(ctx, CompleteParams{
				VisitID: visitID, ActionID: approve, CompletedBy: actor,
			})

			mutex.Lock()
			defer mutex.Unlock()
			errs = append(errs, err)
		}()
	}

	close(release)
	waitGroup.Wait()

	var winners, stale int
	for _, err := range errs {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, ErrVisitNotOpen):
			stale++
		default:
			t.Errorf("unexpected error from a concurrent completion: %v", err)
		}
	}

	if winners != 1 || stale != 1 {
		t.Errorf("got %d winners and %d stale, want exactly 1 of each", winners, stale)
	}

	history, err := engine.GetHistory(ctx, "doc-1", definition.ID)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}

	if len(history) != 2 {
		t.Errorf("history has %d visits, want 2 — the loser must not have advanced the run", len(history))
	}
}
