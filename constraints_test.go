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

// TestCrossDefinitionMappingOnUpdate covers the same three FKs from the update
// side. These reference FKs are DEFERRABLE INITIALLY DEFERRED, so their check
// fires at the statement's implicit commit — which, since the update methods
// read back via UPDATE ... RETURNING (decision 19), is after the returned row
// has been produced. The point of this test is that the violation still reaches
// mapWriteErr rather than being masked by a successfully scanned row.
func TestCrossDefinitionMappingOnUpdate(t *testing.T) {
	catalog := newCatalog(t)
	ctx := context.Background()
	definitionA, a := twoStepDefinition("A")
	mustCreate(t, catalog, definitionA)
	definitionB, b := twoStepDefinition("B")
	mustCreate(t, catalog, definitionB)

	// fk_step_definition_status: repointing B's step at A's status.
	if _, err := catalog.UpdateStep(
		ctx,
		b.managerStep,
		UpdateStepParams{
			Name:     "Manager Review",
			StatusID: a.status,
		}); !errors.Is(err, ErrCrossDefinition) {
		t.Errorf("cross-def step status on update: want ErrCrossDefinition, got %v", err)
	}

	action, err := catalog.AddAction(ctx, b.managerStep, AddActionParams{Name: "proceed", NextStepID: &b.directorStep})
	if err != nil {
		t.Fatalf("AddAction: %v", err)
	}

	// fk_action_definition_next_step: repointing B's action at A's step.
	if _, err := catalog.UpdateAction(
		ctx,
		action.ID,
		UpdateActionParams{
			Name:       "proceed",
			NextStepID: &a.directorStep,
		}); !errors.Is(err, ErrCrossDefinition) {
		t.Errorf("cross-def next step on update: want ErrCrossDefinition, got %v", err)
	}

	// fk_action_definition_terminal_status: repointing B's action at A's status.
	if _, err := catalog.UpdateAction(
		ctx,
		action.ID,
		UpdateActionParams{
			Name:             "proceed",
			TerminalStatusID: &a.status,
		}); !errors.Is(err, ErrCrossDefinition) {
		t.Errorf("cross-def terminal status on update: want ErrCrossDefinition, got %v", err)
	}
}

// TestCascadeDriverFKMapsToNotFound covers the three parent foreign keys, which
// are what enforce parent existence now that the pre-flight reads are gone
// (decision 20). Without this the coverage would be incidental: the not-found
// matrix exercises these same paths, but would still pass if they were enforced
// by a read instead of by the constraint.
//
// The action case is provoked at the store rather than through AddAction, which
// still reads its step first — for the workflow_definition_id it needs, not as a
// check. The only way its insert sees a missing step is a concurrent delete, and
// calling the store directly reproduces that state without the timing.
func TestCascadeDriverFKMapsToNotFound(t *testing.T) {
	catalog := newCatalog(t)
	ctx := context.Background()
	missing := uuid.Must(uuid.NewV7())

	var notFoundErr *NotFoundError

	// fk_workflow_status_definition_workflow
	_, err := catalog.AddStatus(ctx, missing, AddStatusParams{Name: "orphan"})
	if !errors.As(err, &notFoundErr) || notFoundErr.Entity != entityWorkflowDefinition {
		t.Errorf("AddStatus with missing definition: want NotFoundError on the definition, got %v", err)
	}

	// fk_step_definition_workflow. The status reference is missing too, but that
	// FK is deferred while this one is immediate, so the missing parent wins.
	_, err = catalog.AddStep(
		ctx,
		missing,
		AddStepParams{
			Name:     "orphan",
			StatusID: missing,
		})
	if !errors.As(err, &notFoundErr) || notFoundErr.Entity != entityWorkflowDefinition {
		t.Errorf("AddStep with missing definition: want NotFoundError on the definition, got %v", err)
	}

	// fk_action_definition_step: the state a concurrent delete of the step leaves
	// AddAction's insert in.
	orphanStepID := uuid.Must(uuid.NewV7())
	orphan := ActionDefinition{
		ID:                                 uuid.Must(uuid.NewV7()),
		WorkflowDefinitionID:               missing,
		StepDefinitionID:                   orphanStepID,
		Name:                               "orphan",
		TerminalWorkflowStatusDefinitionID: &missing,
	}
	err = insertAction(ctx, testPool, orphan)
	if !errors.As(err, &notFoundErr) || notFoundErr.Entity != entityStep {
		t.Errorf("insertAction with missing step: want NotFoundError on the step, got %v", err)
	}

	if notFoundErr != nil && notFoundErr.ID != orphanStepID {
		t.Errorf("insertAction: NotFoundError should name the missing step %s, got %s", orphanStepID, notFoundErr.ID)
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
