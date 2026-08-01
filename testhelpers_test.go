package flowcore

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultTestDSN = "postgres://flowcore:flowcore@localhost:5432/flowcore_test"

var testPool *pgxpool.Pool

// TestMain establishes the shared, migrated test database for the whole package.
//
// The suite runs integration tests against a real Postgres, sharing one migrated
// schema and isolating by truncating before each test, so it MUST run serially:
// every DB-touching test lives in this package and none calls t.Parallel().
// Adding t.Parallel() later would let tests truncate each other's rows — do not.
//
// The schema is assumed pre-migrated (goose is not imported); `make test` runs
// migrate-test first. When no test database is reachable, tests skip rather than
// fail, so `go test ./...` works on a machine without Postgres.
func TestMain(m *testing.M) {
	dsn := os.Getenv("FLOWCORE_TEST_DSN")
	if dsn == "" {
		dsn = defaultTestDSN
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if pool, err := pgxpool.New(ctx, dsn); err == nil {
		if schemaReady(ctx, pool) {
			testPool = pool
		} else {
			pool.Close()
		}
	}

	code := m.Run()
	if testPool != nil {
		testPool.Close()
	}

	os.Exit(code)
}

// schemaReady is true only if the pool connects and the migrated schema exists,
// so a reachable-but-unmigrated database skips rather than failing obscurely.
func schemaReady(ctx context.Context, pool *pgxpool.Pool) bool {
	if err := pool.Ping(ctx); err != nil {
		return false
	}

	var exists bool
	err := pool.QueryRow(ctx,
		`select exists (
			select 1 from information_schema.tables
			where table_schema = 'flowcore' and table_name = 'workflow_definition'
		)`).Scan(&exists)

	return err == nil && exists
}

// newCatalog skips the test if no migrated test database is reachable, truncates
// every table (setup, not teardown — a failed test leaves its rows for
// inspection), and returns a Catalog on the shared pool.
func newCatalog(t *testing.T) *Catalog {
	t.Helper()
	truncateAll(t)

	return NewCatalog(testPool)
}

// newEngine is newCatalog's instance-side twin, returning both services on the
// shared pool after one truncate — an Engine test almost always needs a Catalog
// first, to author the definition it starts a run from.
func newEngine(t *testing.T) (*Engine, *Catalog) {
	t.Helper()
	truncateAll(t)

	return NewEngine(testPool), NewCatalog(testPool)
}

// truncateAll names flowcore.workflow as well as flowcore.workflow_definition,
// and that is load-bearing rather than belt-and-braces. TRUNCATE ... CASCADE
// follows foreign keys, and there is deliberately none from workflow to
// workflow_definition — instance rows record the definition they came from
// without referencing it, so a run survives its definition being deleted.
// Truncating only the definition side therefore leaves every instance row
// behind, and the leak is not quiet: the next test to start a run for the same
// subject fails on ux_workflow_active instead of on anything it asserts.
//
// workflow does reach step, action, and step_visit, through its own foreign keys.
func truncateAll(t *testing.T) {
	t.Helper()
	if testPool == nil {
		t.Skip("no test database: set FLOWCORE_TEST_DSN and run `make migrate-test` (see Makefile)")
	}

	if _, err := testPool.Exec(context.Background(),
		"truncate flowcore.workflow_definition, flowcore.workflow cascade"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func ptr[T any](v T) *T { return &v }

func rowCount(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), "select count(*) from flowcore."+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}

	return n
}

// assertEmpty verifies no definition-side rows exist anywhere — used to prove an
// aggregate write left nothing partial behind.
func assertEmpty(t *testing.T) {
	t.Helper()
	for _, tbl := range []string{"workflow_definition", "workflow_status_definition", "step_definition", "action_definition"} {
		if n := rowCount(t, tbl); n != 0 {
			t.Errorf("expected %s empty, found %d rows", tbl, n)
		}
	}
}

// definitionIDs holds the explicit ids of a built definition so tests can
// reference and delete specific rows.
type definitionIDs struct {
	workflow, status, managerStep, directorStep uuid.UUID
	// The terminal statuses, distinct from status so that a test can tell a
	// stamped terminal status from one never written at all.
	approvedStatus, rejectedStatus uuid.UUID
}

// twoStepDefinition builds a valid definition — three statuses, two steps
// (manager review -> director review) with routing and terminal actions, entry
// at manager review. Ids are explicit so tests can target them.
//
// The terminal statuses are deliberately distinct from the in-progress one. An
// earlier version gave every step and every terminal action the same single
// status, which made a run read "in progress" both before and after it finished —
// so no assertion could tell a correctly stamped terminal status from one never
// written at all. The whole suite passed with terminal-status stamping removed.
func twoStepDefinition(name string) (WorkflowDefinition, definitionIDs) {
	ids := definitionIDs{
		workflow:       uuid.Must(uuid.NewV7()),
		status:         uuid.Must(uuid.NewV7()),
		managerStep:    uuid.Must(uuid.NewV7()),
		directorStep:   uuid.Must(uuid.NewV7()),
		approvedStatus: uuid.Must(uuid.NewV7()),
		rejectedStatus: uuid.Must(uuid.NewV7()),
	}
	definition := WorkflowDefinition{
		ID:                      ids.workflow,
		Name:                    name,
		InitialStepDefinitionID: &ids.managerStep,
		Statuses: []WorkflowStatusDefinition{
			{ID: ids.status, Name: "in progress"},
			{ID: ids.approvedStatus, Name: "approved"},
			{ID: ids.rejectedStatus, Name: "rejected"},
		},
		Steps: []StepDefinition{
			{ID: ids.managerStep, WorkflowStatusDefinitionID: ids.status, Name: "manager review", Actions: []ActionDefinition{
				{Name: "approve", NextStepDefinitionID: &ids.directorStep},
				{Name: "reject", TerminalWorkflowStatusDefinitionID: &ids.rejectedStatus},
			}},
			{ID: ids.directorStep, WorkflowStatusDefinitionID: ids.status, Name: "director review", Actions: []ActionDefinition{
				{Name: "approve", TerminalWorkflowStatusDefinitionID: &ids.approvedStatus},
			}},
		},
	}

	return definition, ids
}

// loopingDefinition builds a definition whose graph contains a cycle: manager
// review approves onward to director review, and director review's "reject"
// routes *back* to manager review rather than terminating.
//
// twoStepDefinition cannot serve the revisit tests because its graph is acyclic,
// so no run can reach a step twice.
func loopingDefinition(name string) (WorkflowDefinition, definitionIDs) {
	ids := definitionIDs{
		workflow:     uuid.Must(uuid.NewV7()),
		status:       uuid.Must(uuid.NewV7()),
		managerStep:  uuid.Must(uuid.NewV7()),
		directorStep: uuid.Must(uuid.NewV7()),
	}
	approved := uuid.Must(uuid.NewV7())
	definition := WorkflowDefinition{
		ID:                      ids.workflow,
		Name:                    name,
		InitialStepDefinitionID: &ids.managerStep,
		Statuses: []WorkflowStatusDefinition{
			{ID: ids.status, Name: "in progress"},
			{ID: approved, Name: "approved"},
		},
		Steps: []StepDefinition{
			{ID: ids.managerStep, WorkflowStatusDefinitionID: ids.status, Name: "manager review",
				AssigneeID: ptr("group:manager"),
				Actions: []ActionDefinition{
					{Name: "approve", NextStepDefinitionID: &ids.directorStep},
				}},
			{ID: ids.directorStep, WorkflowStatusDefinitionID: ids.status, Name: "director review",
				AssigneeID: ptr("group:director"),
				Actions: []ActionDefinition{
					{Name: "approve", TerminalWorkflowStatusDefinitionID: &approved},
					// the cycle: back to where it came from
					{Name: "reject", NextStepDefinitionID: &ids.managerStep},
				}},
		},
	}

	return definition, ids
}

// actionNamed finds an available action by name on the current step, so tests
// name the decision a person would make rather than carrying ids around.
func actionNamed(t *testing.T, state WorkflowState, name string) uuid.UUID {
	t.Helper()
	if state.CurrentStep == nil {
		t.Fatalf("no current step; cannot choose %q", name)
	}

	for _, action := range state.CurrentStep.Actions {
		if action.Name == name {
			return action.ID
		}
	}

	t.Fatalf("no action %q on step %q (have %+v)", name, state.CurrentStep.Name, state.CurrentStep.Actions)

	return uuid.Nil
}

func mustCreate(t *testing.T, catalog *Catalog, definition WorkflowDefinition) WorkflowDefinition {
	t.Helper()
	created, err := catalog.Create(context.Background(), definition)
	if err != nil {
		t.Fatalf("Create(%q): %v", definition.Name, err)
	}

	return created
}
