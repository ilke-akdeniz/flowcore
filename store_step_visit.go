package flowcore

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// stepVisitRow is a row of flowcore.step_visit: one entry into a step. The
// completion columns are set together or not at all, which the schema enforces;
// while the visit is open all four are null.
type stepVisitRow struct {
	ID                  uuid.UUID
	WorkflowID          uuid.UUID
	StepID              uuid.UUID
	AssigneeID          *string
	EnteredAt           time.Time
	CompletedAt         *time.Time
	CompletedBy         *string
	SelectedActionID    *uuid.UUID
	SubjectVersionToken *string
}

// insertStepVisit opens a visit: the run has just entered this step. EnteredAt
// comes from the database clock for the reason given on insertWorkflow.
//
// At most one visit per workflow may be open, enforced by ux_step_visit_open. A
// violation here means a caller inserted a second open visit without closing the
// first, which no code path should do — so it is left to surface as an
// UnmappedConstraintError rather than dressed up as a domain outcome.
func insertStepVisit(ctx context.Context, q querier, visit stepVisitRow) error {
	_, err := q.Exec(ctx,
		`insert into flowcore.step_visit
		 (id, workflow_id, step_id, assignee_id, entered_at,
		  completed_at, completed_by, selected_action_id, subject_version_token)
		 values ($1, $2, $3, $4, now(), null, null, null, null)`,
		visit.ID,
		visit.WorkflowID,
		visit.StepID,
		visit.AssigneeID)

	return mapWriteErr(err, "")
}

// completeStepVisit closes the open visit, stamping who acted, what they chose,
// and which revision of the subject they acted on. It returns the closed row, so
// the caller learns the step and workflow without a second read.
//
// The `completed_at is null` predicate is the gate, and it is what makes this one
// statement instead of a read followed by a write. Two concurrent completions of
// one visit serialize on the row lock: the loser re-evaluates the predicate after
// the winner commits, matches nothing, and is told its view is stale. There is no
// window between checking and writing because there is no check.
//
// Zero rows is therefore ambiguous, and the ambiguity is worth resolving: an
// unknown id is a caller bug, while a closed visit means the run moved on. A
// follow-up read distinguishes them, and it costs nothing on the success path.
func completeStepVisit(
	ctx context.Context,
	q querier,
	visitID uuid.UUID,
	completedBy string,
	actionID uuid.UUID,
	subjectVersionToken *string,
) (stepVisitRow, error) {
	var visit stepVisitRow
	err := q.QueryRow(ctx,
		`update flowcore.step_visit
		 set completed_at = now(), completed_by = $2, selected_action_id = $3, subject_version_token = $4
		 where id = $1 and completed_at is null
		 returning id, workflow_id, step_id, assignee_id, entered_at,
		           completed_at, completed_by, selected_action_id, subject_version_token`,
		visitID, completedBy, actionID, subjectVersionToken).Scan(
		&visit.ID,
		&visit.WorkflowID,
		&visit.StepID,
		&visit.AssigneeID,
		&visit.EnteredAt,
		&visit.CompletedAt,
		&visit.CompletedBy,
		&visit.SelectedActionID,
		&visit.SubjectVersionToken)
	if errors.Is(err, pgx.ErrNoRows) {
		return stepVisitRow{}, closedOrMissingVisit(ctx, q, visitID)
	}

	if err != nil {
		return stepVisitRow{}, mapWriteErr(err, "")
	}

	return visit, nil
}

// closedOrMissingVisit decides which error a zero-row completion deserves. If the
// diagnostic read itself fails, that error is returned instead — reporting a
// database failure as "already completed" would be a worse lie than losing the
// distinction.
func closedOrMissingVisit(ctx context.Context, q querier, visitID uuid.UUID) error {
	_, err := getStepVisit(ctx, q, visitID)
	if err != nil {
		return err
	}

	return &VisitNotOpenError{VisitID: visitID}
}

// getStepVisit reads one visit by id.
func getStepVisit(ctx context.Context, q querier, id uuid.UUID) (stepVisitRow, error) {
	var visit stepVisitRow
	err := q.QueryRow(ctx,
		`select id, workflow_id, step_id, assignee_id, entered_at,
		        completed_at, completed_by, selected_action_id, subject_version_token
		 from flowcore.step_visit where id = $1`,
		id).Scan(
		&visit.ID,
		&visit.WorkflowID,
		&visit.StepID,
		&visit.AssigneeID,
		&visit.EnteredAt,
		&visit.CompletedAt,
		&visit.CompletedBy,
		&visit.SelectedActionID,
		&visit.SubjectVersionToken)
	if errors.Is(err, pgx.ErrNoRows) {
		return stepVisitRow{}, &NotFoundError{Entity: entityStepVisit, ID: id}
	}

	if err != nil {
		return stepVisitRow{}, err
	}

	return visit, nil
}

// listStepVisits returns a run's history, oldest first, including the open visit.
//
// This is a single query rather than a read-plus-assemble because the joins are
// strictly one-to-one: a visit has one step, and at most one selected action. So
// there is no fan-out to deduplicate and no transaction to hold — unlike
// getWorkflowState, whose actions are one-to-many.
//
// Ordering by entered_at is what makes a loop legible: a step reached twice
// appears twice, in the order it was reached.
func listStepVisits(ctx context.Context, q querier, workflowID uuid.UUID) ([]StepVisit, error) {
	rows, err := q.Query(ctx,
		`select v.id, v.step_id, s.name, v.assignee_id, v.entered_at,
		        v.completed_at, v.completed_by, v.selected_action_id, a.name,
		        v.subject_version_token
		 from flowcore.step_visit v
		 join flowcore.step s on s.id = v.step_id
		 left join flowcore.action a on a.id = v.selected_action_id
		 where v.workflow_id = $1
		 order by v.entered_at, v.id`,
		workflowID)
	if err != nil {
		return nil, err
	}

	visits, err := pgx.CollectRows(rows, rowToStepVisit)
	if err != nil {
		return nil, err
	}

	if visits == nil {
		visits = []StepVisit{}
	}

	return visits, nil
}

// rowToStepVisit scans a visit and folds its completion columns into a single
// nested value, so that the all-or-nothing rule the schema enforces is the same
// rule the returned type expresses.
func rowToStepVisit(row pgx.CollectableRow) (StepVisit, error) {
	var (
		visit               StepVisit
		completedAt         *time.Time
		completedBy         *string
		selectedActionID    *uuid.UUID
		selectedActionName  *string
		subjectVersionToken *string
	)

	err := row.Scan(
		&visit.ID,
		&visit.StepID,
		&visit.StepName,
		&visit.AssigneeID,
		&visit.EnteredAt,
		&completedAt,
		&completedBy,
		&selectedActionID,
		&selectedActionName,
		&subjectVersionToken)
	if err != nil {
		return StepVisit{}, err
	}

	if completedAt != nil {
		visit.Completion = &Completion{
			At:                  *completedAt,
			By:                  *completedBy,
			ActionID:            *selectedActionID,
			ActionName:          *selectedActionName,
			SubjectVersionToken: subjectVersionToken,
		}
	}

	return visit, nil
}
