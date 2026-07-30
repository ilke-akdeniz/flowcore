package flowcore

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// actionRow is a row of flowcore.action: one action of the frozen graph. Exactly
// one of NextStepID or the terminal status pair is set, which the schema enforces.
type actionRow struct {
	ID                                 uuid.UUID
	WorkflowID                         uuid.UUID
	StepID                             uuid.UUID
	ActionDefinitionID                 uuid.UUID
	Name                               string
	NextStepID                         *uuid.UUID
	TerminalWorkflowStatusDefinitionID *uuid.UUID
	TerminalWorkflowStatusName         *string
}

// IsTerminal reports whether completing this action ends the run. Reading it off
// NextStepID rather than the terminal status is deliberate: the schema's XOR makes
// the two equivalent, and a nil next step is the end-of-flow signal the definition
// side already uses.
func (a actionRow) IsTerminal() bool { return a.NextStepID == nil }

// insertAction writes one action of the snapshot.
func insertAction(ctx context.Context, q querier, action actionRow) error {
	_, err := q.Exec(ctx,
		`insert into flowcore.action
		 (id, workflow_id, step_id, action_definition_id, name,
		  next_step_id, terminal_workflow_status_definition_id, terminal_workflow_status_name)
		 values ($1, $2, $3, $4, $5, $6, $7, $8)`,
		action.ID,
		action.WorkflowID,
		action.StepID,
		action.ActionDefinitionID,
		action.Name,
		action.NextStepID,
		action.TerminalWorkflowStatusDefinitionID,
		action.TerminalWorkflowStatusName)

	return mapWriteErr(err, action.Name)
}

// listActionsByStep returns the choices available on a step, as the projection a
// caller sees. Ordered by name so a run's presented options are stable across
// calls.
//
// An empty non-nil slice means the step has no actions — a dead end in the
// definition the run started from, which the definition side permits.
func listActionsByStep(ctx context.Context, q querier, stepID uuid.UUID) ([]Action, error) {
	rows, err := q.Query(ctx,
		`select id, name from flowcore.action where step_id = $1 order by name`,
		stepID)
	if err != nil {
		return nil, err
	}

	actions, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Action, error) {
		var action Action
		err := row.Scan(&action.ID, &action.Name)

		return action, err
	})
	if err != nil {
		return nil, err
	}

	if actions == nil {
		actions = []Action{}
	}

	return actions, nil
}

// getActionForStep reads an action, requiring it to belong to the given step, and
// returns ActionNotAvailableError when it does not.
//
// The scoping is not belt-and-braces: fk_step_visit_selected_action enforces the
// same pair, but it is DEFERRABLE INITIALLY DEFERRED, so it fires at commit rather
// than at the statement that set the column. Left to the constraint, Complete
// would compute routing from an action off some other step and only fail at
// commit, with a raw foreign-key error. Reading it scoped turns that into a
// precise error at the moment the decision is made, leaving the constraint as the
// backstop it should be.
func getActionForStep(ctx context.Context, q querier, actionID uuid.UUID, stepID uuid.UUID) (actionRow, error) {
	var action actionRow
	err := q.QueryRow(ctx,
		`select id, workflow_id, step_id, action_definition_id, name,
		        next_step_id, terminal_workflow_status_definition_id, terminal_workflow_status_name
		 from flowcore.action where id = $1 and step_id = $2`,
		actionID, stepID).Scan(
		&action.ID,
		&action.WorkflowID,
		&action.StepID,
		&action.ActionDefinitionID,
		&action.Name,
		&action.NextStepID,
		&action.TerminalWorkflowStatusDefinitionID,
		&action.TerminalWorkflowStatusName)
	if errors.Is(err, pgx.ErrNoRows) {
		return actionRow{}, &ActionNotAvailableError{ActionID: actionID, StepID: stepID}
	}

	if err != nil {
		return actionRow{}, err
	}

	return action, nil
}
