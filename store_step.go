package flowcore

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// stepRow is a row of flowcore.step: one step of the frozen graph. It carries no
// execution state — whether the run has reached this step is answered by the
// presence of a step_visit row, never by a column here.
type stepRow struct {
	ID                         uuid.UUID
	WorkflowID                 uuid.UUID
	StepDefinitionID           uuid.UUID
	Name                       string
	WorkflowStatusDefinitionID uuid.UUID
	WorkflowStatusName         string
	AssigneeID                 *string
}

// insertStep writes one step of the snapshot. StepDefinitionID and the status id
// are recorded, not verified: they name definition rows the library deliberately
// does not constrain, so that a run survives the definition being edited or
// deleted.
func insertStep(ctx context.Context, q querier, step stepRow) error {
	_, err := q.Exec(ctx,
		`insert into flowcore.step
		 (id, workflow_id, step_definition_id, name,
		  workflow_status_definition_id, workflow_status_name, assignee_id)
		 values ($1, $2, $3, $4, $5, $6, $7)`,
		step.ID,
		step.WorkflowID,
		step.StepDefinitionID,
		step.Name,
		step.WorkflowStatusDefinitionID,
		step.WorkflowStatusName,
		step.AssigneeID)

	return mapWriteErr(err, step.Name)
}

// getStep reads a snapshot step. Complete uses it on a routing transition, for
// the two things the next visit needs: the status to stamp on the workflow, and
// the frozen default assignee to seed the visit with.
//
// Reading the default from here rather than from the step definition is what
// makes a second visit to a step reset to the definition's assignee as it stood
// at start, rather than picking up a later edit.
func getStep(ctx context.Context, q querier, id uuid.UUID) (stepRow, error) {
	var step stepRow
	err := q.QueryRow(ctx,
		`select id, workflow_id, step_definition_id, name,
		        workflow_status_definition_id, workflow_status_name, assignee_id
		 from flowcore.step where id = $1`,
		id).Scan(
		&step.ID,
		&step.WorkflowID,
		&step.StepDefinitionID,
		&step.Name,
		&step.WorkflowStatusDefinitionID,
		&step.WorkflowStatusName,
		&step.AssigneeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return stepRow{}, &NotFoundError{Entity: entityStep, ID: id}
	}

	if err != nil {
		return stepRow{}, err
	}

	return step, nil
}
