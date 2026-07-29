package flowcore

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestAddStepReturnsEmptyNonNilActions(t *testing.T) {
	catalog := newCatalog(t)
	ctx := context.Background()
	definition, ids := twoStepDefinition("AddStep")
	mustCreate(t, catalog, definition)

	step, err := catalog.AddStep(ctx, ids.workflow,
		AddStepParams{
			Name:       "vp review",
			StatusID:   ids.status,
			AssigneeID: ptr("group:vp"),
		})
	if err != nil {
		t.Fatalf("AddStep: %v", err)
	}

	if step.Actions == nil || len(step.Actions) != 0 {
		t.Errorf("AddStep Actions = %v, want empty non-nil", step.Actions)
	}

	if step.AssigneeID == nil || *step.AssigneeID != "group:vp" {
		t.Errorf("AssigneeID = %v, want group:vp", step.AssigneeID)
	}
}

func TestUpdateStepFullReplaceAndActionRefetch(t *testing.T) {
	catalog := newCatalog(t)
	ctx := context.Background()
	definition, ids := twoStepDefinition("UpdateStep")
	mustCreate(t, catalog, definition)

	// manager review starts with an assignee via a fresh add so we can watch it clear.
	step, err := catalog.AddStep(ctx, ids.workflow,
		AddStepParams{
			Name:       "vp review",
			StatusID:   ids.status,
			AssigneeID: ptr("group:vp"),
		})
	if err != nil {
		t.Fatalf("AddStep: %v", err)
	}

	if _, err := catalog.AddAction(ctx, step.ID, AddActionParams{Name: "approve", TerminalStatusID: &ids.status}); err != nil {
		t.Fatalf("AddAction: %v", err)
	}

	// Full replace: new name, same status, assignee cleared — but only because
	// Clear says so. Omitting the field is rejected, not treated as a clear.
	updated, err := catalog.UpdateStep(
		ctx,
		step.ID,
		UpdateStepParams{
			Name:       "VP Review",
			StatusID:   ids.status,
			AssigneeID: Clear[string](),
		})
	if err != nil {
		t.Fatalf("UpdateStep: %v", err)
	}

	if updated.Name != "VP Review" {
		t.Errorf("name = %q, want VP Review", updated.Name)
	}

	if updated.AssigneeID != nil {
		t.Errorf("assignee = %v, want nil (Clear)", *updated.AssigneeID)
	}

	// Actions re-fetched and populated on return.
	if len(updated.Actions) != 1 {
		t.Errorf("returned actions = %d, want 1 (re-fetched)", len(updated.Actions))
	}
}

func TestToUpdateRoundTrip(t *testing.T) {
	catalog := newCatalog(t)
	ctx := context.Background()
	definition, ids := twoStepDefinition("RoundTrip")
	created := mustCreate(t, catalog, definition)

	// Grab manager review from the tree, change only its name via ToUpdate.
	var managerStep StepDefinition
	for _, step := range created.Steps {
		if step.ID == ids.managerStep {
			managerStep = step
		}
	}

	params := managerStep.ToUpdate()
	params.Name = "Manager Sign-off"

	updated, err := catalog.UpdateStep(ctx, ids.managerStep, params)
	if err != nil {
		t.Fatalf("UpdateStep: %v", err)
	}

	if updated.Name != "Manager Sign-off" {
		t.Errorf("name = %q, want Manager Sign-off", updated.Name)
	}

	// Status carried forward unchanged by ToUpdate.
	if updated.WorkflowStatusDefinitionID != ids.status {
		t.Errorf("status changed unexpectedly: %v", updated.WorkflowStatusDefinitionID)
	}

	// The case ToUpdate exists to protect: a step that HAS an assignee, renamed
	// through ToUpdate, keeps it. The fixture above has no assignee, so without
	// this the round-trip would never exercise carrying a non-nil value forward.
	assigned, err := catalog.AddStep(
		ctx,
		ids.workflow,
		AddStepParams{
			Name:       "vp review",
			StatusID:   ids.status,
			AssigneeID: ptr("group:vp"),
		})
	if err != nil {
		t.Fatalf("AddStep: %v", err)
	}

	renameOnly := assigned.ToUpdate()
	renameOnly.Name = "VP Sign-off"

	renamed, err := catalog.UpdateStep(ctx, assigned.ID, renameOnly)
	if err != nil {
		t.Fatalf("UpdateStep: %v", err)
	}

	if renamed.AssigneeID == nil {
		t.Fatal("assignee was lost renaming through ToUpdate; it must be carried forward")
	}

	if *renamed.AssigneeID != "group:vp" {
		t.Errorf("assignee = %q, want group:vp", *renamed.AssigneeID)
	}
}

// TestUpdateStepRejectsUndecidedAssignee is the regression guard for the failure
// decision 21 recorded: params built by hand, omitting AssigneeID, used to clear
// the column silently. It must now fail before any write.
func TestUpdateStepRejectsUndecidedAssignee(t *testing.T) {
	catalog := newCatalog(t)
	ctx := context.Background()
	definition, ids := twoStepDefinition("Undecided")
	mustCreate(t, catalog, definition)

	step, err := catalog.AddStep(
		ctx,
		ids.workflow,
		AddStepParams{
			Name:       "vp review",
			StatusID:   ids.status,
			AssigneeID: ptr("group:vp"),
		})
	if err != nil {
		t.Fatalf("AddStep: %v", err)
	}

	_, err = catalog.UpdateStep(ctx, step.ID, UpdateStepParams{Name: "VP Review", StatusID: ids.status})

	var fieldErr *FieldNotSetError
	if !errors.As(err, &fieldErr) {
		t.Fatalf("want *FieldNotSetError, got %v", err)
	}

	if !errors.Is(err, ErrFieldNotSet) {
		t.Errorf("want errors.Is(err, ErrFieldNotSet)")
	}

	if fieldErr.Field != "UpdateStepParams.AssigneeID" {
		t.Errorf("Field = %q, want UpdateStepParams.AssigneeID", fieldErr.Field)
	}

	// Rejected before the database was touched: nothing changed.
	unchanged, err := catalog.Get(ctx, ids.workflow)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	for _, s := range unchanged.Steps {
		if s.ID != step.ID {
			continue
		}

		if s.Name != "vp review" {
			t.Errorf("name = %q, want unchanged", s.Name)
		}

		if s.AssigneeID == nil || *s.AssigneeID != "group:vp" {
			t.Error("assignee was modified by a rejected update")
		}
	}
}

// TestUpdateStepSetToReplacesAssignee covers the third state: an explicit new
// value, distinct from both cleared and undecided.
func TestUpdateStepSetToReplacesAssignee(t *testing.T) {
	catalog := newCatalog(t)
	ctx := context.Background()
	definition, ids := twoStepDefinition("SetTo")
	mustCreate(t, catalog, definition)

	step, err := catalog.AddStep(
		ctx,
		ids.workflow,
		AddStepParams{
			Name:       "vp review",
			StatusID:   ids.status,
			AssigneeID: ptr("group:vp"),
		})
	if err != nil {
		t.Fatalf("AddStep: %v", err)
	}

	updated, err := catalog.UpdateStep(
		ctx,
		step.ID,
		UpdateStepParams{
			Name:       "vp review",
			StatusID:   ids.status,
			AssigneeID: SetTo("group:cfo"),
		})
	if err != nil {
		t.Fatalf("UpdateStep: %v", err)
	}

	if updated.AssigneeID == nil || *updated.AssigneeID != "group:cfo" {
		t.Errorf("assignee = %v, want group:cfo", updated.AssigneeID)
	}
}

func TestUpdateStatusAndAction(t *testing.T) {
	catalog := newCatalog(t)
	ctx := context.Background()
	definition, ids := twoStepDefinition("Updates")
	mustCreate(t, catalog, definition)

	got, err := catalog.UpdateStatus(ctx, ids.status, UpdateStatusParams{Name: "In Review"})
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	if got.Name != "In Review" {
		t.Errorf("status name = %q, want In Review", got.Name)
	}

	// Add an action, then flip it from terminal to routing via update.
	added, err := catalog.AddAction(ctx, ids.managerStep, AddActionParams{Name: "escalate", TerminalStatusID: &ids.status})
	if err != nil {
		t.Fatalf("AddAction: %v", err)
	}

	upd, err := catalog.UpdateAction(ctx, added.ID, UpdateActionParams{Name: "escalate", NextStepID: &ids.directorStep})
	if err != nil {
		t.Fatalf("UpdateAction: %v", err)
	}

	if upd.NextStepDefinitionID == nil || *upd.NextStepDefinitionID != ids.directorStep {
		t.Errorf("next step = %v, want %v", upd.NextStepDefinitionID, ids.directorStep)
	}

	if upd.TerminalWorkflowStatusDefinitionID != nil {
		t.Errorf("terminal status = %v, want nil after flip to routing", *upd.TerminalWorkflowStatusDefinitionID)
	}
}

func TestDeleteStatusStepAction(t *testing.T) {
	catalog := newCatalog(t)
	ctx := context.Background()
	definition, ids := twoStepDefinition("Deletes")
	mustCreate(t, catalog, definition)

	// Add an unreferenced status and delete it — allowed.
	extra, err := catalog.AddStatus(ctx, ids.workflow, AddStatusParams{Name: "spare"})
	if err != nil {
		t.Fatalf("AddStatus: %v", err)
	}

	if err := catalog.DeleteStatus(ctx, extra.ID); err != nil {
		t.Errorf("DeleteStatus (unreferenced): %v", err)
	}

	// Delete an action — nothing references actions, always allowed.
	got, err := catalog.Get(ctx, ids.workflow)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	var actionID uuid.UUID
	for _, step := range got.Steps {
		if step.ID == ids.managerStep {
			actionID = step.Actions[0].ID
		}
	}

	if err := catalog.DeleteAction(ctx, actionID); err != nil {
		t.Errorf("DeleteAction: %v", err)
	}
}

func TestNotFoundOnMissingTargets(t *testing.T) {
	catalog := newCatalog(t)
	ctx := context.Background()
	missing := uuid.Must(uuid.NewV7())

	cases := []struct {
		name string
		call func() error
	}{
		{"Get", func() error { _, e := catalog.Get(ctx, missing); return e }},
		{"DeleteWorkflowDefinition", func() error { return catalog.DeleteWorkflowDefinition(ctx, missing) }},
		{"UpdateStatus", func() error { _, e := catalog.UpdateStatus(ctx, missing, UpdateStatusParams{Name: "x"}); return e }},
		{"DeleteStatus", func() error { return catalog.DeleteStatus(ctx, missing) }},
		{"UpdateStep", func() error {
			// AssigneeID must be decided even here: validation runs before the
			// lookup, so an undecided field would mask the not-found this asserts.
			_, e := catalog.UpdateStep(
				ctx,
				missing,
				UpdateStepParams{
					Name:       "x",
					StatusID:   missing,
					AssigneeID: Clear[string](),
				})
			return e
		}},
		{"DeleteStep", func() error { return catalog.DeleteStep(ctx, missing) }},
		{"UpdateAction", func() error {
			_, e := catalog.UpdateAction(ctx, missing, UpdateActionParams{Name: "x", TerminalStatusID: &missing})
			return e
		}},
		{"DeleteAction", func() error { return catalog.DeleteAction(ctx, missing) }},
		{"AddStatus (missing definition)", func() error { _, e := catalog.AddStatus(ctx, missing, AddStatusParams{Name: "x"}); return e }},
		{"AddStep (missing definition)", func() error {
			_, e := catalog.AddStep(ctx, missing, AddStepParams{Name: "x", StatusID: missing})
			return e
		}},
		{"AddAction (missing step)", func() error {
			_, e := catalog.AddAction(ctx, missing, AddActionParams{Name: "x", TerminalStatusID: &missing})
			return e
		}},
	}
	for _, tc := range cases {
		if err := tc.call(); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s on missing id: want ErrNotFound, got %v", tc.name, err)
		}
	}
}

func TestNotFoundCarriesEntityAndID(t *testing.T) {
	catalog := newCatalog(t)
	missing := uuid.Must(uuid.NewV7())

	err := catalog.DeleteStep(context.Background(), missing)
	var notFoundErr *NotFoundError
	if !errors.As(err, &notFoundErr) {
		t.Fatalf("want *NotFoundError, got %v", err)
	}

	if notFoundErr.Entity != entityStep || notFoundErr.ID != missing {
		t.Errorf("NotFoundError = {%q,%v}, want {%q,%v}", notFoundErr.Entity, notFoundErr.ID, entityStep, missing)
	}
}
