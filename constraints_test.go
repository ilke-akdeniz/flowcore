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
	c := newCatalog(t)
	ctx := context.Background()
	def, ids := twoStepDef("Dup")
	mustCreate(t, c, def)

	// status: "in progress" already exists.
	_, err := c.AddStatus(ctx, ids.def, AddStatusParams{Name: "In Progress"})
	var dup *DuplicateNameError
	if !errors.As(err, &dup) {
		t.Fatalf("status dup: want *DuplicateNameError, got %v", err)
	}
	if dup.Entity != entityStatus || dup.Name != "In Progress" {
		t.Errorf("status dup = {%q,%q}, want {%q,%q}", dup.Entity, dup.Name, entityStatus, "In Progress")
	}

	// step: "manager review" already exists.
	if _, err := c.AddStep(ctx, ids.def, AddStepParams{Name: "Manager Review", StatusID: ids.status}); !errors.Is(err, ErrDuplicateName) {
		t.Errorf("step dup: want ErrDuplicateName, got %v", err)
	}

	// action: "approve" already exists on manager review.
	if _, err := c.AddAction(ctx, ids.mgr, AddActionParams{Name: "Approve", TerminalStatusID: &ids.status}); !errors.Is(err, ErrDuplicateName) {
		t.Errorf("action dup: want ErrDuplicateName, got %v", err)
	}
}

// TestActionXORMapping covers ck_action_definition_terminal_xor from both illegal
// shapes: both set and neither set.
func TestActionXORMapping(t *testing.T) {
	c := newCatalog(t)
	ctx := context.Background()
	def, ids := twoStepDef("XOR")
	mustCreate(t, c, def)

	both := AddActionParams{Name: "both", NextStepID: &ids.dir, TerminalStatusID: &ids.status}
	if _, err := c.AddAction(ctx, ids.mgr, both); !errors.Is(err, ErrInvalidAction) {
		t.Errorf("both set: want ErrInvalidAction, got %v", err)
	}
	neither := AddActionParams{Name: "neither"}
	if _, err := c.AddAction(ctx, ids.mgr, neither); !errors.Is(err, ErrInvalidAction) {
		t.Errorf("neither set: want ErrInvalidAction, got %v", err)
	}
}

// TestCrossDefinitionMapping covers the write side of the three same-definition
// FKs: a step or action in one definition referencing rows in another.
func TestCrossDefinitionMapping(t *testing.T) {
	c := newCatalog(t)
	ctx := context.Background()
	defA, a := twoStepDef("A")
	mustCreate(t, c, defA)
	defB, b := twoStepDef("B")
	mustCreate(t, c, defB)

	// fk_step_definition_status: B's step using A's status.
	if _, err := c.AddStep(ctx, b.def, AddStepParams{Name: "x", StatusID: a.status}); !errors.Is(err, ErrCrossDefinition) {
		t.Errorf("cross-def step status: want ErrCrossDefinition, got %v", err)
	}
	// fk_action_definition_next_step: B's action routing to A's step.
	if _, err := c.AddAction(ctx, b.mgr, AddActionParams{Name: "y", NextStepID: &a.dir}); !errors.Is(err, ErrCrossDefinition) {
		t.Errorf("cross-def next step: want ErrCrossDefinition, got %v", err)
	}
	// fk_action_definition_terminal_status: B's action ending in A's status.
	if _, err := c.AddAction(ctx, b.mgr, AddActionParams{Name: "z", TerminalStatusID: &a.status}); !errors.Is(err, ErrCrossDefinition) {
		t.Errorf("cross-def terminal status: want ErrCrossDefinition, got %v", err)
	}
}

// TestReferencedDeleteMapping covers the delete side of the same FKs — the other
// half of the two-sided constraints — using a definition whose statuses have
// isolated roles so each FK can be provoked on its own.
func TestReferencedDeleteMapping(t *testing.T) {
	c := newCatalog(t)
	ctx := context.Background()

	sStep := uuid.Must(uuid.NewV7()) // used by steps
	sTerm := uuid.Must(uuid.NewV7()) // used only as a terminal status
	st1 := uuid.Must(uuid.NewV7())   // entry step, routes to st2
	st2 := uuid.Must(uuid.NewV7())
	defID := uuid.Must(uuid.NewV7())

	def := WorkflowDefinition{
		ID: defID, Name: "Referenced", InitialStepDefinitionID: &st1,
		Statuses: []WorkflowStatusDefinition{{ID: sStep, Name: "working"}, {ID: sTerm, Name: "done"}},
		Steps: []StepDefinition{
			{ID: st1, WorkflowStatusDefinitionID: sStep, Name: "first", Actions: []ActionDefinition{
				{Name: "go", NextStepDefinitionID: &st2},
				{Name: "finish", TerminalWorkflowStatusDefinitionID: &sTerm},
			}},
			{ID: st2, WorkflowStatusDefinitionID: sStep, Name: "second"},
		},
	}
	mustCreate(t, c, def)

	// Each blocked delete rolls back, so the tree stays intact between attempts.
	cases := []struct {
		name   string
		call   func() error
		entity string
		id     uuid.UUID
	}{
		{"delete step routed-to (fk_action_definition_next_step)", func() error { return c.DeleteStep(ctx, st2) }, entityStep, st2},
		{"delete entry step (fk_workflow_definition_initial_step)", func() error { return c.DeleteStep(ctx, st1) }, entityStep, st1},
		{"delete in-use step status (fk_step_definition_status)", func() error { return c.DeleteStatus(ctx, sStep) }, entityStatus, sStep},
		{"delete in-use terminal status (fk_action_definition_terminal_status)", func() error { return c.DeleteStatus(ctx, sTerm) }, entityStatus, sTerm},
	}
	for _, tc := range cases {
		err := tc.call()
		var ref *ReferencedError
		if !errors.As(err, &ref) {
			t.Errorf("%s: want *ReferencedError, got %v", tc.name, err)
			continue
		}
		if ref.Entity != tc.entity || ref.ID != tc.id {
			t.Errorf("%s: ReferencedError = {%q,%v}, want {%q,%v}", tc.name, ref.Entity, ref.ID, tc.entity, tc.id)
		}
	}

	// Sanity: nothing was actually deleted by the blocked attempts.
	if n := rowCount(t, "step_definition"); n != 2 {
		t.Errorf("step_definition rows = %d, want 2 (no blocked delete took effect)", n)
	}
}
