package app

import (
	"errors"
	"fmt"

	"github.com/mike-akdeniz/flowcore"
)

// ErrorMessage turns a FlowCore error into a sentence someone editing a workflow
// can act on.
//
// This function is the reason a library's error taxonomy is worth designing
// carefully, and the reason that design is invisible until something builds a
// real interface on top of it. Every branch below exists because the library
// returns a distinct type rather than one opaque error, and each one produces
// different advice.
//
// Note what is not here: no string matching on error text, and no inspection of
// the underlying database error. The library maps constraint violations to typed
// errors so its callers never have to know what a constraint is, and this is what
// that buys.
func ErrorMessage(err error) string {
	if err == nil {
		return ""
	}

	// The typed errors first, because they carry detail worth showing.
	var duplicate *flowcore.DuplicateNameError
	if errors.As(err, &duplicate) {
		return fmt.Sprintf(
			"There is already a %s called %q here. Names have to be unique within a "+
				"workflow, ignoring case.", duplicate.Entity, duplicate.Name)
	}

	var referenced *flowcore.ReferencedError
	if errors.As(err, &referenced) {
		return fmt.Sprintf(
			"That %s is still pointed at by something else — an action routes to it, "+
				"or a step uses it as its status. Repoint whatever refers to it first.",
			referenced.Entity)
	}

	var identifier *flowcore.InvalidIdentifierError
	if errors.As(err, &identifier) {
		return fmt.Sprintf(
			"%s has to be between 1 and 500 characters. It is an opaque reference — "+
				"FlowCore never interprets it — but it cannot be empty.", identifier.Field)
	}

	var notFound *flowcore.NotFoundError
	if errors.As(err, &notFound) {
		return fmt.Sprintf("That %s no longer exists. Someone may have deleted it.", notFound.Entity)
	}

	// Then the sentinels, for the errors that carry no detail because there is
	// only one way to cause them.
	switch {
	case errors.Is(err, flowcore.ErrCrossDefinition):
		return "That belongs to a different workflow. A step's status and an action's " +
			"destination both have to live in the same workflow as the thing referring to them."

	case errors.Is(err, flowcore.ErrInvalidName):
		return "A name has to be between 1 and 200 characters."

	case errors.Is(err, flowcore.ErrInvalidAction):
		return "An action goes either to another step or to a terminal status — one or " +
			"the other, never both and never neither."

	case errors.Is(err, flowcore.ErrNoSteps):
		return "A workflow needs at least one step before it can be created."

	case errors.Is(err, flowcore.ErrInitialStepNotInTree):
		return "The starting step has to be one of this workflow's own steps."

	case errors.Is(err, flowcore.ErrDefinitionHasNoInitialStep):
		return "This workflow has no starting step, so a run cannot begin. Pick one below."

	case errors.Is(err, flowcore.ErrActiveWorkflowExists):
		return "There is already a run of this workflow for that subject. Only one can be " +
			"open at a time; finish it before starting another."

	case errors.Is(err, flowcore.ErrVisitNotOpen):
		return "That step was already completed — the run has moved on since this page was " +
			"loaded. Reload to see where it stands now."

	case errors.Is(err, flowcore.ErrActionNotAvailable):
		return "That action does not belong to the step the run is on."

	case errors.Is(err, flowcore.ErrInvalidRemark):
		return "A remark has to be between 1 and 3000 characters — a sentence or a page. " +
			"Anything longer is a document, and documents belong in the application, not here."

	case errors.Is(err, flowcore.ErrUnmappedConstraint):
		// This one means the library hit a constraint it does not map, which is a
		// defect in the library rather than a mistake by whoever is using the form.
		return "Something went wrong inside FlowCore that it did not expect. This is a " +
			"bug worth reporting rather than something you did."
	}

	return "Something went wrong: " + err.Error()
}
