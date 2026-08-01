package flowcore

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The instance-side store. Hand-written SQL over the four instance tables, as
// free functions handed a querier, exactly like the definition-side helpers.
//
// The row structs here mirror table rows and are unexported: no caller receives
// one. What a client gets back are the projections in instance_types.go, which
// span several tables and are assembled a layer above. The Row suffix is the
// reminder of which is which.

// workflowRow is a row of flowcore.workflow.
type workflowRow struct {
	ID                         uuid.UUID
	WorkflowDefinitionID       uuid.UUID
	Name                       string
	SubjectReference           string
	SubjectVersionToken        *string
	WorkflowStatusDefinitionID uuid.UUID
	WorkflowStatusName         string
	StartedAt                  time.Time
	CompletedAt                *time.Time
}

// insertWorkflow writes the workflow row that starts a run. StartedAt is taken
// from the database clock, not from the struct, so every timestamp in a run comes
// from one clock regardless of which process wrote it.
//
// A second active run for the same {subject, definition} surfaces as
// ActiveWorkflowExistsError.
func insertWorkflow(ctx context.Context, q querier, workflow workflowRow) error {
	_, err := q.Exec(ctx,
		`insert into flowcore.workflow
		 (id, workflow_definition_id, name, subject_reference, subject_version_token,
		  workflow_status_definition_id, workflow_status_name, started_at, completed_at)
		 values ($1, $2, $3, $4, $5, $6, $7, now(), null)`,
		workflow.ID,
		workflow.WorkflowDefinitionID,
		workflow.Name,
		workflow.SubjectReference,
		workflow.SubjectVersionToken,
		workflow.WorkflowStatusDefinitionID,
		workflow.WorkflowStatusName)

	return mapWorkflowInsertErr(err, workflow.SubjectReference, workflow.WorkflowDefinitionID)
}

// getWorkflowIDBySubject resolves a {subject, definition} to the run it refers
// to: the most recently started one.
//
// A pair accumulates a row per run over its lifetime — ux_workflow_active
// constrains only open rows, so a finished run releases the pair for a new one.
// Matching on the pair alone therefore matches history as well as the present,
// and would silently return whichever row the planner produced first.
//
// "Most recently started" and "the open run, or the last finished one" are the
// same rule rather than two: no run can start while another is open, so an open
// run is necessarily the most recent. That gives the live run while one is in
// flight, and the final state of the last run once none is — which is what both
// Get Current Step and the history read want.
//
// The id tie-break matters only if two runs share a started_at to the microsecond;
// UUIDv7 being time-ordered makes it the right direction.
func getWorkflowIDBySubject(ctx context.Context, q querier, subjectReference string, workflowDefinitionID uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := q.QueryRow(ctx,
		`select id from flowcore.workflow
		 where subject_reference = $1 and workflow_definition_id = $2
		 order by started_at desc, id desc
		 limit 1`,
		subjectReference, workflowDefinitionID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, &WorkflowNotFoundError{
			SubjectReference:     subjectReference,
			WorkflowDefinitionID: workflowDefinitionID,
		}
	}

	if err != nil {
		return uuid.Nil, err
	}

	return id, nil
}

// getWorkflowState reads where a run stands: the workflow's own columns joined to
// its open step visit and that visit's step.
//
// It is keyed on the workflow id rather than on the subject because Start and
// Complete both hold an id already — Start generated it, Complete took it off the
// visit it just closed — and because keying it on the subject would put the
// most-recent rule in two places instead of one. Only GetState resolves, through
// getWorkflowIDBySubject.
//
// The join to the visit and step is a left join because a finished run has no
// open visit — that is the only shape in which "complete, no current step" is
// representable, and it is why the scan targets for those columns are pointers.
//
// CurrentStep.Actions is left nil; the caller loads it with listActionsByStep.
// That mirrors listStepDefinitionsByWorkflowDefinition, which likewise returns
// steps with Actions unloaded.
func getWorkflowState(ctx context.Context, q querier, workflowID uuid.UUID) (WorkflowState, error) {
	var (
		state          WorkflowState
		visitID        *uuid.UUID
		stepID         *uuid.UUID
		stepName       *string
		stepAssigneeID *string
		enteredAt      *time.Time
	)

	err := q.QueryRow(ctx,
		`select w.id, w.name, w.subject_reference, w.subject_version_token,
		        w.workflow_status_definition_id, w.workflow_status_name,
		        w.started_at, w.completed_at,
		        v.id, s.id, s.name, v.assignee_id, v.entered_at
		 from flowcore.workflow w
		 left join flowcore.step_visit v
		        on v.workflow_id = w.id and v.completed_at is null
		 left join flowcore.step s on s.id = v.step_id
		 where w.id = $1`,
		workflowID).Scan(
		&state.ID,
		&state.Name,
		&state.SubjectReference,
		&state.SubjectVersionToken,
		&state.WorkflowStatusDefinitionID,
		&state.WorkflowStatusName,
		&state.StartedAt,
		&state.CompletedAt,
		&visitID,
		&stepID,
		&stepName,
		&stepAssigneeID,
		&enteredAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkflowState{}, &NotFoundError{Entity: entityWorkflow, ID: workflowID}
	}

	if err != nil {
		return WorkflowState{}, err
	}

	if visitID != nil {
		state.CurrentStep = &CurrentStep{
			ID:         *stepID,
			VisitID:    *visitID,
			Name:       *stepName,
			AssigneeID: stepAssigneeID,
			EnteredAt:  *enteredAt,
		}
	}

	return state, nil
}

// updateWorkflowStatus stamps the status a routing transition moved the run into.
// It does not touch completed_at: the run is still open.
func updateWorkflowStatus(ctx context.Context, q querier, workflowID uuid.UUID, statusDefinitionID uuid.UUID, statusName string) error {
	tag, err := q.Exec(ctx,
		`update flowcore.workflow
		 set workflow_status_definition_id = $2, workflow_status_name = $3
		 where id = $1`,
		workflowID, statusDefinitionID, statusName)
	if err != nil {
		return mapWriteErr(err, "")
	}

	if tag.RowsAffected() == 0 {
		return &NotFoundError{Entity: entityWorkflow, ID: workflowID}
	}

	return nil
}

// completeWorkflow stamps the terminating action's status and closes the run.
// Setting completed_at is what releases the {subject, definition} pair for a new
// run, since the active-workflow index covers only open rows.
func completeWorkflow(ctx context.Context, q querier, workflowID uuid.UUID, statusDefinitionID uuid.UUID, statusName string) error {
	tag, err := q.Exec(ctx,
		`update flowcore.workflow
		 set workflow_status_definition_id = $2, workflow_status_name = $3, completed_at = now()
		 where id = $1`,
		workflowID, statusDefinitionID, statusName)
	if err != nil {
		return mapWriteErr(err, "")
	}

	if tag.RowsAffected() == 0 {
		return &NotFoundError{Entity: entityWorkflow, ID: workflowID}
	}

	return nil
}
