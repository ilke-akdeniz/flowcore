package flowcore

import "github.com/google/uuid"

// Each mutating operation on Catalog takes a dedicated params struct
// carrying only the columns that operation may set. Identity and
// parent-membership columns are deliberately absent: they cannot be changed, so
// they are unrepresentable rather than validated. Update is a full replace of
// the listed columns — a nil pointer clears the column to NULL, it does not mean
// "leave unchanged".

// AddStatusParams are the settable columns when adding a status to a definition.
type AddStatusParams struct {
	Name string
}

// UpdateStatusParams are the settable columns when updating a status.
type UpdateStatusParams struct {
	Name string
}

// AddStepParams are the settable columns when adding a step to a definition.
// StatusID must reference a status in the same definition; AssigneeID is opaque
// and nil means unassigned.
type AddStepParams struct {
	Name       string
	StatusID   uuid.UUID
	AssigneeID *string
}

// UpdateStepParams are the settable columns when updating a step. It carries no
// actions: actions are managed through AddAction/UpdateAction/DeleteAction.
type UpdateStepParams struct {
	Name       string
	StatusID   uuid.UUID
	AssigneeID *string
}

// AddActionParams are the settable columns when adding an action to a step.
// Exactly one of NextStepID / TerminalStatusID must be set; both or neither is
// rejected (InvalidActionError).
type AddActionParams struct {
	Name             string
	NextStepID       *uuid.UUID
	TerminalStatusID *uuid.UUID
}

// UpdateActionParams are the settable columns when updating an action. The same
// exactly-one rule as AddActionParams applies.
type UpdateActionParams struct {
	Name             string
	NextStepID       *uuid.UUID
	TerminalStatusID *uuid.UUID
}
