package flowcore

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Engine runs workflow instances. Definitions are authored through Catalog;
// Engine starts runs from them and advances them.
//
// It owns the instance-side transactions, and the isolation levels differ between
// its two write paths on purpose — see Start and Complete.
type Engine struct {
	pool *pgxpool.Pool
}

// NewEngine returns an Engine over the given pool.
func NewEngine(pool *pgxpool.Pool) *Engine { return &Engine{pool: pool} }

// Start begins a run of a definition for a subject, snapshots the definition's
// graph, and opens the first step visit at the entry step. It returns where the
// run now stands.
//
// The transaction is REPEATABLE READ, and that is this method's own correctness
// condition rather than general caution. readDefinition is four separate queries;
// under read committed they can straddle a concurrent Catalog edit and snapshot a
// definition that never existed as a whole — statuses from before an edit, steps
// from after. Get has the same exposure and decision 12 took the same wrapper for
// it, but the damage here is worse: Get returns a bad answer once, while Start
// freezes one into a run permanently, and that run is then the source of truth
// for something incoherent.
//
// Repeatable read is safe here in a way it is not for Complete: Postgres raises
// 40001 on a write-write conflict against a row another transaction updated, and
// Start only inserts rows whose ids it just generated. Its one contention point
// is ux_workflow_active, which surfaces as ActiveWorkflowExistsError.
func (e *Engine) Start(ctx context.Context, params StartParams) (WorkflowState, error) {
	tx, err := e.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return WorkflowState{}, err
	}

	defer func() { _ = tx.Rollback(ctx) }()

	definition, err := readDefinition(ctx, tx, params.WorkflowDefinitionID)
	if err != nil {
		return WorkflowState{}, err
	}

	if definition.InitialStepDefinitionID == nil {
		return WorkflowState{}, ErrDefinitionHasNoInitialStep
	}

	snapshot := buildSnapshot(definition, params)
	if err := writeSnapshot(ctx, tx, snapshot); err != nil {
		return WorkflowState{}, err
	}

	state, err := readState(ctx, tx, snapshot.workflow.ID)
	if err != nil {
		return WorkflowState{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return WorkflowState{}, mapWriteErr(err, "")
	}

	return state, nil
}

// CompleteStep closes the open step visit with the caller's decision and advances the
// run — to the action's next step, or to the end if the action is terminal. It
// returns where the run now stands.
//
// The transaction is READ COMMITTED, deliberately, and repeatable read would be a
// defect here. Two concurrent completions of one visit both target the same row:
// under read committed the loser blocks, re-evaluates `completed_at is null`
// against the committed row, matches nothing, and is told its view is stale —
// VisitNotOpenError, the error the whole visit-id design exists to produce. Under
// repeatable read the same race raises 40001, which has no member in the taxonomy
// and no retry logic behind it. Probed both ways.
func (e *Engine) CompleteStep(ctx context.Context, params CompleteParams) (WorkflowState, error) {
	tx, err := e.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return WorkflowState{}, err
	}

	defer func() { _ = tx.Rollback(ctx) }()

	// The conditional update is the gate: it closes the visit only if it was open,
	// so nothing below runs against a run someone else already advanced.
	closed, err := completeStepVisit(ctx, tx,
		params.VisitID, params.CompletedBy, params.ActionID, params.SubjectVersionToken)
	if err != nil {
		return WorkflowState{}, err
	}

	// Scoped to the closed visit's step, so an action from elsewhere is refused
	// here rather than at commit — see getActionForStep.
	action, err := getActionForStep(ctx, tx, params.ActionID, closed.StepID)
	if err != nil {
		return WorkflowState{}, err
	}

	if err := advance(ctx, tx, closed.WorkflowID, action); err != nil {
		return WorkflowState{}, err
	}

	state, err := readState(ctx, tx, closed.WorkflowID)
	if err != nil {
		return WorkflowState{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return WorkflowState{}, mapWriteErr(err, "")
	}

	return state, nil
}

// GetState returns where a run stands. When several runs exist for the subject
// and definition, it is the most recent — the live one while one is in flight,
// the last finished one otherwise.
//
// Repeatable read plus read-only: the state and its actions are separate queries,
// so a concurrent Complete landing between them would otherwise return the status
// from before a transition beside the step from after it. A read-only snapshot
// cannot fail to serialize, so this costs no error class.
func (e *Engine) GetState(ctx context.Context, subjectReference string, workflowDefinitionID uuid.UUID) (WorkflowState, error) {
	tx, err := e.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return WorkflowState{}, err
	}

	defer func() { _ = tx.Rollback(ctx) }()

	workflowID, err := getWorkflowIDBySubject(ctx, tx, subjectReference, workflowDefinitionID)
	if err != nil {
		return WorkflowState{}, err
	}

	state, err := readState(ctx, tx, workflowID)
	if err != nil {
		return WorkflowState{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return WorkflowState{}, err
	}

	return state, nil
}

// GetHistory returns every visit the run has made, oldest first, including the
// open one. A step reached twice by a loop appears twice.
//
// No transaction, unlike GetState. The two statements are a lookup and then a
// read keyed on the id it returned, so a concurrent write cannot tear the result:
// once resolved, that run's history is read whole by one query. A run completing
// and another starting in between yields the earlier run's history — complete and
// self-consistent, merely a moment stale, which is indistinguishable from having
// been called a moment sooner.
func (e *Engine) GetHistory(ctx context.Context, subjectReference string, workflowDefinitionID uuid.UUID) ([]StepVisit, error) {
	workflowID, err := getWorkflowIDBySubject(ctx, e.pool, subjectReference, workflowDefinitionID)
	if err != nil {
		return nil, err
	}

	return listStepVisits(ctx, e.pool, workflowID)
}

// advance applies the routing decision the completed action carries: either open
// the next visit and stamp the step's status, or close the run in the action's
// terminal status.
//
// The next visit's assignee comes from the snapshot step, not from the definition,
// which is what makes a second visit reset to the default as it stood at start
// rather than pick up a later edit.
func advance(ctx context.Context, q txQuerier, workflowID uuid.UUID, action actionRow) error {
	if action.IsTerminal() {
		return completeWorkflow(ctx, q, workflowID,
			*action.TerminalWorkflowStatusDefinitionID, *action.TerminalWorkflowStatusName)
	}

	nextStep, err := getStep(ctx, q, *action.NextStepID)
	if err != nil {
		return err
	}

	visit := stepVisitRow{
		ID:         uuid.Must(uuid.NewV7()),
		WorkflowID: workflowID,
		StepID:     nextStep.ID,
		AssigneeID: nextStep.AssigneeID,
	}
	if err := insertStepVisit(ctx, q, visit); err != nil {
		return err
	}

	return updateWorkflowStatus(ctx, q, workflowID,
		nextStep.WorkflowStatusDefinitionID, nextStep.WorkflowStatusName)
}

// readState assembles the projection from its two reads. It contains no SQL and
// no transaction: every caller already holds one, which is what lets Start and
// Complete read their own uncommitted writes back and return a canonical value
// rather than one assembled in Go from what they believe they wrote.
func readState(ctx context.Context, q txQuerier, workflowID uuid.UUID) (WorkflowState, error) {
	state, err := getWorkflowState(ctx, q, workflowID)
	if err != nil {
		return WorkflowState{}, err
	}

	if state.CurrentStep == nil {
		return state, nil
	}

	actions, err := listActionsByStep(ctx, q, state.CurrentStep.ID)
	if err != nil {
		return WorkflowState{}, err
	}

	state.CurrentStep.Actions = actions

	return state, nil
}

// workflowSnapshot is the whole instance-side write of a Start, assembled in
// memory before any of it is sent.
type workflowSnapshot struct {
	workflow   workflowRow
	steps      []stepRow
	actions    []actionRow
	firstVisit stepVisitRow
}

// buildSnapshot turns a definition into the rows that freeze it. It is
// tree-shaped and contains no SQL, mirroring fillIDs on the definition side.
//
// Two maps do the work. Snapshot step ids are generated for every step before any
// action is built, so an action can point at the snapshot id of a step declared
// later — the same reason ids are application-generated at all. Status names are
// resolved from the definition's statuses, because the instance side stores the
// name beside the id rather than referencing a status table.
//
// Neither lookup can miss: composite foreign keys guarantee a step's status and an
// action's targets belong to this same definition. If one somehow did, the zero
// value would be an empty name, which the instance length CHECKs reject rather
// than store.
func buildSnapshot(definition WorkflowDefinition, params StartParams) workflowSnapshot {
	statusNames := make(map[uuid.UUID]string, len(definition.Statuses))
	for _, status := range definition.Statuses {
		statusNames[status.ID] = status.Name
	}

	workflowID := uuid.Must(uuid.NewV7())

	stepIDs := make(map[uuid.UUID]uuid.UUID, len(definition.Steps))
	for _, step := range definition.Steps {
		stepIDs[step.ID] = uuid.Must(uuid.NewV7())
	}

	snapshot := workflowSnapshot{
		steps:   make([]stepRow, 0, len(definition.Steps)),
		actions: make([]actionRow, 0),
	}

	var entryStep stepRow
	for _, step := range definition.Steps {
		row := stepRow{
			ID:                         stepIDs[step.ID],
			WorkflowID:                 workflowID,
			StepDefinitionID:           step.ID,
			Name:                       step.Name,
			WorkflowStatusDefinitionID: step.WorkflowStatusDefinitionID,
			WorkflowStatusName:         statusNames[step.WorkflowStatusDefinitionID],
			AssigneeID:                 step.AssigneeID,
		}
		if step.ID == *definition.InitialStepDefinitionID {
			entryStep = row
		}

		snapshot.steps = append(snapshot.steps, row)

		for _, action := range step.Actions {
			snapshot.actions = append(snapshot.actions, buildActionRow(action, workflowID, row.ID, stepIDs, statusNames))
		}
	}

	snapshot.workflow = workflowRow{
		ID:                         workflowID,
		WorkflowDefinitionID:       definition.ID,
		Name:                       definition.Name,
		SubjectReference:           params.SubjectReference,
		SubjectVersionToken:        params.SubjectVersionToken,
		WorkflowStatusDefinitionID: entryStep.WorkflowStatusDefinitionID,
		WorkflowStatusName:         entryStep.WorkflowStatusName,
	}

	snapshot.firstVisit = stepVisitRow{
		ID:         uuid.Must(uuid.NewV7()),
		WorkflowID: workflowID,
		StepID:     entryStep.ID,
		AssigneeID: entryStep.AssigneeID,
	}

	return snapshot
}

func buildActionRow(
	action ActionDefinition,
	workflowID uuid.UUID,
	stepID uuid.UUID,
	stepIDs map[uuid.UUID]uuid.UUID,
	statusNames map[uuid.UUID]string,
) actionRow {
	row := actionRow{
		ID:                 uuid.Must(uuid.NewV7()),
		WorkflowID:         workflowID,
		StepID:             stepID,
		ActionDefinitionID: action.ID,
		Name:               action.Name,
	}

	if action.NextStepDefinitionID != nil {
		nextStepID := stepIDs[*action.NextStepDefinitionID]
		row.NextStepID = &nextStepID
	}

	if action.TerminalWorkflowStatusDefinitionID != nil {
		terminalStatusID := *action.TerminalWorkflowStatusDefinitionID
		terminalStatusName := statusNames[terminalStatusID]
		row.TerminalWorkflowStatusDefinitionID = &terminalStatusID
		row.TerminalWorkflowStatusName = &terminalStatusName
	}

	return row
}

// writeSnapshot sends the assembled rows in dependency order: the workflow, then
// its steps, then their actions, then the visit that opens the run.
func writeSnapshot(ctx context.Context, q txQuerier, snapshot workflowSnapshot) error {
	if err := insertWorkflow(ctx, q, snapshot.workflow); err != nil {
		return err
	}

	for _, step := range snapshot.steps {
		if err := insertStep(ctx, q, step); err != nil {
			return err
		}
	}

	for _, action := range snapshot.actions {
		if err := insertAction(ctx, q, action); err != nil {
			return err
		}
	}

	return insertStepVisit(ctx, q, snapshot.firstVisit)
}
