package flowcore

// Tests for the assignee dimension: finding the work that waits on someone, and
// moving it to someone else. Both are keyed on the open visit, which is what ties
// them together — a closed visit is in nobody's queue and can no longer be moved.

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// The worklist answers "what is waiting on me" across runs, for a set of opaque
// references the caller resolves.
func TestListAssignedSteps(t *testing.T) {
	engine, catalog := newEngine(t)
	ctx := context.Background()

	first, _ := twoStepDefinition("expense approval")
	startRun(t, engine, catalog, first, "expense:1")

	second, _ := twoStepDefinition("travel approval")
	state := startRun(t, engine, catalog, second, "travel:9")

	assigned, err := engine.ListAssignedSteps(ctx, []string{"group:manager"})
	if err != nil {
		t.Fatalf("ListAssignedSteps: %v", err)
	}

	if len(assigned) != 2 {
		t.Fatalf("got %d assigned steps, want 2 — the worklist spans runs", len(assigned))
	}

	for _, step := range assigned {
		if step.AssigneeID != "group:manager" {
			t.Errorf("assignee = %q, want group:manager", step.AssigneeID)
		}

		if step.StepName != "manager review" {
			t.Errorf("step = %q, want manager review", step.StepName)
		}

		if step.SubjectReference == "" || step.WorkflowName == "" {
			t.Errorf("row carries no subject or workflow name: %+v", step)
		}
	}

	// Completing one removes it from the queue and puts the next step in the
	// director's, which is the whole point of keying on the open set.
	if _, err := engine.CompleteStep(ctx, CompleteParams{
		VisitID:     state.CurrentStep.VisitID,
		ActionID:    actionNamed(t, state, "approve"),
		CompletedBy: "user:dana",
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	assigned, err = engine.ListAssignedSteps(ctx, []string{"group:manager"})
	if err != nil {
		t.Fatalf("ListAssignedSteps: %v", err)
	}

	if len(assigned) != 1 {
		t.Errorf("after completing one, got %d, want 1", len(assigned))
	}

	directors, err := engine.ListAssignedSteps(ctx, []string{"group:director"})
	if err != nil {
		t.Fatalf("ListAssignedSteps: %v", err)
	}

	if len(directors) != 1 {
		t.Errorf("director queue has %d, want 1", len(directors))
	}

	// Several references at once, which is how a user plus their groups is asked.
	both, err := engine.ListAssignedSteps(ctx, []string{"group:manager", "group:director"})
	if err != nil {
		t.Fatalf("ListAssignedSteps: %v", err)
	}

	if len(both) != 2 {
		t.Errorf("manager+director = %d, want 2", len(both))
	}

	// Asking about nobody is not asking for everything.
	none, err := engine.ListAssignedSteps(ctx, nil)
	if err != nil {
		t.Fatalf("ListAssignedSteps(nil): %v", err)
	}

	if len(none) != 0 {
		t.Errorf("no references returned %d rows, want 0", len(none))
	}
}

func TestReassign(t *testing.T) {
	engine, catalog := newEngine(t)
	definition, _ := twoStepDefinition("expense approval")
	ctx := context.Background()

	state := startRun(t, engine, catalog, definition, "expense:4471")
	visitID := state.CurrentStep.VisitID

	state, err := engine.Reassign(ctx, visitID, "user:priya")
	if err != nil {
		t.Fatalf("Reassign: %v", err)
	}

	if state.CurrentStep.AssigneeID != "user:priya" {
		t.Errorf("assignee = %q, want user:priya", state.CurrentStep.AssigneeID)
	}

	if state.CurrentStep.VisitID != visitID {
		t.Error("reassigning must not open a new visit")
	}

	// It moves between queues rather than copying into one.
	managers, err := engine.ListAssignedSteps(ctx, []string{"group:manager"})
	if err != nil {
		t.Fatalf("ListAssignedSteps: %v", err)
	}

	if len(managers) != 0 {
		t.Errorf("group:manager still has %d rows, want 0", len(managers))
	}

	priya, err := engine.ListAssignedSteps(ctx, []string{"user:priya"})
	if err != nil {
		t.Fatalf("ListAssignedSteps: %v", err)
	}

	if len(priya) != 1 {
		t.Fatalf("user:priya has %d rows, want 1", len(priya))
	}

	// An empty assignee is refused rather than quietly unassigning.
	if _, err := engine.Reassign(ctx, visitID, ""); !errors.Is(err, ErrInvalidIdentifier) {
		t.Errorf("empty assignee: want ErrInvalidIdentifier, got %v", err)
	}

	// An unknown visit is a caller bug, not a stale view.
	if _, err := engine.Reassign(ctx, uuid.Must(uuid.NewV7()), "user:sam"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown visit: want ErrNotFound, got %v", err)
	}
}

// A closed visit is never rewritten, so a past decision keeps the assignee it was
// made under. That is what makes "who was this assigned to when they approved it"
// answerable, and it is the reason reassignment is gated on the open visit.
func TestReassignRefusesAClosedVisit(t *testing.T) {
	engine, catalog := newEngine(t)
	definition, _ := twoStepDefinition("expense approval")
	ctx := context.Background()

	state := startRun(t, engine, catalog, definition, "expense:4471")
	closedVisit := state.CurrentStep.VisitID

	if _, err := engine.CompleteStep(ctx, CompleteParams{
		VisitID:     closedVisit,
		ActionID:    actionNamed(t, state, "approve"),
		CompletedBy: "user:dana",
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	var notOpen *VisitNotOpenError
	_, err := engine.Reassign(ctx, closedVisit, "user:priya")
	if !errors.As(err, &notOpen) {
		t.Fatalf("want *VisitNotOpenError, got %v", err)
	}

	history, err := engine.GetHistory(ctx, "expense:4471", definition.ID)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}

	if history[0].AssigneeID != "group:manager" {
		t.Errorf("closed visit's assignee = %q, want group:manager unchanged", history[0].AssigneeID)
	}
}
