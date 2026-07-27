package flowcore

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// TestDuplicateNameMapping covers the three unique indexes (status, step, action)
// and confirms case-insensitivity and that the carried Name is the caller's
// original casing.
func TestDuplicateNameMapping(t *testing.T) {
	catalog := newCatalog(t)
	ctx := context.Background()
	definition, ids := twoStepDefinition("Dup")
	mustCreate(t, catalog, definition)

	// status: "in progress" already exists.
	_, err := catalog.AddStatus(ctx, ids.workflow, AddStatusParams{Name: "In Progress"})
	var duplicateErr *DuplicateNameError
	if !errors.As(err, &duplicateErr) {
		t.Fatalf("status dup: want *DuplicateNameError, got %v", err)
	}
	if duplicateErr.Entity != entityStatus || duplicateErr.Name != "In Progress" {
		t.Errorf("status dup = {%q,%q}, want {%q,%q}", duplicateErr.Entity, duplicateErr.Name, entityStatus, "In Progress")
	}

	// step: "manager review" already exists.
	if _, err := catalog.AddStep(ctx, ids.workflow, AddStepParams{Name: "Manager Review", StatusID: ids.status}); !errors.Is(err, ErrDuplicateName) {
		t.Errorf("step dup: want ErrDuplicateName, got %v", err)
	}

	// action: "approve" already exists on manager review.
	if _, err := catalog.AddAction(ctx, ids.managerStep, AddActionParams{Name: "Approve", TerminalStatusID: &ids.status}); !errors.Is(err, ErrDuplicateName) {
		t.Errorf("action dup: want ErrDuplicateName, got %v", err)
	}
}

// TestActionXORMapping covers ck_action_definition_terminal_xor from both illegal
// shapes: both set and neither set.
func TestActionXORMapping(t *testing.T) {
	catalog := newCatalog(t)
	ctx := context.Background()
	definition, ids := twoStepDefinition("XOR")
	mustCreate(t, catalog, definition)

	both := AddActionParams{Name: "both", NextStepID: &ids.directorStep, TerminalStatusID: &ids.status}
	if _, err := catalog.AddAction(ctx, ids.managerStep, both); !errors.Is(err, ErrInvalidAction) {
		t.Errorf("both set: want ErrInvalidAction, got %v", err)
	}
	neither := AddActionParams{Name: "neither"}
	if _, err := catalog.AddAction(ctx, ids.managerStep, neither); !errors.Is(err, ErrInvalidAction) {
		t.Errorf("neither set: want ErrInvalidAction, got %v", err)
	}
}

// TestCrossDefinitionMapping covers the write side of the three same-definition
// FKs: a step or action in one definition referencing rows in another.
func TestCrossDefinitionMapping(t *testing.T) {
	catalog := newCatalog(t)
	ctx := context.Background()
	definitionA, a := twoStepDefinition("A")
	mustCreate(t, catalog, definitionA)
	definitionB, b := twoStepDefinition("B")
	mustCreate(t, catalog, definitionB)

	// fk_step_definition_status: B's step using A's status.
	if _, err := catalog.AddStep(ctx, b.workflow, AddStepParams{Name: "x", StatusID: a.status}); !errors.Is(err, ErrCrossDefinition) {
		t.Errorf("cross-def step status: want ErrCrossDefinition, got %v", err)
	}
	// fk_action_definition_next_step: B's action routing to A's step.
	if _, err := catalog.AddAction(ctx, b.managerStep, AddActionParams{Name: "y", NextStepID: &a.directorStep}); !errors.Is(err, ErrCrossDefinition) {
		t.Errorf("cross-def next step: want ErrCrossDefinition, got %v", err)
	}
	// fk_action_definition_terminal_status: B's action ending in A's status.
	if _, err := catalog.AddAction(ctx, b.managerStep, AddActionParams{Name: "z", TerminalStatusID: &a.status}); !errors.Is(err, ErrCrossDefinition) {
		t.Errorf("cross-def terminal status: want ErrCrossDefinition, got %v", err)
	}
}

// TestReferencedDeleteMapping covers the delete side of the same FKs — the other
// half of the two-sided constraints — using a definition whose statuses have
// isolated roles so each FK can be provoked on its own.
func TestReferencedDeleteMapping(t *testing.T) {
	catalog := newCatalog(t)
	ctx := context.Background()

	stepStatusID := uuid.Must(uuid.NewV7())     // used by steps
	terminalStatusID := uuid.Must(uuid.NewV7()) // used only as a terminal status
	entryStepID := uuid.Must(uuid.NewV7())      // entry step, routes to secondStepID
	secondStepID := uuid.Must(uuid.NewV7())
	definitionID := uuid.Must(uuid.NewV7())

	definition := WorkflowDefinition{
		ID: definitionID, Name: "Referenced", InitialStepDefinitionID: &entryStepID,
		Statuses: []WorkflowStatusDefinition{{ID: stepStatusID, Name: "working"}, {ID: terminalStatusID, Name: "done"}},
		Steps: []StepDefinition{
			{ID: entryStepID, WorkflowStatusDefinitionID: stepStatusID, Name: "first", Actions: []ActionDefinition{
				{Name: "go", NextStepDefinitionID: &secondStepID},
				{Name: "finish", TerminalWorkflowStatusDefinitionID: &terminalStatusID},
			}},
			{ID: secondStepID, WorkflowStatusDefinitionID: stepStatusID, Name: "second"},
		},
	}
	mustCreate(t, catalog, definition)

	// Each blocked delete rolls back, so the tree stays intact between attempts.
	cases := []struct {
		name   string
		call   func() error
		entity string
		id     uuid.UUID
	}{
		{"delete step routed-to (fk_action_definition_next_step)", func() error { return catalog.DeleteStep(ctx, secondStepID) }, entityStep, secondStepID},
		{"delete entry step (fk_workflow_definition_initial_step)", func() error { return catalog.DeleteStep(ctx, entryStepID) }, entityStep, entryStepID},
		{"delete in-use step status (fk_step_definition_status)", func() error { return catalog.DeleteStatus(ctx, stepStatusID) }, entityStatus, stepStatusID},
		{"delete in-use terminal status (fk_action_definition_terminal_status)", func() error { return catalog.DeleteStatus(ctx, terminalStatusID) }, entityStatus, terminalStatusID},
	}
	for _, tc := range cases {
		err := tc.call()
		var referencedErr *ReferencedError
		if !errors.As(err, &referencedErr) {
			t.Errorf("%s: want *ReferencedError, got %v", tc.name, err)
			continue
		}
		if referencedErr.Entity != tc.entity || referencedErr.ID != tc.id {
			t.Errorf("%s: ReferencedError = {%q,%v}, want {%q,%v}", tc.name, referencedErr.Entity, referencedErr.ID, tc.entity, tc.id)
		}
	}

	// Sanity: nothing was actually deleted by the blocked attempts.
	if n := rowCount(t, "step_definition"); n != 2 {
		t.Errorf("step_definition rows = %d, want 2 (no blocked delete took effect)", n)
	}
}
