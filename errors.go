package flowcore

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// The error surface is a small typed taxonomy over the database's rejections.
// Every typed error wraps a sentinel via Unwrap, so a caller can branch coarsely
// with errors.Is(err, ErrDuplicateName) or extract detail with
// errors.As(err, &dupErr). Types use pointer receivers throughout; match with a
// pointer target: errors.As(err, &dupErr) where dupErr is *DuplicateNameError.

// Sentinels. Compare against these with errors.Is.
var (
	// ErrNotFound is returned when an operation targets a row that does not exist.
	ErrNotFound = errors.New("flowcore: not found")
	// ErrDuplicateName is returned when a name collides (case-insensitively)
	// with a sibling's within the same scope.
	ErrDuplicateName = errors.New("flowcore: duplicate name")
	// ErrCrossDefinition is returned when a reference points at a row belonging
	// to a different definition.
	ErrCrossDefinition = errors.New("flowcore: cross-definition reference")
	// ErrReferenced is returned when a row cannot be deleted because another row
	// still references it.
	ErrReferenced = errors.New("flowcore: row is still referenced")
	// ErrInvalidAction is returned when an action does not have exactly one of a
	// next step or a terminal status.
	ErrInvalidAction = errors.New("flowcore: invalid action")
	// ErrInvalidName is returned when a name is empty or too long.
	ErrInvalidName = errors.New("flowcore: invalid name")
	// ErrUnmappedConstraint is returned when a database constraint violation's
	// constraint name is not in the mapper's explicit table — a deliberate
	// fail-loud for an unanticipated constraint, kept distinct from the domain
	// errors above so it is never silently guessed as one of them.
	ErrUnmappedConstraint = errors.New("flowcore: unmapped database constraint")

	// ErrNoSteps and ErrInitialStepNotInTree are pre-flight input checks in
	// Create, not mappings of a database rejection — so, like the others, they
	// carry no fields.
	//
	// ErrNoSteps: the definition has no steps; an empty definition cannot be
	// started.
	ErrNoSteps = errors.New("flowcore: a definition must have at least one step")
	// ErrInitialStepNotInTree: an explicit InitialStepDefinitionID does not match
	// any step in the definition's Steps.
	ErrInitialStepNotInTree = errors.New("flowcore: initial step is not one of the definition's steps")

	// ErrFieldNotSet is returned when a Nullable params field was left at its
	// zero value instead of being decided with SetTo or Clear. See
	// FieldNotSetError.
	ErrFieldNotSet = errors.New("flowcore: params field not set")

	// The instance-side sentinels, for the Engine.
	//
	// ErrDefinitionHasNoInitialStep: the definition names no entry step, so there
	// is nowhere to start. Like the Create pre-flight sentinels it carries no
	// fields — it maps no database rejection, and the only detail is the
	// definition id the caller just supplied. It is checked after the definition
	// is read rather than before, which is the only way it differs from them.
	ErrDefinitionHasNoInitialStep = errors.New("flowcore: definition has no initial step")

	// ErrActiveWorkflowExists is returned when a workflow is already running for
	// a {subject, definition}. See ActiveWorkflowExistsError.
	ErrActiveWorkflowExists = errors.New("flowcore: an active workflow already exists for this subject and definition")
	// ErrVisitNotOpen is returned when the step visit being completed has already
	// been completed. See VisitNotOpenError.
	ErrVisitNotOpen = errors.New("flowcore: step visit is already completed")
	// ErrActionNotAvailable is returned when the requested action does not belong
	// to the step being completed. See ActionNotAvailableError.
	ErrActionNotAvailable = errors.New("flowcore: action is not available on this step")
	// ErrInvalidIdentifier is returned when an opaque identifier is empty or too
	// long. See InvalidIdentifierError.
	ErrInvalidIdentifier = errors.New("flowcore: invalid identifier")
)

// Entity labels carried on errors so a caller can name the offending kind.
const (
	entityWorkflowDefinition = "workflow definition"
	entityStatus             = "status"
	entityStep               = "step"
	entityAction             = "action"
	entityWorkflow           = "workflow"
	entityStepVisit          = "step visit"
)

// NotFoundError reports that a targeted row does not exist. Wraps ErrNotFound.
type NotFoundError struct {
	Entity string
	ID     uuid.UUID
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("flowcore: %s %s not found", e.Entity, e.ID)
}
func (e *NotFoundError) Unwrap() error { return ErrNotFound }

// DuplicateNameError reports a name collision within a scope (statuses and steps
// are unique per definition, actions per step; all case-insensitive). Wraps
// ErrDuplicateName. Name is the value that collided, in the caller's original
// casing.
type DuplicateNameError struct {
	Entity string
	Name   string
}

func (e *DuplicateNameError) Error() string {
	return fmt.Sprintf("flowcore: %s name %q already exists", e.Entity, e.Name)
}
func (e *DuplicateNameError) Unwrap() error { return ErrDuplicateName }

// CrossDefinitionError reports that a step's status, or an action's next step or
// terminal status, references a row in a different definition. Wraps
// ErrCrossDefinition.
type CrossDefinitionError struct{}

func (e *CrossDefinitionError) Error() string {
	return "flowcore: reference to a row in another definition"
}
func (e *CrossDefinitionError) Unwrap() error { return ErrCrossDefinition }

// ReferencedError reports that a row cannot be deleted because another row still
// references it — a step used as another action's next step or as the entry
// step, or a status used by a step or a terminal action. Wraps ErrReferenced.
type ReferencedError struct {
	Entity string
	ID     uuid.UUID
}

func (e *ReferencedError) Error() string {
	return fmt.Sprintf("flowcore: %s %s is still referenced and cannot be deleted", e.Entity, e.ID)
}
func (e *ReferencedError) Unwrap() error { return ErrReferenced }

// InvalidActionError reports that an action does not have exactly one of a next
// step or a terminal status. Wraps ErrInvalidAction.
type InvalidActionError struct{}

func (e *InvalidActionError) Error() string {
	return "flowcore: action must have exactly one of a next step or a terminal status"
}
func (e *InvalidActionError) Unwrap() error { return ErrInvalidAction }

// InvalidNameError reports that a name is empty or longer than 200 characters.
// Wraps ErrInvalidName.
type InvalidNameError struct{}

func (e *InvalidNameError) Error() string {
	return "flowcore: name must be between 1 and 200 characters"
}
func (e *InvalidNameError) Unwrap() error { return ErrInvalidName }

// UnmappedConstraintError reports a constraint violation whose constraint name
// the mapper does not recognize — the "fail loudly on the unexpected" signal,
// deliberately distinct from the domain errors so an unanticipated constraint
// surfaces (in CI, or to a caller) rather than being guessed at. Unwrap returns
// both ErrUnmappedConstraint (for errors.Is) and the underlying pgconn error
// (so errors.As can still recover the raw detail for diagnosis).
type UnmappedConstraintError struct {
	Constraint string
	Code       string
	cause      error
}

func (e *UnmappedConstraintError) Error() string {
	return fmt.Sprintf("flowcore: unmapped database constraint %q (SQLSTATE %s): %v", e.Constraint, e.Code, e.cause)
}
func (e *UnmappedConstraintError) Unwrap() []error { return []error{ErrUnmappedConstraint, e.cause} }

// FieldNotSetError reports a Nullable params field the caller never decided.
// Unlike the other pre-flight errors it carries a field name, because the type is
// reusable and "which field" is genuine per-occurrence detail — a departure from
// decision 16's field-less pre-flight sentinels, made deliberately. Wraps
// ErrFieldNotSet.
//
// The message names ToUpdate on purpose. The quickest way to silence this error
// is Clear, which destroys the stored value the error exists to protect, so the
// error has to point at the remedy that preserves it.
type FieldNotSetError struct {
	Field string
}

func (e *FieldNotSetError) Error() string {
	return fmt.Sprintf(
		"flowcore: %s was not set; decide it with SetTo or Clear, or build the params "+
			"with ToUpdate() to carry the stored value forward",
		e.Field)
}
func (e *FieldNotSetError) Unwrap() error { return ErrFieldNotSet }

// WorkflowNotFoundError reports that no workflow exists for a {subject,
// definition}. Wraps ErrNotFound, so a caller branching coarsely on
// errors.Is(err, ErrNotFound) treats it alongside every other not-found.
//
// It exists because NotFoundError carries a single uuid, and this lookup key is a
// subject reference and a definition id. Reusing NotFoundError would report a
// definition id while claiming to name a workflow, which reads as a library bug
// to anyone debugging from the message.
type WorkflowNotFoundError struct {
	SubjectReference     string
	WorkflowDefinitionID uuid.UUID
}

func (e *WorkflowNotFoundError) Error() string {
	return fmt.Sprintf("flowcore: no workflow for subject %q on definition %s",
		e.SubjectReference, e.WorkflowDefinitionID)
}
func (e *WorkflowNotFoundError) Unwrap() error { return ErrNotFound }

// ActiveWorkflowExistsError reports that a workflow is already running for this
// subject and definition. Only one may be active at a time, so the subject
// identifies a single run; finishing the existing one allows a new start. Wraps
// ErrActiveWorkflowExists.
//
// This is not a DuplicateNameError: a subject reference is not a name, and the
// pair that collided is a subject and a definition rather than a name in a scope.
type ActiveWorkflowExistsError struct {
	SubjectReference     string
	WorkflowDefinitionID uuid.UUID
}

func (e *ActiveWorkflowExistsError) Error() string {
	return fmt.Sprintf("flowcore: an active workflow already exists for subject %q on definition %s",
		e.SubjectReference, e.WorkflowDefinitionID)
}
func (e *ActiveWorkflowExistsError) Unwrap() error { return ErrActiveWorkflowExists }

// VisitNotOpenError reports that the step visit being completed is already
// closed, which means the run advanced after the caller last looked at it. Wraps
// ErrVisitNotOpen.
//
// It is deliberately distinct from NotFoundError, which a completely unknown
// visit id yields instead: the remedies differ. This one says the caller's view
// is stale and re-reading the state will show where the run actually is; a
// not-found says the id itself is wrong.
type VisitNotOpenError struct {
	VisitID uuid.UUID
}

func (e *VisitNotOpenError) Error() string {
	return fmt.Sprintf("flowcore: step visit %s is already completed; re-read the workflow state", e.VisitID)
}
func (e *VisitNotOpenError) Unwrap() error { return ErrVisitNotOpen }

// ActionNotAvailableError reports that the requested action does not belong to
// the step being completed. Wraps ErrActionNotAvailable.
//
// The action usually does exist — on some other step — so this is not a
// not-found. The likely cause is a caller acting on a stale view that still
// offers the actions of a step the run has since left.
type ActionNotAvailableError struct {
	ActionID uuid.UUID
	StepID   uuid.UUID
}

func (e *ActionNotAvailableError) Error() string {
	return fmt.Sprintf("flowcore: action %s is not available on step %s", e.ActionID, e.StepID)
}
func (e *ActionNotAvailableError) Unwrap() error { return ErrActionNotAvailable }

// InvalidIdentifierError reports that an opaque identifier is empty or longer
// than 500 characters. Wraps ErrInvalidIdentifier.
//
// Field names the column that failed, because one type serves every opaque
// identifier the library stores — subject reference, subject version token,
// assignee, completedBy — and which one is genuine per-occurrence detail. It is
// kept separate from InvalidNameError because the two limits differ: a
// human-facing name caps at 200, an opaque identifier at 500, so one message
// cannot state both truthfully.
type InvalidIdentifierError struct {
	Field string
}

func (e *InvalidIdentifierError) Error() string {
	return fmt.Sprintf("flowcore: %s must be between 1 and 500 characters", e.Field)
}
func (e *InvalidIdentifierError) Unwrap() error { return ErrInvalidIdentifier }
