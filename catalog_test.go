package flowcore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCreateAndGetDeepTree(t *testing.T) {
	c := newCatalog(t)
	ctx := context.Background()
	def, ids := twoStepDef("Expense")

	created := mustCreate(t, c, def)
	if created.ID != ids.def {
		t.Errorf("created id = %v, want supplied %v", created.ID, ids.def)
	}
	if created.InitialStepDefinitionID == nil || *created.InitialStepDefinitionID != ids.mgr {
		t.Errorf("initial step = %v, want %v", created.InitialStepDefinitionID, ids.mgr)
	}

	got, err := c.Get(ctx, ids.def)
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
	for _, s := range got.Steps {
		if s.WorkflowDefinitionID != ids.def {
			t.Errorf("step %q WorkflowDefinitionID = %v, want %v", s.Name, s.WorkflowDefinitionID, ids.def)
		}
		for _, a := range s.Actions {
			if a.WorkflowDefinitionID != ids.def || a.StepDefinitionID != s.ID {
				t.Errorf("action %q links = {%v,%v}, want {%v,%v}", a.Name, a.WorkflowDefinitionID, a.StepDefinitionID, ids.def, s.ID)
			}
		}
	}
}

func TestCreateGeneratesMissingIDsAndPreservesSupplied(t *testing.T) {
	c := newCatalog(t)

	// Caller ids the status (an action references it) but leaves the definition
	// and step ids zero and sets no initial pointer: library generates the
	// missing ids and defaults the entry step to Steps[0].
	sid := uuid.Must(uuid.NewV7())
	def := WorkflowDefinition{
		Name:     "Generated",
		Statuses: []WorkflowStatusDefinition{{ID: sid, Name: "s"}},
		Steps: []StepDefinition{
			{WorkflowStatusDefinitionID: sid, Name: "only", Actions: []ActionDefinition{
				{Name: "end", TerminalWorkflowStatusDefinitionID: &sid},
			}},
		},
	}
	created := mustCreate(t, c, def)
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
	c := newCatalog(t)
	def, _ := twoStepDef("NoMutate")
	// Blank the child parent-links so we can prove Create fills its copy, not ours.
	def.Steps[0].WorkflowDefinitionID = uuid.Nil
	def.Steps[0].Actions[0].WorkflowDefinitionID = uuid.Nil
	def.Steps[0].Actions[0].StepDefinitionID = uuid.Nil

	mustCreate(t, c, def)

	if def.Steps[0].WorkflowDefinitionID != uuid.Nil {
		t.Error("Create mutated caller's step.WorkflowDefinitionID")
	}
	if def.Steps[0].Actions[0].WorkflowDefinitionID != uuid.Nil || def.Steps[0].Actions[0].StepDefinitionID != uuid.Nil {
		t.Error("Create mutated caller's action links")
	}
}

func TestCreateIsAtomic(t *testing.T) {
	c := newCatalog(t)
	ctx := context.Background()

	// Two statuses with the same name (case-insensitive) fail mid-insert; the
	// whole transaction must roll back, leaving nothing.
	def, ids := twoStepDef("Atomic")
	def.Statuses = append(def.Statuses, WorkflowStatusDefinition{ID: uuid.Must(uuid.NewV7()), Name: "In Progress"})

	_, err := c.Create(ctx, def)
	if !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("want ErrDuplicateName, got %v", err)
	}
	if _, err := c.Get(ctx, ids.def); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after failed Create: want ErrNotFound, got %v", err)
	}
	assertEmpty(t)
}

func TestCreateCommitPathMapsCrossDefinition(t *testing.T) {
	c := newCatalog(t)
	ctx := context.Background()

	// A valid definition to point at.
	otherDef, other := twoStepDef("Other")
	mustCreate(t, c, otherDef)

	// A new definition whose routing action (manager.approve) points at the OTHER
	// definition's step. All ids are internally consistent and only next-step is
	// set (XOR holds), so the violation is a deferred FK that fires at commit —
	// this exercises the Commit-error mapping path.
	def, _ := twoStepDef("Bad")
	def.Steps[0].Actions[0].NextStepDefinitionID = &other.dir

	_, err := c.Create(ctx, def)
	if !errors.Is(err, ErrCrossDefinition) {
		t.Fatalf("want ErrCrossDefinition from commit path, got %v", err)
	}
	// Only the OTHER definition survived.
	if n := rowCount(t, "workflow_definition"); n != 1 {
		t.Errorf("workflow_definition rows = %d, want 1", n)
	}
}

func TestGetLoadedButEmptyActionsIsNonNil(t *testing.T) {
	c := newCatalog(t)
	ctx := context.Background()

	// A step with no actions.
	sid := uuid.Must(uuid.NewV7())
	stepID := uuid.Must(uuid.NewV7())
	def := WorkflowDefinition{
		Name: "Empty actions", InitialStepDefinitionID: &stepID,
		Statuses: []WorkflowStatusDefinition{{ID: sid, Name: "s"}},
		Steps:    []StepDefinition{{ID: stepID, WorkflowStatusDefinitionID: sid, Name: "lonely"}},
	}
	created := mustCreate(t, c, def)

	got, err := c.Get(ctx, created.ID)
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
	c := newCatalog(t)
	ctx := context.Background()

	// Explicit mismatch: initial points at an id not in Steps.
	def, _ := twoStepDef("Mismatch")
	stray := uuid.Must(uuid.NewV7())
	def.InitialStepDefinitionID = &stray
	if _, err := c.Create(ctx, def); !errors.Is(err, ErrInitialStepNotInTree) {
		t.Errorf("explicit mismatch: want ErrInitialStepNotInTree, got %v", err)
	}

	// No steps at all.
	if _, err := c.Create(ctx, WorkflowDefinition{Name: "empty"}); !errors.Is(err, ErrNoSteps) {
		t.Errorf("no steps: want ErrNoSteps, got %v", err)
	}
}

func TestDeleteWorkflowDefinitionCascades(t *testing.T) {
	c := newCatalog(t)
	ctx := context.Background()
	def, ids := twoStepDef("ToDelete")
	mustCreate(t, c, def)

	if err := c.DeleteWorkflowDefinition(ctx, ids.def); err != nil {
		t.Fatalf("DeleteWorkflowDefinition: %v", err)
	}
	if _, err := c.Get(ctx, ids.def); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after delete: want ErrNotFound, got %v", err)
	}
	assertEmpty(t)
}

func TestNameLengthRejected(t *testing.T) {
	c := newCatalog(t)
	ctx := context.Background()
	def, ids := twoStepDef("Lengths")
	mustCreate(t, c, def)

	if _, err := c.AddStatus(ctx, ids.def, AddStatusParams{Name: ""}); !errors.Is(err, ErrInvalidName) {
		t.Errorf("empty name: want ErrInvalidName, got %v", err)
	}
	if _, err := c.AddStatus(ctx, ids.def, AddStatusParams{Name: strings.Repeat("x", 201)}); !errors.Is(err, ErrInvalidName) {
		t.Errorf("201-char name: want ErrInvalidName, got %v", err)
	}
}
