package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/mike-akdeniz/flowcore"
)

// ErrNotYours is returned when a session touches a definition it does not own.
//
// This check is the client's job and nothing enforces it below. FlowCore will
// happily delete any status whose id you name — it has no tenant, no owner and no
// session, exactly as designed. Every method here therefore resolves the
// definition through the session first, and a child id that is not in that
// definition is refused before the library is called at all.
var ErrNotYours = errors.New("that workflow belongs to another session")

// Definition reads one of this session's workflow definitions.
func (a *App) Definition(ctx context.Context, sessionID string, definitionID uuid.UUID) (flowcore.WorkflowDefinition, error) {
	owned, err := a.owns(ctx, sessionID, definitionID)
	if err != nil {
		return flowcore.WorkflowDefinition{}, err
	}

	if !owned {
		return flowcore.WorkflowDefinition{}, ErrNotYours
	}

	return a.Catalog.Get(ctx, definitionID)
}

// Definitions reads every definition this session owns.
func (a *App) Definitions(ctx context.Context, sessionID string) ([]flowcore.WorkflowDefinition, error) {
	registered, err := a.Store.RegisteredWorkflows(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	definitions := make([]flowcore.WorkflowDefinition, 0, len(registered))
	for _, workflow := range registered {
		definition, err := a.Catalog.Get(ctx, workflow.FlowcoreDefinitionID)
		if err != nil {
			return nil, err
		}

		definitions = append(definitions, definition)
	}

	return definitions, nil
}

// owns reports whether this session registered that definition.
//
// Authorization is the console's job and nothing below enforces it: FlowCore will
// happily return any definition whose id you name, because it has no tenant, no
// owner and no session. Every method here resolves ownership through the registry
// before touching the library.
func (a *App) owns(ctx context.Context, sessionID string, definitionID uuid.UUID) (bool, error) {
	registered, err := a.Store.RegisteredWorkflows(ctx, sessionID)
	if err != nil {
		return false, err
	}

	for _, workflow := range registered {
		if workflow.FlowcoreDefinitionID == definitionID {
			return true, nil
		}
	}

	return false, nil
}

// NewDefinition is what the "new workflow" form collects.
//
// It asks for a first status and a first step as well as a name, because FlowCore
// rejects a definition with no steps (ErrNoSteps) — a workflow that cannot be
// started is not a state it will store. So there is no "create it empty and fill
// it in" path, and the form has to open with three fields rather than one.
type NewDefinition struct {
	Name       string
	StatusName string
	StepName   string
	AssigneeID string
}

// CreateDefinition creates a workflow and records it against this session.
func (a *App) CreateDefinition(ctx context.Context, sessionID string, request NewDefinition) (flowcore.WorkflowDefinition, error) {
	statusID := uuid.Must(uuid.NewV7())
	stepID := uuid.Must(uuid.NewV7())

	definition, err := a.Catalog.Create(ctx, flowcore.WorkflowDefinition{
		Name:                    request.Name,
		InitialStepDefinitionID: &stepID,
		Statuses: []flowcore.WorkflowStatusDefinition{
			{ID: statusID, Name: request.StatusName},
		},
		Steps: []flowcore.StepDefinition{
			{
				ID:                         stepID,
				WorkflowStatusDefinitionID: statusID,
				Name:                       request.StepName,
				AssigneeID:                 request.AssigneeID,
			},
		},
	})
	if err != nil {
		return flowcore.WorkflowDefinition{}, err
	}

	return definition, nil
}

// The child operations below all take the definition id as well as the child's,
// so ownership can be checked before anything is written. The extra read is the
// price of the library having no opinion about who owns what.

func (a *App) AddStatus(ctx context.Context, sessionID string, definitionID uuid.UUID, name string) error {
	if err := a.mustOwn(ctx, sessionID, definitionID); err != nil {
		return err
	}

	_, err := a.Catalog.AddStatus(ctx, definitionID, flowcore.AddStatusParams{Name: name})

	return err
}

func (a *App) DeleteStatus(ctx context.Context, sessionID string, definitionID, statusID uuid.UUID) error {
	if err := a.mustContain(ctx, sessionID, definitionID, statusID, containsStatus); err != nil {
		return err
	}

	return a.Catalog.DeleteStatus(ctx, statusID)
}

// AddStepRequest is the settable shape of a step.
type AddStepRequest struct {
	Name       string
	StatusID   uuid.UUID
	AssigneeID string
}

func (a *App) AddStep(ctx context.Context, sessionID string, definitionID uuid.UUID, request AddStepRequest) error {
	if err := a.mustOwn(ctx, sessionID, definitionID); err != nil {
		return err
	}

	_, err := a.Catalog.AddStep(ctx, definitionID, flowcore.AddStepParams{
		Name:       request.Name,
		StatusID:   request.StatusID,
		AssigneeID: request.AssigneeID,
	})

	return err
}

func (a *App) UpdateStep(ctx context.Context, sessionID string, definitionID, stepID uuid.UUID, request AddStepRequest) error {
	if err := a.mustContain(ctx, sessionID, definitionID, stepID, containsStep); err != nil {
		return err
	}

	// Update is a full replace, so every column the params list is written. There
	// is no "change only the name" — the form posts all three fields, and building
	// these by hand while omitting one would overwrite it.
	_, err := a.Catalog.UpdateStep(ctx, stepID, flowcore.UpdateStepParams{
		Name:       request.Name,
		StatusID:   request.StatusID,
		AssigneeID: request.AssigneeID,
	})

	return err
}

func (a *App) DeleteStep(ctx context.Context, sessionID string, definitionID, stepID uuid.UUID) error {
	if err := a.mustContain(ctx, sessionID, definitionID, stepID, containsStep); err != nil {
		return err
	}

	return a.Catalog.DeleteStep(ctx, stepID)
}

// AddActionRequest routes either to a step or to a terminal status. Exactly one
// of the two is set, which the schema also enforces.
type AddActionRequest struct {
	Name             string
	NextStepID       *uuid.UUID
	TerminalStatusID *uuid.UUID
}

func (a *App) AddAction(ctx context.Context, sessionID string, definitionID, stepID uuid.UUID, request AddActionRequest) error {
	if err := a.mustContain(ctx, sessionID, definitionID, stepID, containsStep); err != nil {
		return err
	}

	_, err := a.Catalog.AddAction(ctx, stepID, flowcore.AddActionParams{
		Name:             request.Name,
		NextStepID:       request.NextStepID,
		TerminalStatusID: request.TerminalStatusID,
	})

	return err
}

func (a *App) DeleteAction(ctx context.Context, sessionID string, definitionID, actionID uuid.UUID) error {
	if err := a.mustContain(ctx, sessionID, definitionID, actionID, containsAction); err != nil {
		return err
	}

	return a.Catalog.DeleteAction(ctx, actionID)
}

// SetEntryStep changes where runs of this workflow begin.
func (a *App) SetEntryStep(ctx context.Context, sessionID string, definitionID, stepID uuid.UUID) error {
	definition, err := a.Definition(ctx, sessionID, definitionID)
	if err != nil {
		return err
	}

	if !containsStep(definition, stepID) {
		return ErrNotYours
	}

	_, err = a.Catalog.UpdateWorkflowDefinition(ctx, definitionID, flowcore.UpdateWorkflowDefinitionParams{
		Name:                    definition.Name,
		InitialStepDefinitionID: stepID,
	})

	return err
}

// RenameDefinition changes the workflow's own name, keeping its entry step.
func (a *App) RenameDefinition(ctx context.Context, sessionID string, definitionID uuid.UUID, name string) error {
	definition, err := a.Definition(ctx, sessionID, definitionID)
	if err != nil {
		return err
	}

	if definition.InitialStepDefinitionID == nil {
		return flowcore.ErrDefinitionHasNoInitialStep
	}

	// ToUpdate exists for exactly this: Update is a full replace, so carrying the
	// stored entry step forward by hand is how you avoid clearing it while
	// renaming something else.
	params := definition.ToUpdate()
	params.Name = name

	_, err = a.Catalog.UpdateWorkflowDefinition(ctx, definitionID, params)

	return err
}

type contains func(flowcore.WorkflowDefinition, uuid.UUID) bool

func containsStatus(definition flowcore.WorkflowDefinition, id uuid.UUID) bool {
	for _, status := range definition.Statuses {
		if status.ID == id {
			return true
		}
	}

	return false
}

func containsStep(definition flowcore.WorkflowDefinition, id uuid.UUID) bool {
	for _, step := range definition.Steps {
		if step.ID == id {
			return true
		}
	}

	return false
}

func containsAction(definition flowcore.WorkflowDefinition, id uuid.UUID) bool {
	for _, step := range definition.Steps {
		for _, action := range step.Actions {
			if action.ID == id {
				return true
			}
		}
	}

	return false
}

// mustContain is the authorization check in one place: the session owns the
// definition, and the child really belongs to it.
func (a *App) mustContain(ctx context.Context, sessionID string, definitionID, childID uuid.UUID, has contains) error {
	definition, err := a.Definition(ctx, sessionID, definitionID)
	if err != nil {
		return err
	}

	if !has(definition, childID) {
		return fmt.Errorf("%w: it is not part of %q", ErrNotYours, definition.Name)
	}

	return nil
}

// mustOwn is the ownership check where a caller needs only the error.
func (a *App) mustOwn(ctx context.Context, sessionID string, definitionID uuid.UUID) error {
	owned, err := a.owns(ctx, sessionID, definitionID)
	if err != nil {
		return err
	}

	if !owned {
		return ErrNotYours
	}

	return nil
}
