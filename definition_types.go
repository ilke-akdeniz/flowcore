// Package flowcore is a subject-agnostic workflow library. Its configuration
// side — the definition entities modeled here — is a template from which the
// Engine later starts running workflow instances. Definitions are edited
// through the Catalog type.
package flowcore

import "github.com/google/uuid"

// WorkflowDefinition is the template for a workflow: a set of statuses, a set of
// steps (each with its actions), and the entry step a run begins on. It is the
// aggregate root — Create writes the whole tree in one transaction and Get reads
// it back whole.
type WorkflowDefinition struct {
	ID   uuid.UUID
	Name string
	// InitialStepDefinitionID is the entry step a run begins on. Nil only on a
	// partially built definition; the aggregate Create always sets it. It
	// references a step in Steps.
	InitialStepDefinitionID *uuid.UUID
	Statuses                []WorkflowStatusDefinition
	Steps                   []StepDefinition
}

// WorkflowStatusDefinition is a named status a workflow can be in (e.g. "in
// progress", "approved"). A step shows one while a run sits on it; a terminal
// action ends a run in one.
type WorkflowStatusDefinition struct {
	ID                   uuid.UUID
	WorkflowDefinitionID uuid.UUID
	Name                 string
}

// StepDefinition is one step in a workflow: the status a run shows while on it,
// an optional default assignee, and the actions that leave it.
type StepDefinition struct {
	ID                         uuid.UUID
	WorkflowDefinitionID       uuid.UUID
	WorkflowStatusDefinitionID uuid.UUID
	// AssigneeID is an opaque reference to the person or group expected to act
	// on this step. The library never interprets it. Nil means unassigned.
	AssigneeID *string
	Name       string
	// Actions is the set of actions leaving this step. Loaded by Get and by the
	// mutating methods on return. An empty non-nil slice means "loaded, no
	// actions"; nil means "not loaded".
	Actions []ActionDefinition
}

// ActionDefinition is a choice available on a step. Exactly one of
// NextStepDefinitionID (route to another step) or
// TerminalWorkflowStatusDefinitionID (end the run in a status) is set.
type ActionDefinition struct {
	ID                   uuid.UUID
	WorkflowDefinitionID uuid.UUID
	StepDefinitionID     uuid.UUID
	Name                 string
	// NextStepDefinitionID routes to another step in the same definition. Nil
	// when the action is terminal.
	NextStepDefinitionID *uuid.UUID
	// TerminalWorkflowStatusDefinitionID ends the run in a status of the same
	// definition. Nil when the action routes to a next step.
	TerminalWorkflowStatusDefinitionID *uuid.UUID
}

// ToUpdate returns the update params for this definition, pre-filled from its
// current state. Note it does not carry statuses or steps: those are managed
// through their own Add/Update/Delete methods, never through
// UpdateWorkflowDefinition.
//
// A definition with no entry step yields a zero InitialStepDefinitionID, which
// the update rejects rather than stores — the same loud failure a caller would
// get for omitting it.
func (w WorkflowDefinition) ToUpdate() UpdateWorkflowDefinitionParams {
	params := UpdateWorkflowDefinitionParams{Name: w.Name}
	if w.InitialStepDefinitionID != nil {
		params.InitialStepDefinitionID = *w.InitialStepDefinitionID
	}

	return params
}

// ToUpdate returns the update params for this status, pre-filled from its
// current state, so a caller can read-modify-write without copying fields by
// hand.
func (s WorkflowStatusDefinition) ToUpdate() UpdateStatusParams {
	return UpdateStatusParams{Name: s.Name}
}

// ToUpdate returns the update params for this step, pre-filled from its current
// state. Note it does not carry actions: a step's actions are managed through
// AddAction/UpdateAction/DeleteAction, never through UpdateStep.
//
// This is the safe way to build UpdateStepParams, not merely the convenient one.
// Update is a full replace, so params assembled by hand overwrite every column
// they list; starting from ToUpdate carries the stored assignee forward so that
// changing a step's name cannot unassign it as a side effect.
func (s StepDefinition) ToUpdate() UpdateStepParams {
	assignee := Clear[string]()
	if s.AssigneeID != nil {
		assignee = SetTo(*s.AssigneeID)
	}

	return UpdateStepParams{
		Name:       s.Name,
		StatusID:   s.WorkflowStatusDefinitionID,
		AssigneeID: assignee,
	}
}

// ToUpdate returns the update params for this action, pre-filled from its
// current state, preserving whichever of next-step / terminal-status is set.
func (a ActionDefinition) ToUpdate() UpdateActionParams {
	return UpdateActionParams{
		Name:             a.Name,
		NextStepID:       a.NextStepDefinitionID,
		TerminalStatusID: a.TerminalWorkflowStatusDefinitionID,
	}
}
