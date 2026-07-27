package flowcore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCreateAndGetDeepTree(t *testing.T) {
	catalog := newCatalog(t)
	ctx := context.Background()
	definition, ids := twoStepDefinition("Expense")

	created := mustCreate(t, catalog, definition)
	if created.ID != ids.workflow {
		t.Errorf("created id = %v, want supplied %v", created.ID, ids.workflow)
	}

	if created.InitialStepDefinitionID == nil || *created.InitialStepDefinitionID != ids.managerStep {
		t.Errorf("initial step = %v, want %v", created.InitialStepDefinitionID, ids.managerStep)
	}

	got, err := catalog.Get(ctx, ids.workflow)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if len(got.Statuses) != 1 || len(got.Steps) != 2 {
		t.Fatalf("tree shape: %d statuses, %d steps", len(got.Statuses), len(got.Steps))
	}

	// Steps ordered by name: "director review" < "manager review".
	if got.Steps[0].Name != "director review" || got.Steps[1].Name != "manager review" {
		t.Errorf("step order = %q, %q", got.Steps[0].Name, got.Steps[1].Name)
	}

	// director review has one action; manager review has two.
	if len(got.Steps[0].Actions) != 1 {
		t.Errorf("director actions = %d, want 1", len(got.Steps[0].Actions))
	}

	if len(got.Steps[1].Actions) != 2 {
		t.Errorf("manager actions = %d, want 2", len(got.Steps[1].Actions))
	}

	// Parent links populated by the read.
	for _, step := range got.Steps {
		if step.WorkflowDefinitionID != ids.workflow {
			t.Errorf("step %q WorkflowDefinitionID = %v, want %v", step.Name, step.WorkflowDefinitionID, ids.workflow)
		}

		for _, action := range step.Actions {
			if action.WorkflowDefinitionID != ids.workflow || action.StepDefinitionID != step.ID {
				t.Errorf("action %q links = {%v,%v}, want {%v,%v}", action.Name, action.WorkflowDefinitionID, action.StepDefinitionID, ids.workflow, step.ID)
			}
		}
	}
}

func TestCreateGeneratesMissingIDsAndPreservesSupplied(t *testing.T) {
	catalog := newCatalog(t)

	// Caller ids the status (an action references it) but leaves the definition
	// and step ids zero and sets no initial pointer: library generates the
	// missing ids and defaults the entry step to Steps[0].
	sid := uuid.Must(uuid.NewV7())
	definition := WorkflowDefinition{
		Name:     "Generated",
		Statuses: []WorkflowStatusDefinition{{ID: sid, Name: "s"}},
		Steps: []StepDefinition{
			{WorkflowStatusDefinitionID: sid, Name: "only", Actions: []ActionDefinition{
				{Name: "end", TerminalWorkflowStatusDefinitionID: &sid},
			}},
		},
	}
	created := mustCreate(t, catalog, definition)
	if created.ID == uuid.Nil {
		t.Error("definition id was not generated")
	}

	if created.Steps[0].ID == uuid.Nil {
		t.Error("step id was not generated")
	}

	if created.Statuses[0].ID != sid {
		t.Errorf("supplied status id not preserved: %v != %v", created.Statuses[0].ID, sid)
	}

	if created.InitialStepDefinitionID == nil || *created.InitialStepDefinitionID != created.Steps[0].ID {
		t.Errorf("entry step not defaulted to Steps[0]")
	}
}

func TestCreateDoesNotMutateCallerInput(t *testing.T) {
	catalog := newCatalog(t)
	definition, _ := twoStepDefinition("NoMutate")
	// Blank the child parent-links so we can prove Create fills its copy, not ours.
	definition.Steps[0].WorkflowDefinitionID = uuid.Nil
	definition.Steps[0].Actions[0].WorkflowDefinitionID = uuid.Nil
	definition.Steps[0].Actions[0].StepDefinitionID = uuid.Nil

	mustCreate(t, catalog, definition)

	if definition.Steps[0].WorkflowDefinitionID != uuid.Nil {
		t.Error("Create mutated caller's step.WorkflowDefinitionID")
	}

	if definition.Steps[0].Actions[0].WorkflowDefinitionID != uuid.Nil || definition.Steps[0].Actions[0].StepDefinitionID != uuid.Nil {
		t.Error("Create mutated caller's action links")
	}
}

func TestCreateIsAtomic(t *testing.T) {
	catalog := newCatalog(t)
	ctx := context.Background()

	// Two statuses with the same name (case-insensitive) fail mid-insert; the
	// whole transaction must roll back, leaving nothing.
	definition, ids := twoStepDefinition("Atomic")
	definition.Statuses = append(definition.Statuses, WorkflowStatusDefinition{ID: uuid.Must(uuid.NewV7()), Name: "In Progress"})

	_, err := catalog.Create(ctx, definition)
	if !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("want ErrDuplicateName, got %v", err)
	}

	if _, err := catalog.Get(ctx, ids.workflow); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after failed Create: want ErrNotFound, got %v", err)
	}

	assertEmpty(t)
}

func TestCreateCommitPathMapsCrossDefinition(t *testing.T) {
	catalog := newCatalog(t)
	ctx := context.Background()

	// A valid definition to point at.
	otherDefinition, other := twoStepDefinition("Other")
	mustCreate(t, catalog, otherDefinition)

	// A new definition whose routing action (manager.approve) points at the OTHER
	// definition's step. All ids are internally consistent and only next-step is
	// set (XOR holds), so the violation is a deferred FK that fires at commit —
	// this exercises the Commit-error mapping path.
	definition, _ := twoStepDefinition("Bad")
	definition.Steps[0].Actions[0].NextStepDefinitionID = &other.directorStep

	_, err := catalog.Create(ctx, definition)
	if !errors.Is(err, ErrCrossDefinition) {
		t.Fatalf("want ErrCrossDefinition from commit path, got %v", err)
	}

	// Only the OTHER definition survived.
	if n := rowCount(t, "workflow_definition"); n != 1 {
		t.Errorf("workflow_definition rows = %d, want 1", n)
	}
}

func TestGetLoadedButEmptyActionsIsNonNil(t *testing.T) {
	catalog := newCatalog(t)
	ctx := context.Background()

	// A step with no actions.
	sid := uuid.Must(uuid.NewV7())
	stepID := uuid.Must(uuid.NewV7())
	definition := WorkflowDefinition{
		Name: "Empty actions", InitialStepDefinitionID: &stepID,
		Statuses: []WorkflowStatusDefinition{{ID: sid, Name: "s"}},
		Steps:    []StepDefinition{{ID: stepID, WorkflowStatusDefinitionID: sid, Name: "lonely"}},
	}
	created := mustCreate(t, catalog, definition)

	got, err := catalog.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Steps[0].Actions == nil {
		t.Error("loaded step with no actions has nil Actions; want empty non-nil slice")
	}

	if len(got.Steps[0].Actions) != 0 {
		t.Errorf("Actions len = %d, want 0", len(got.Steps[0].Actions))
	}
}

func TestEntryStepContract(t *testing.T) {
	catalog := newCatalog(t)
	ctx := context.Background()

	// Explicit mismatch: initial points at an id not in Steps.
	definition, _ := twoStepDefinition("Mismatch")
	stray := uuid.Must(uuid.NewV7())
	definition.InitialStepDefinitionID = &stray
	if _, err := catalog.Create(ctx, definition); !errors.Is(err, ErrInitialStepNotInTree) {
		t.Errorf("explicit mismatch: want ErrInitialStepNotInTree, got %v", err)
	}

	// No steps at all.
	if _, err := catalog.Create(ctx, WorkflowDefinition{Name: "empty"}); !errors.Is(err, ErrNoSteps) {
		t.Errorf("no steps: want ErrNoSteps, got %v", err)
	}
}

func TestDeleteWorkflowDefinitionCascades(t *testing.T) {
	catalog := newCatalog(t)
	ctx := context.Background()
	definition, ids := twoStepDefinition("ToDelete")
	mustCreate(t, catalog, definition)

	if err := catalog.DeleteWorkflowDefinition(ctx, ids.workflow); err != nil {
		t.Fatalf("DeleteWorkflowDefinition: %v", err)
	}

	if _, err := catalog.Get(ctx, ids.workflow); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after delete: want ErrNotFound, got %v", err)
	}

	assertEmpty(t)
}

func TestNameLengthRejected(t *testing.T) {
	catalog := newCatalog(t)
	ctx := context.Background()
	definition, ids := twoStepDefinition("Lengths")
	mustCreate(t, catalog, definition)

	if _, err := catalog.AddStatus(ctx, ids.workflow, AddStatusParams{Name: ""}); !errors.Is(err, ErrInvalidName) {
		t.Errorf("empty name: want ErrInvalidName, got %v", err)
	}

	if _, err := catalog.AddStatus(ctx, ids.workflow, AddStatusParams{Name: strings.Repeat("x", 201)}); !errors.Is(err, ErrInvalidName) {
		t.Errorf("201-char name: want ErrInvalidName, got %v", err)
	}
}
