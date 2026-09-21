package flowcore

import "github.com/google/uuid"

// Each mutating operation on Catalog takes a dedicated params struct
// carrying only the columns that operation may set. Identity and
// parent-membership columns are deliberately absent: they cannot be changed, so
// they are unrepresentable rather than validated. Update is a full replace of
// the listed columns; it never means "leave unchanged".
//
// Full replace makes an omitted field destructive, and every field is protected
// from that by a constraint — an empty Name fails a length CHECK, a zero StatusID
// fails a foreign key, an empty AssigneeID fails its own length CHECK, and an
// action with neither next step nor terminal status fails the XOR CHECK. So a
// forgotten field is always a loud failure before anything is written, and no
// params field needs machinery to distinguish "unset" from "meant it".
//
// That was not always true. Decision 22 introduced a Nullable[T] wrapper for
// AssigneeID, the one settable column that was nullable and so had nothing to
// catch an accidental zero value. Making the column NOT NULL removed the gap
// rather than guarding it, and the wrapper went with it.

// UpdateWorkflowDefinitionParams are the settable columns on a definition itself.
// It carries no statuses or steps: those are managed through their own Add/Update/
// Delete methods, so an Update owns exactly the definition's own row.
//
// InitialStepDefinitionID is required, even though the column is nullable in the
// schema. That NULL is a bootstrap artifact, not a state a definition may be left
// in: Create must insert the definition row before the step it points at exists,
// and stamps it later in the same transaction, so the column is null only
// mid-write. Letting an update clear it would produce a definition that exists
// and can never be started.
//
// A forgotten field fails loudly here: uuid.Nil is not NULL, so it hits the
// entry-step foreign key and returns CrossDefinitionError.
type UpdateWorkflowDefinitionParams struct {
	Name                    string
	InitialStepDefinitionID uuid.UUID
}

// AddStatusParams are the settable columns when adding a status to a definition.
type AddStatusParams struct {
	Name string
}

// UpdateStatusParams are the settable columns when updating a status.
type UpdateStatusParams struct {
	Name string
}

// AddStepParams are the settable columns when adding a step to a definition.
// StatusID must reference a status in the same definition.
//
// AssigneeID is required and opaque. A step whose owner is not yet decided says so
// with a value the client chooses — "unassigned", "pool:support" — rather than by
// omitting one, because the library never interprets the string and a value can be
// found by the worklist where NULL cannot.
type AddStepParams struct {
	Name       string
	StatusID   uuid.UUID
	AssigneeID string
}

// UpdateStepParams are the settable columns when updating a step. It carries no
// actions: actions are managed through AddAction/UpdateAction/DeleteAction.
//
// AssigneeID is required, like every other field here: full replace means an
// omitted one is destructive, and the empty string fails the column's length CHECK
// rather than quietly erasing the assignment. Building these params from
// StepDefinition.ToUpdate carries the stored assignee forward for you.
type UpdateStepParams struct {
	Name       string
	StatusID   uuid.UUID
	AssigneeID string
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

// The instance-side params, for the Engine. A plain pointer means what it says,
// absent: these operations insert rather than replace a row, so there is no stored
// value for an omitted field to destroy. Reassign is the one instance-side write
// that updates, and it takes its single settable value as a required argument
// rather than a params struct, so nothing there can be omitted either.

// StartParams are the inputs for starting a workflow. The definition is read and
// snapshotted at that moment, so later edits to it do not reach the run.
//
// SubjectReference is opaque and required: the library compares it for equality,
// never interprets it, and it is half the key of the one-active-run-per-subject
// rule. SubjectVersionToken is opaque and optional — supply it to make every
// decision in the run answerable as "which revision was this", or leave it nil if
// the subject does not have revisions worth recording.
type StartParams struct {
	WorkflowDefinitionID uuid.UUID
	SubjectReference     string
	SubjectVersionToken  *string
}

// CompleteParams are the inputs for completing the step a run is waiting on.
//
// VisitID names the visit being completed, taken from CurrentStep.VisitID. It is
// how a stale caller is caught: if the run has moved on — including looping back
// to the same step — the visit that was current when the caller last looked is
// closed, and completing it is refused rather than silently applied to a visit
// the caller never saw.
//
// ActionID must be an action of that visit's step, which the schema enforces.
// CompletedBy is required and opaque: the library records who acted and never
// decides whether they were allowed to.
// Remark is optional and opaque: why this decision was made, recorded beside the
// decision itself so the two cannot be separated by a failure between two writes.
// The schema caps it at 3000 characters — a sentence or a page. Anything longer is
// a document, and documents belong in the client, keyed by the subject.
type CompleteParams struct {
	VisitID             uuid.UUID
	ActionID            uuid.UUID
	CompletedBy         string
	SubjectVersionToken *string
	Remark              *string
}
