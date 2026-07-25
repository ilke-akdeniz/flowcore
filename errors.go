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
)

// Entity labels carried on errors so a caller can name the offending kind.
const (
	entityWorkflowDefinition = "workflow definition"
	entityStatus             = "status"
	entityStep               = "step"
	entityAction             = "action"
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
