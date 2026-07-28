package flowcore

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// rowToStepDefinition scans a step's own columns; Actions is left nil (not
// loaded) and is populated separately by the deep read or the mutating methods'
// return path.
func rowToStepDefinition(row pgx.CollectableRow) (StepDefinition, error) {
	var step StepDefinition
	err := row.Scan(&step.ID, &step.WorkflowDefinitionID, &step.WorkflowStatusDefinitionID, &step.AssigneeID, &step.Name)

	return step, err
}

// insertStepDefinition writes a step. A missing parent definition surfaces from
// the parent foreign key as NotFoundError, for the reason given on
// insertStatusDefinition. That check is immediate while the status reference FK
// is deferred, so when both are violated the missing parent wins — the more
// fundamental of the two.
func insertStepDefinition(ctx context.Context, q querier, step StepDefinition) error {
	_, err := q.Exec(ctx,
		`insert into flowcore.step_definition
		 (id, workflow_definition_id, workflow_status_definition_id, assignee_id, name)
		 values ($1, $2, $3, $4, $5)`,
		step.ID, step.WorkflowDefinitionID, step.WorkflowStatusDefinitionID, step.AssigneeID, step.Name)

	return mapInsertErr(err, step.Name, entityWorkflowDefinition, step.WorkflowDefinitionID)
}

func getStepDefinitionRow(ctx context.Context, q querier, id uuid.UUID) (StepDefinition, error) {
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

func listStepDefinitionsByWorkflowDefinition(ctx context.Context, q querier, definitionID uuid.UUID) ([]StepDefinition, error) {
	rows, err := q.Query(ctx,
		`select id, workflow_definition_id, workflow_status_definition_id, assignee_id, name
		 from flowcore.step_definition where workflow_definition_id = $1 order by name`,
		definitionID)
	if err != nil {
		return nil, err
	}

	steps, err := pgx.CollectRows(rows, rowToStepDefinition)
	if err != nil {
		return nil, err
	}

	if steps == nil {
		steps = []StepDefinition{}
	}

	return steps, nil
}

// updateStepDefinition replaces the step's own mutable columns and returns the
// stored row in one statement, for the reason given on updateStatusDefinition.
// Actions are not touched and are left nil; the caller populates them.
func updateStepDefinition(ctx context.Context, q querier, id uuid.UUID, p UpdateStepParams) (StepDefinition, error) {
	var step StepDefinition
	err := q.QueryRow(ctx,
		`update flowcore.step_definition
		 set name = $2, workflow_status_definition_id = $3, assignee_id = $4
		 where id = $1
		 returning id, workflow_definition_id, workflow_status_definition_id, assignee_id, name`,
		id, p.Name, p.StatusID, p.AssigneeID).Scan(
		&step.ID, &step.WorkflowDefinitionID, &step.WorkflowStatusDefinitionID, &step.AssigneeID, &step.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return StepDefinition{}, &NotFoundError{Entity: entityStep, ID: id}
	}

	if err != nil {
		return StepDefinition{}, mapWriteErr(err, p.Name)
	}

	return step, nil
}

func deleteStepDefinition(ctx context.Context, q querier, id uuid.UUID) error {
	tag, err := q.Exec(ctx, `delete from flowcore.step_definition where id = $1`, id)
	if err != nil {
		return mapDeleteErr(err, entityStep, id)
	}

	if tag.RowsAffected() == 0 {
		return &NotFoundError{Entity: entityStep, ID: id}
	}

	return nil
}
