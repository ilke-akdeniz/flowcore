package flowcore

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func rowToAction(row pgx.CollectableRow) (ActionDefinition, error) {
	var action ActionDefinition
	err := row.Scan(&action.ID, &action.WorkflowDefinitionID, &action.StepDefinitionID, &action.Name,
		&action.NextStepDefinitionID, &action.TerminalWorkflowStatusDefinitionID)

	return action, err
}

// insertAction writes an action. A missing parent step surfaces from the parent
// foreign key as NotFoundError, for the reason given on insertStatus — which is
// what makes AddAction's read of that step safe to race: if the step is deleted
// between the read and this insert, the caller still gets not-found.
func insertAction(ctx context.Context, q querier, action ActionDefinition) error {
	_, err := q.Exec(ctx,
		`insert into flowcore.action_definition
		 (id, workflow_definition_id, step_definition_id, name,
		  next_step_definition_id, terminal_workflow_status_definition_id)
		 values ($1, $2, $3, $4, $5, $6)`,
		action.ID, action.WorkflowDefinitionID, action.StepDefinitionID, action.Name,
		action.NextStepDefinitionID, action.TerminalWorkflowStatusDefinitionID)

	return mapInsertErr(err, action.Name, entityStep, action.StepDefinitionID)
}

// listActionsByDefinition returns every action in a definition, ordered so a
// deep read can group them under their steps. The action table carries
// workflow_definition_id (denormalized), so this needs no join.
func listActionsByDefinition(ctx context.Context, q querier, definitionID uuid.UUID) ([]ActionDefinition, error) {
	rows, err := q.Query(ctx,
		`select id, workflow_definition_id, step_definition_id, name,
		        next_step_definition_id, terminal_workflow_status_definition_id
		 from flowcore.action_definition
		 where workflow_definition_id = $1 order by step_definition_id, name`,
		definitionID)
	if err != nil {
		return nil, err
	}

	return collectActions(rows)
}

func listActionsByStep(ctx context.Context, q querier, stepID uuid.UUID) ([]ActionDefinition, error) {
	rows, err := q.Query(ctx,
		`select id, workflow_definition_id, step_definition_id, name,
		        next_step_definition_id, terminal_workflow_status_definition_id
		 from flowcore.action_definition
		 where step_definition_id = $1 order by name`,
		stepID)
	if err != nil {
		return nil, err
	}

	return collectActions(rows)
}

func collectActions(rows pgx.Rows) ([]ActionDefinition, error) {
	actions, err := pgx.CollectRows(rows, rowToAction)
	if err != nil {
		return nil, err
	}

	if actions == nil {
		actions = []ActionDefinition{}
	}

	return actions, nil
}

// updateAction replaces the action's mutable columns and returns the stored row
// in one statement, for the reason given on updateStatus. Its routing columns
// sit behind deferred foreign keys, so a cross-definition reference is rejected
// at the statement's implicit commit — after RETURNING has produced its row, but
// still surfaced from this call, so mapWriteErr sees it as it always has.
func updateAction(ctx context.Context, q querier, id uuid.UUID, p UpdateActionParams) (ActionDefinition, error) {
	var action ActionDefinition
	err := q.QueryRow(ctx,
		`update flowcore.action_definition
		 set name = $2, next_step_definition_id = $3, terminal_workflow_status_definition_id = $4
		 where id = $1
		 returning id, workflow_definition_id, step_definition_id, name,
		           next_step_definition_id, terminal_workflow_status_definition_id`,
		id, p.Name, p.NextStepID, p.TerminalStatusID).Scan(
		&action.ID, &action.WorkflowDefinitionID, &action.StepDefinitionID, &action.Name,
		&action.NextStepDefinitionID, &action.TerminalWorkflowStatusDefinitionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ActionDefinition{}, &NotFoundError{Entity: entityAction, ID: id}
	}

	if err != nil {
		return ActionDefinition{}, mapWriteErr(err, p.Name)
	}

	return action, nil
}

func deleteAction(ctx context.Context, q querier, id uuid.UUID) error {
	tag, err := q.Exec(ctx, `delete from flowcore.action_definition where id = $1`, id)
	if err != nil {
		return mapDeleteErr(err, entityAction, id)
	}

	if tag.RowsAffected() == 0 {
		return &NotFoundError{Entity: entityAction, ID: id}
	}

	return nil
}
