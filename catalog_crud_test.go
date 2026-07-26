package flowcore

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestAddStepReturnsEmptyNonNilActions(t *testing.T) {
	c := newCatalog(t)
	ctx := context.Background()
	def, ids := twoStepDef("AddStep")
	mustCreate(t, c, def)

	step, err := c.AddStep(ctx, ids.def, AddStepParams{Name: "vp review", StatusID: ids.status, AssigneeID: ptr("group:vp")})
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
	c := newCatalog(t)
	ctx := context.Background()
	def, ids := twoStepDef("UpdateStep")
	mustCreate(t, c, def)

	// manager review starts with an assignee via a fresh add so we can watch it clear.
	step, err := c.AddStep(ctx, ids.def, AddStepParams{Name: "vp review", StatusID: ids.status, AssigneeID: ptr("group:vp")})
	if err != nil {
		t.Fatalf("AddStep: %v", err)
	}
	if _, err := c.AddAction(ctx, step.ID, AddActionParams{Name: "approve", TerminalStatusID: &ids.status}); err != nil {
		t.Fatalf("AddAction: %v", err)
	}

	// Full replace: new name, same status, assignee omitted (nil) -> cleared.
	updated, err := c.UpdateStep(ctx, step.ID, UpdateStepParams{Name: "VP Review", StatusID: ids.status})
	if err != nil {
		t.Fatalf("UpdateStep: %v", err)
	}
	if updated.Name != "VP Review" {
		t.Errorf("name = %q, want VP Review", updated.Name)
	}
	if updated.AssigneeID != nil {
		t.Errorf("assignee = %v, want nil (full replace clears)", *updated.AssigneeID)
	}
	// Actions re-fetched and populated on return.
	if len(updated.Actions) != 1 {
		t.Errorf("returned actions = %d, want 1 (re-fetched)", len(updated.Actions))
	}
}

func TestToUpdateRoundTrip(t *testing.T) {
	c := newCatalog(t)
	ctx := context.Background()
	def, ids := twoStepDef("RoundTrip")
	created := mustCreate(t, c, def)

	// Grab manager review from the tree, change only its name via ToUpdate.
	var mgr StepDefinition
	for _, s := range created.Steps {
		if s.ID == ids.mgr {
			mgr = s
		}
	}
	params := mgr.ToUpdate()
	params.Name = "Manager Sign-off"

	updated, err := c.UpdateStep(ctx, ids.mgr, params)
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
}

func TestUpdateStatusAndAction(t *testing.T) {
	c := newCatalog(t)
	ctx := context.Background()
	def, ids := twoStepDef("Updates")
	mustCreate(t, c, def)

	got, err := c.UpdateStatus(ctx, ids.status, UpdateStatusParams{Name: "In Review"})
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if got.Name != "In Review" {
		t.Errorf("status name = %q, want In Review", got.Name)
	}

	// Add an action, then flip it from terminal to routing via update.
	added, err := c.AddAction(ctx, ids.mgr, AddActionParams{Name: "escalate", TerminalStatusID: &ids.status})
	if err != nil {
		t.Fatalf("AddAction: %v", err)
	}
	upd, err := c.UpdateAction(ctx, added.ID, UpdateActionParams{Name: "escalate", NextStepID: &ids.dir})
	if err != nil {
		t.Fatalf("UpdateAction: %v", err)
	}
	if upd.NextStepDefinitionID == nil || *upd.NextStepDefinitionID != ids.dir {
		t.Errorf("next step = %v, want %v", upd.NextStepDefinitionID, ids.dir)
	}
	if upd.TerminalWorkflowStatusDefinitionID != nil {
		t.Errorf("terminal status = %v, want nil after flip to routing", *upd.TerminalWorkflowStatusDefinitionID)
	}
}

func TestDeleteStatusStepAction(t *testing.T) {
	c := newCatalog(t)
	ctx := context.Background()
	def, ids := twoStepDef("Deletes")
	mustCreate(t, c, def)

	// Add an unreferenced status and delete it — allowed.
	extra, err := c.AddStatus(ctx, ids.def, AddStatusParams{Name: "spare"})
	if err != nil {
		t.Fatalf("AddStatus: %v", err)
	}
	if err := c.DeleteStatus(ctx, extra.ID); err != nil {
		t.Errorf("DeleteStatus (unreferenced): %v", err)
	}

	// Delete an action — nothing references actions, always allowed.
	got, err := c.Get(ctx, ids.def)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	var actionID uuid.UUID
	for _, s := range got.Steps {
		if s.ID == ids.mgr {
			actionID = s.Actions[0].ID
		}
	}
	if err := c.DeleteAction(ctx, actionID); err != nil {
		t.Errorf("DeleteAction: %v", err)
	}
}

func TestNotFoundOnMissingTargets(t *testing.T) {
	c := newCatalog(t)
	ctx := context.Background()
	missing := uuid.Must(uuid.NewV7())

	cases := []struct {
		name string
		call func() error
	}{
		{"Get", func() error { _, e := c.Get(ctx, missing); return e }},
		{"DeleteWorkflowDefinition", func() error { return c.DeleteWorkflowDefinition(ctx, missing) }},
		{"UpdateStatus", func() error { _, e := c.UpdateStatus(ctx, missing, UpdateStatusParams{Name: "x"}); return e }},
		{"DeleteStatus", func() error { return c.DeleteStatus(ctx, missing) }},
		{"UpdateStep", func() error { _, e := c.UpdateStep(ctx, missing, UpdateStepParams{Name: "x", StatusID: missing}); return e }},
		{"DeleteStep", func() error { return c.DeleteStep(ctx, missing) }},
		{"UpdateAction", func() error { _, e := c.UpdateAction(ctx, missing, UpdateActionParams{Name: "x", TerminalStatusID: &missing}); return e }},
		{"DeleteAction", func() error { return c.DeleteAction(ctx, missing) }},
		{"AddStatus (missing definition)", func() error { _, e := c.AddStatus(ctx, missing, AddStatusParams{Name: "x"}); return e }},
		{"AddStep (missing definition)", func() error { _, e := c.AddStep(ctx, missing, AddStepParams{Name: "x", StatusID: missing}); return e }},
		{"AddAction (missing step)", func() error { _, e := c.AddAction(ctx, missing, AddActionParams{Name: "x", TerminalStatusID: &missing}); return e }},
	}
	for _, tc := range cases {
		if err := tc.call(); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s on missing id: want ErrNotFound, got %v", tc.name, err)
		}
	}
}

func TestNotFoundCarriesEntityAndID(t *testing.T) {
	c := newCatalog(t)
	missing := uuid.Must(uuid.NewV7())

	err := c.DeleteStep(context.Background(), missing)
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("want *NotFoundError, got %v", err)
	}
	if nf.Entity != entityStep || nf.ID != missing {
		t.Errorf("NotFoundError = {%q,%v}, want {%q,%v}", nf.Entity, nf.ID, entityStep, missing)
	}
}
