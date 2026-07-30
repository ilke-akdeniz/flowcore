package flowcore

import "github.com/google/uuid"

// Each mutating operation on Catalog takes a dedicated params struct
// carrying only the columns that operation may set. Identity and
// parent-membership columns are deliberately absent: they cannot be changed, so
// they are unrepresentable rather than validated. Update is a full replace of
// the listed columns; it never means "leave unchanged".
//
// Full replace makes an omitted field destructive, and most fields are protected
// from that by a constraint — an empty Name fails a length CHECK, a zero StatusID
// fails a foreign key, an action with neither next step nor terminal status fails
// the XOR CHECK. A nullable column with no constraint has no such backstop, so it
// is typed Nullable and must be decided explicitly.

// Nullable carries a three-state value for a settable column that is nullable in
// the schema: unset, set to a value, or explicitly cleared to NULL. Its zero
// value is unset, which an update rejects with FieldNotSetError — so a caller who
// simply never touched the field can never be mistaken for one who meant to clear
// it.
//
// It exists because assignee_id is the only settable column with nothing to catch
// an accidental zero value. It is opaque by design — the library never interprets
// it — so it carries no foreign key, CHECK, or format rule to fail loudly the way
// an unset name or status does.
//
// Build one with SetTo or Clear. Nullable appears only in params, never in a read
// type, so it exposes no accessor.
type Nullable[T any] struct {
	value *T
	set   bool
}

// SetTo sets the column to v.
func SetTo[T any](v T) Nullable[T] { return Nullable[T]{value: &v, set: true} }

// Clear sets the column to NULL. Clearing is always written out, never something
// that happens by leaving a field alone.
func Clear[T any]() Nullable[T] { return Nullable[T]{set: true} }

// ptr returns the value to bind to the SQL parameter: nil for a cleared column.
func (n Nullable[T]) ptr() *T { return n.value }

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
//
// AssigneeID stays a plain pointer here, unlike UpdateStepParams: a create has no
// stored assignee to destroy, so omitting it means "start unassigned" rather than
// "discard what was there".
type AddStepParams struct {
	Name       string
	StatusID   uuid.UUID
	AssigneeID *string
}

// UpdateStepParams are the settable columns when updating a step. It carries no
// actions: actions are managed through AddAction/UpdateAction/DeleteAction.
//
// AssigneeID must be decided explicitly with SetTo or Clear. Building these params
// from StepDefinition.ToUpdate carries the stored assignee forward for you.
type UpdateStepParams struct {
	Name       string
	StatusID   uuid.UUID
	AssigneeID Nullable[string]
}

// validate rejects params whose Nullable fields were never decided. It runs
// before the database is touched, so an undecided field costs no write.
func (p UpdateStepParams) validate() error {
	if !p.AssigneeID.set {
		return &FieldNotSetError{Field: "UpdateStepParams.AssigneeID"}
	}

	return nil
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

// The instance-side params, for the Engine. Nullable does not appear in either:
// nothing updates an instance row, so full replace — and the omitted-field hazard
// it carries — never arises. A plain pointer means what it says, absent.

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
type CompleteParams struct {
	VisitID             uuid.UUID
	ActionID            uuid.UUID
	CompletedBy         string
	SubjectVersionToken *string
}
