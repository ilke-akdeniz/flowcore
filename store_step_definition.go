package flowcore

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// rowToStep scans a step's own columns; Actions is left nil (not loaded) and is
// populated separately by the deep read or the mutating methods' return path.
func rowToStep(row pgx.CollectableRow) (StepDefinition, error) {
	var step StepDefinition
	err := row.Scan(&step.ID, &step.WorkflowDefinitionID, &step.WorkflowStatusDefinitionID, &step.AssigneeID, &step.Name)
	return step, err
}

func insertStep(ctx context.Context, q querier, step StepDefinition) error {
	_, err := q.Exec(ctx,
		`insert into flowcore.step_definition
		 (id, workflow_definition_id, workflow_status_definition_id, assignee_id, name)
		 values ($1, $2, $3, $4, $5)`,
		step.ID, step.WorkflowDefinitionID, step.WorkflowStatusDefinitionID, step.AssigneeID, step.Name)
	return mapWriteErr(err, step.Name)
}

func getStepRow(ctx context.Context, q querier, id uuid.UUID) (StepDefinition, error) {
	var step StepDefinition
	err := q.QueryRow(ctx,
		`select id, workflow_definition_id, workflow_status_definition_id, assignee_id, name
		 from flowcore.step_definition where id = $1`,
		id).Scan(&step.ID, &step.WorkflowDefinitionID, &step.WorkflowStatusDefinitionID, &step.AssigneeID, &step.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return StepDefinition{}, &NotFoundError{Entity: entityStep, ID: id}
	}
	if err != nil {
		return StepDefinition{}, err
	}
	return step, nil
}

func listStepsByDefinition(ctx context.Context, q querier, definitionID uuid.UUID) ([]StepDefinition, error) {
	rows, err := q.Query(ctx,
		`select id, workflow_definition_id, workflow_status_definition_id, assignee_id, name
		 from flowcore.step_definition where workflow_definition_id = $1 order by name`,
		definitionID)
	if err != nil {
		return nil, err
	}
	steps, err := pgx.CollectRows(rows, rowToStep)
	if err != nil {
		return nil, err
	}
	if steps == nil {
		steps = []StepDefinition{}
	}
	return steps, nil
}

func updateStep(ctx context.Context, q querier, id uuid.UUID, p UpdateStepParams) error {
	tag, err := q.Exec(ctx,
		`update flowcore.step_definition
		 set name = $2, workflow_status_definition_id = $3, assignee_id = $4
		 where id = $1`,
		id, p.Name, p.StatusID, p.AssigneeID)
	if err != nil {
		return mapWriteErr(err, p.Name)
	}
	if tag.RowsAffected() == 0 {
		return &NotFoundError{Entity: entityStep, ID: id}
	}
	return nil
}

func deleteStep(ctx context.Context, q querier, id uuid.UUID) error {
	tag, err := q.Exec(ctx, `delete from flowcore.step_definition where id = $1`, id)
	if err != nil {
		return mapDeleteErr(err, entityStep, id)
	}
	if tag.RowsAffected() == 0 {
		return &NotFoundError{Entity: entityStep, ID: id}
	}
	return nil
}
