package flowcore

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Catalog creates and edits workflow definitions. Create writes a whole
// definition tree in one transaction; every other method is granular per-entity
// CRUD. It holds the pool and owns definition-side transactions.
type Catalog struct {
	pool *pgxpool.Pool
}

// NewCatalog returns a Catalog backed by pool.
func NewCatalog(pool *pgxpool.Pool) *Catalog {
	return &Catalog{pool: pool}
}

// Create writes a complete definition — its statuses, steps, and actions, plus
// the entry step — in a single transaction, so a reader never sees a
// half-built definition. The caller's value is not mutated; the returned value
// is the stored definition, read back canonically.
//
// Ids are optional and generated (UUIDv7) when omitted; ids supplied by the
// caller are kept, which is how an action references a step declared elsewhere
// in the same tree. InitialStepDefinitionID is optional too: when unset, the
// first step in Steps becomes the entry step; when set, it must reference one of
// the steps in Steps. A definition with no steps cannot be created.
func (c *Catalog) Create(ctx context.Context, definition WorkflowDefinition) (WorkflowDefinition, error) {
	if len(definition.Steps) == 0 {
		return WorkflowDefinition{}, ErrNoSteps
	}

	definition = definition.clone()
	if err := fillIDs(&definition); err != nil {
		return WorkflowDefinition{}, err
	}

	// Resolve the entry step after fillIDs, so the default uses Steps[0]'s final
	// id whether the caller supplied it or the library generated it. An explicit
	// id is checked against the tree here, before the transaction: otherwise a
	// mismatch surfaces only as a deferred-FK violation at commit, mapped to the
	// unhelpful CrossDefinitionError.
	if definition.InitialStepDefinitionID == nil {
		definition.InitialStepDefinitionID = &definition.Steps[0].ID
	} else if !stepExists(definition.Steps, *definition.InitialStepDefinitionID) {
		return WorkflowDefinition{}, ErrInitialStepNotInTree
	}

	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return WorkflowDefinition{}, err
	}

	defer func() { _ = tx.Rollback(ctx) }()

	if err := insertWorkflowDefinition(ctx, tx, definition.ID, definition.Name); err != nil {
		return WorkflowDefinition{}, err
	}

	for _, status := range definition.Statuses {
		if err := insertStatus(ctx, tx, status); err != nil {
			return WorkflowDefinition{}, err
		}
	}

	for _, step := range definition.Steps {
		if err := insertStep(ctx, tx, step); err != nil {
			return WorkflowDefinition{}, err
		}

		for _, action := range step.Actions {
			if err := insertAction(ctx, tx, action); err != nil {
				return WorkflowDefinition{}, err
			}
		}
	}

	if err := setInitialStep(ctx, tx, definition.ID, *definition.InitialStepDefinitionID); err != nil {
		return WorkflowDefinition{}, err
	}

	// Read back within the same transaction, so the return value is canonical
	// (ordered, non-nil slices) and reflects exactly what was written.
	result, err := readDefinition(ctx, tx, definition.ID)
	if err != nil {
		return WorkflowDefinition{}, err
	}

	// The reference FKs are deferred, so a malformed reference in the input
	// (e.g. an action pointing at a step that is not in this tree) surfaces here
	// at commit, not at the offending insert. Map it like any other write error.
	if err := tx.Commit(ctx); err != nil {
		return WorkflowDefinition{}, mapWriteErr(err, "")
	}

	return result, nil
}

// Get returns the whole definition tree — statuses, steps, and each step's
// actions. It reads inside a repeatable-read transaction so its four queries are
// one consistent snapshot: a concurrent edit cannot tear the read.
func (c *Catalog) Get(ctx context.Context, id uuid.UUID) (WorkflowDefinition, error) {
	tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return WorkflowDefinition{}, err
	}

	defer func() { _ = tx.Rollback(ctx) }()

	definition, err := readDefinition(ctx, tx, id)
	if err != nil {
		return WorkflowDefinition{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return WorkflowDefinition{}, err
	}

	return definition, nil
}

// DeleteWorkflowDefinition removes a definition and, by cascade, all its
// statuses, steps, and actions. Returns NotFoundError if no such definition.
func (c *Catalog) DeleteWorkflowDefinition(ctx context.Context, id uuid.UUID) error {
	return deleteWorkflowDefinition(ctx, c.pool, id)
}

// AddStatus adds a status to a definition. The definition must exist
// (NotFoundError otherwise), enforced by the parent foreign key at insert rather
// than by a pre-flight read: a read cannot establish that the definition still
// exists by the time the insert runs, so the constraint is the only check that
// actually holds.
func (c *Catalog) AddStatus(ctx context.Context, workflowDefinitionID uuid.UUID, p AddStatusParams) (WorkflowStatusDefinition, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return WorkflowStatusDefinition{}, err
	}

	status := WorkflowStatusDefinition{ID: id, WorkflowDefinitionID: workflowDefinitionID, Name: p.Name}
	if err := insertStatus(ctx, c.pool, status); err != nil {
		return WorkflowStatusDefinition{}, err
	}

	return status, nil
}

// UpdateStatus replaces a status's mutable columns and returns the stored row.
// The write and the read-back are a single statement, so the returned value is
// this call's own write even under a concurrent update.
func (c *Catalog) UpdateStatus(ctx context.Context, statusID uuid.UUID, p UpdateStatusParams) (WorkflowStatusDefinition, error) {
	return updateStatus(ctx, c.pool, statusID, p)
}

// DeleteStatus removes a status. Returns ReferencedError if a step uses it or an
// action ends in it, NotFoundError if no such status.
func (c *Catalog) DeleteStatus(ctx context.Context, statusID uuid.UUID) error {
	return deleteStatus(ctx, c.pool, statusID)
}

// AddStep adds a step to a definition. The definition must exist (NotFoundError
// otherwise), enforced as in AddStatus. The returned step has an empty, non-nil
// Actions slice: it is loaded and has no actions yet.
func (c *Catalog) AddStep(ctx context.Context, workflowDefinitionID uuid.UUID, p AddStepParams) (StepDefinition, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return StepDefinition{}, err
	}

	step := StepDefinition{
		ID:                         id,
		WorkflowDefinitionID:       workflowDefinitionID,
		WorkflowStatusDefinitionID: p.StatusID,
		AssigneeID:                 p.AssigneeID,
		Name:                       p.Name,
	}
	if err := insertStep(ctx, c.pool, step); err != nil {
		return StepDefinition{}, err
	}

	step.Actions = []ActionDefinition{}

	return step, nil
}

// UpdateStep replaces a step's own mutable columns (name, status, assignee) and
// returns the stored step with its actions re-fetched and populated. It does not
// read or change the step's actions as an input: actions are managed through
// AddAction/UpdateAction/DeleteAction.
//
// The step's own columns come back from the update statement itself, so they are
// always this call's own write. The actions are a second query, so a concurrent
// AddAction or DeleteAction can be reflected in the returned slice — deliberate,
// since this method never writes actions, and an action set read a moment later
// is indistinguishable from one changed a moment after this call returned.
func (c *Catalog) UpdateStep(ctx context.Context, stepID uuid.UUID, p UpdateStepParams) (StepDefinition, error) {
	step, err := updateStep(ctx, c.pool, stepID, p)
	if err != nil {
		return StepDefinition{}, err
	}

	actions, err := listActionsByStep(ctx, c.pool, stepID)
	if err != nil {
		return StepDefinition{}, err
	}

	step.Actions = actions

	return step, nil
}

// DeleteStep removes a step and, by cascade, its actions. Returns ReferencedError
// if another action routes to it or it is the entry step, NotFoundError if no
// such step.
func (c *Catalog) DeleteStep(ctx context.Context, stepID uuid.UUID) error {
	return deleteStep(ctx, c.pool, stepID)
}

// AddAction adds an action to a step. Exactly one of NextStepID / TerminalStatusID
// must be set. The step must exist (NotFoundError otherwise).
//
// Unlike AddStatus and AddStep, this reads its parent step first — not as an
// existence check, but because the action carries the step's
// workflow_definition_id and there is nowhere else to get it. The read is
// therefore permanent. It is safe to race: if the step is deleted between the
// read and the insert, the parent foreign key produces the same NotFoundError
// the read would have.
func (c *Catalog) AddAction(ctx context.Context, stepDefinitionID uuid.UUID, p AddActionParams) (ActionDefinition, error) {
	step, err := getStepRow(ctx, c.pool, stepDefinitionID)
	if err != nil {
		return ActionDefinition{}, err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return ActionDefinition{}, err
	}

	action := ActionDefinition{
		ID:                                 id,
		WorkflowDefinitionID:               step.WorkflowDefinitionID,
		StepDefinitionID:                   stepDefinitionID,
		Name:                               p.Name,
		NextStepDefinitionID:               p.NextStepID,
		TerminalWorkflowStatusDefinitionID: p.TerminalStatusID,
	}
	if err := insertAction(ctx, c.pool, action); err != nil {
		return ActionDefinition{}, err
	}

	return action, nil
}

// UpdateAction replaces an action's mutable columns and returns the stored row.
// The exactly-one next-step / terminal-status rule is enforced. As with
// UpdateStatus, the write and the read-back are a single statement.
func (c *Catalog) UpdateAction(ctx context.Context, actionID uuid.UUID, p UpdateActionParams) (ActionDefinition, error) {
	return updateAction(ctx, c.pool, actionID, p)
}

// DeleteAction removes an action. Nothing references an action, so this only
// fails with NotFoundError if no such action.
func (c *Catalog) DeleteAction(ctx context.Context, actionID uuid.UUID) error {
	return deleteAction(ctx, c.pool, actionID)
}

// readDefinition assembles the deep tree from four queries on q. Shared by Get
// (in a repeatable-read snapshot) and Create (reading its own writes before
// commit). Every returned slice is non-nil: an empty slice means "loaded, none".
func readDefinition(ctx context.Context, q querier, id uuid.UUID) (WorkflowDefinition, error) {
	definition, err := getWorkflowDefinitionRow(ctx, q, id)
	if err != nil {
		return WorkflowDefinition{}, err
	}

	statuses, err := listStatusesByDefinition(ctx, q, id)
	if err != nil {
		return WorkflowDefinition{}, err
	}

	steps, err := listStepsByDefinition(ctx, q, id)
	if err != nil {
		return WorkflowDefinition{}, err
	}

	actions, err := listActionsByDefinition(ctx, q, id)
	if err != nil {
		return WorkflowDefinition{}, err
	}

	byStep := make(map[uuid.UUID][]ActionDefinition, len(steps))
	for _, action := range actions {
		byStep[action.StepDefinitionID] = append(byStep[action.StepDefinitionID], action)
	}

	for i := range steps {
		stepActions := byStep[steps[i].ID]
		if stepActions == nil {
			stepActions = []ActionDefinition{}
		}

		steps[i].Actions = stepActions
	}

	definition.Statuses = statuses
	definition.Steps = steps

	return definition, nil
}

// clone returns a deep-enough copy that filling ids and parent links never
// mutates the caller's slices. Pointer fields are shared but never written
// through.
func (def WorkflowDefinition) clone() WorkflowDefinition {
	out := def
	out.Statuses = append([]WorkflowStatusDefinition(nil), def.Statuses...)
	out.Steps = make([]StepDefinition, len(def.Steps))
	for i, step := range def.Steps {
		cp := step
		cp.Actions = append([]ActionDefinition(nil), step.Actions...)
		out.Steps[i] = cp
	}

	return out
}

// fillIDs generates any zero id and stamps parent links from the tree structure,
// so the caller only supplies ids for entities it references.
func fillIDs(d *WorkflowDefinition) error {
	var err error
	if d.ID, err = ensureID(d.ID); err != nil {
		return err
	}

	for i := range d.Statuses {
		if d.Statuses[i].ID, err = ensureID(d.Statuses[i].ID); err != nil {
			return err
		}

		d.Statuses[i].WorkflowDefinitionID = d.ID
	}

	for i := range d.Steps {
		if d.Steps[i].ID, err = ensureID(d.Steps[i].ID); err != nil {
			return err
		}

		d.Steps[i].WorkflowDefinitionID = d.ID

		for j := range d.Steps[i].Actions {
			if d.Steps[i].Actions[j].ID, err = ensureID(d.Steps[i].Actions[j].ID); err != nil {
				return err
			}

			d.Steps[i].Actions[j].WorkflowDefinitionID = d.ID
			d.Steps[i].Actions[j].StepDefinitionID = d.Steps[i].ID
		}
	}

	return nil
}

func ensureID(id uuid.UUID) (uuid.UUID, error) {
	if id != uuid.Nil {
		return id, nil
	}

	return uuid.NewV7()
}

func stepExists(steps []StepDefinition, id uuid.UUID) bool {
	for i := range steps {
		if steps[i].ID == id {
			return true
		}
	}

	return false
}
