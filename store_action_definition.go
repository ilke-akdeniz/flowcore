package flowcore

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func rowToAction(row pgx.CollectableRow) (ActionDefinition, error) {
	var a ActionDefinition
	err := row.Scan(&a.ID, &a.WorkflowDefinitionID, &a.StepDefinitionID, &a.Name,
		&a.NextStepDefinitionID, &a.TerminalWorkflowStatusDefinitionID)
	return a, err
}

func insertAction(ctx context.Context, q querier, a ActionDefinition) error {
	_, err := q.Exec(ctx,
		`insert into flowcore.action_definition
		 (id, workflow_definition_id, step_definition_id, name,
		  next_step_definition_id, terminal_workflow_status_definition_id)
		 values ($1, $2, $3, $4, $5, $6)`,
		a.ID, a.WorkflowDefinitionID, a.StepDefinitionID, a.Name,
		a.NextStepDefinitionID, a.TerminalWorkflowStatusDefinitionID)
	return mapWriteErr(err, a.Name)
}

func getActionRow(ctx context.Context, q querier, id uuid.UUID) (ActionDefinition, error) {
	var a ActionDefinition
	err := q.QueryRow(ctx,
		`select id, workflow_definition_id, step_definition_id, name,
		        next_step_definition_id, terminal_workflow_status_definition_id
		 from flowcore.action_definition where id = $1`,
		id).Scan(&a.ID, &a.WorkflowDefinitionID, &a.StepDefinitionID, &a.Name,
		&a.NextStepDefinitionID, &a.TerminalWorkflowStatusDefinitionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ActionDefinition{}, &NotFoundError{Entity: entityAction, ID: id}
	}
	if err != nil {
		return ActionDefinition{}, err
	}
	return a, nil
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

func updateAction(ctx context.Context, q querier, id uuid.UUID, p UpdateActionParams) error {
	tag, err := q.Exec(ctx,
		`update flowcore.action_definition
		 set name = $2, next_step_definition_id = $3, terminal_workflow_status_definition_id = $4
		 where id = $1`,
		id, p.Name, p.NextStepID, p.TerminalStatusID)
	if err != nil {
		return mapWriteErr(err, p.Name)
	}
	if tag.RowsAffected() == 0 {
		return &NotFoundError{Entity: entityAction, ID: id}
	}
	return nil
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
