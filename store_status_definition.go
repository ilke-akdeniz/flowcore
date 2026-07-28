package flowcore

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func rowToStatus(row pgx.CollectableRow) (WorkflowStatusDefinition, error) {
	var status WorkflowStatusDefinition
	err := row.Scan(&status.ID, &status.WorkflowDefinitionID, &status.Name)

	return status, err
}

// insertStatus writes a status. A missing parent definition surfaces from the
// parent foreign key as NotFoundError rather than from a pre-flight read, so a
// definition deleted concurrently reads as not-found and not as an internal
// defect.
func insertStatus(ctx context.Context, q querier, status WorkflowStatusDefinition) error {
	_, err := q.Exec(ctx,
		`insert into flowcore.workflow_status_definition (id, workflow_definition_id, name)
		 values ($1, $2, $3)`,
		status.ID, status.WorkflowDefinitionID, status.Name)

	return mapInsertErr(err, status.Name, entityWorkflowDefinition, status.WorkflowDefinitionID)
}

func listStatusesByDefinition(ctx context.Context, q querier, definitionID uuid.UUID) ([]WorkflowStatusDefinition, error) {
	rows, err := q.Query(ctx,
		`select id, workflow_definition_id, name from flowcore.workflow_status_definition
		 where workflow_definition_id = $1 order by name`,
		definitionID)
	if err != nil {
		return nil, err
	}

	statuses, err := pgx.CollectRows(rows, rowToStatus)
	if err != nil {
		return nil, err
	}

	if statuses == nil {
		statuses = []WorkflowStatusDefinition{}
	}

	return statuses, nil
}

// updateStatus replaces the status's mutable columns and returns the stored row.
// RETURNING makes the read-back part of the write itself, so the returned value
// is always this statement's own write and never a concurrent caller's; a
// separate select could observe another writer's row. Zero rows returned means
// no such status, which is why not-found arrives as pgx.ErrNoRows here rather
// than through a RowsAffected check.
func updateStatus(ctx context.Context, q querier, id uuid.UUID, p UpdateStatusParams) (WorkflowStatusDefinition, error) {
	var status WorkflowStatusDefinition
	err := q.QueryRow(ctx,
		`update flowcore.workflow_status_definition set name = $2
		 where id = $1
		 returning id, workflow_definition_id, name`,
		id, p.Name).Scan(&status.ID, &status.WorkflowDefinitionID, &status.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkflowStatusDefinition{}, &NotFoundError{Entity: entityStatus, ID: id}
	}

	if err != nil {
		return WorkflowStatusDefinition{}, mapWriteErr(err, p.Name)
	}

	return status, nil
}

func deleteStatus(ctx context.Context, q querier, id uuid.UUID) error {
	tag, err := q.Exec(ctx, `delete from flowcore.workflow_status_definition where id = $1`, id)
	if err != nil {
		return mapDeleteErr(err, entityStatus, id)
	}

	if tag.RowsAffected() == 0 {
		return &NotFoundError{Entity: entityStatus, ID: id}
	}

	return nil
}
