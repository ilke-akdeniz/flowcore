package flowcore

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// insertWorkflowDefinition writes the root row with a null entry step; the entry
// step is stamped by setInitialStepDefinition once the steps exist, inside the
// same aggregate transaction.
func insertWorkflowDefinition(ctx context.Context, q querier, id uuid.UUID, name string) error {
	_, err := q.Exec(ctx,
		`insert into flowcore.workflow_definition (id, name) values ($1, $2)`,
		id, name)

	return mapWriteErr(err, name)
}

// setInitialStepDefinition stamps the entry step. The step must belong to the
// same definition (enforced by fk_workflow_definition_initial_step); a mismatch
// surfaces as CrossDefinitionError.
func setInitialStepDefinition(ctx context.Context, q querier, definitionID, stepID uuid.UUID) error {
	_, err := q.Exec(ctx,
		`update flowcore.workflow_definition set initial_step_definition_id = $2 where id = $1`,
		definitionID, stepID)

	return mapWriteErr(err, "")
}

// updateWorkflowDefinition replaces the definition's own scalar columns and
// returns the stored row in one statement, for the reason given on
// updateStatusDefinition. Statuses and Steps are not touched and are left nil;
// they are managed through their own methods.
//
// An entry step belonging to another definition surfaces as CrossDefinitionError.
// That check is deferred, so it fires at the statement's implicit commit rather
// than as the row is produced — the returned row is discarded when it does.
func updateWorkflowDefinition(ctx context.Context, q querier, id uuid.UUID, p UpdateWorkflowDefinitionParams) (WorkflowDefinition, error) {
	var definition WorkflowDefinition
	err := q.QueryRow(ctx,
		`update flowcore.workflow_definition
		 set name = $2, initial_step_definition_id = $3
		 where id = $1
		 returning id, name, initial_step_definition_id`,
		id, p.Name, p.InitialStepDefinitionID).Scan(
		&definition.ID, &definition.Name, &definition.InitialStepDefinitionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkflowDefinition{}, &NotFoundError{Entity: entityWorkflowDefinition, ID: id}
	}

	if err != nil {
		return WorkflowDefinition{}, mapWriteErr(err, p.Name)
	}

	return definition, nil
}

func getWorkflowDefinitionRow(ctx context.Context, q querier, id uuid.UUID) (WorkflowDefinition, error) {
	var definition WorkflowDefinition
	err := q.QueryRow(ctx,
		`select id, name, initial_step_definition_id from flowcore.workflow_definition where id = $1`,
		id).Scan(&definition.ID, &definition.Name, &definition.InitialStepDefinitionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkflowDefinition{}, &NotFoundError{Entity: entityWorkflowDefinition, ID: id}
	}

	if err != nil {
		return WorkflowDefinition{}, err
	}

	return definition, nil
}

// deleteWorkflowDefinition removes the definition; the schema's cascades clear
// its statuses, steps, and actions. Deferred reference FKs let the whole cascade
// resolve before their checks run.
func deleteWorkflowDefinition(ctx context.Context, q querier, id uuid.UUID) error {
	tag, err := q.Exec(ctx, `delete from flowcore.workflow_definition where id = $1`, id)
	if err != nil {
		return mapDeleteErr(err, entityWorkflowDefinition, id)
	}

	if tag.RowsAffected() == 0 {
		return &NotFoundError{Entity: entityWorkflowDefinition, ID: id}
	}

	return nil
}
