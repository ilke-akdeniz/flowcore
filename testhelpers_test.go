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
// all definition tables (setup, not teardown — a failed test leaves its rows for
// inspection), and returns a Catalog on the shared pool.
func newCatalog(t *testing.T) *Catalog {
	t.Helper()
	if testPool == nil {
		t.Skip("no test database: set FLOWCORE_TEST_DSN and run `make migrate-test` (see Makefile)")
	}

	if _, err := testPool.Exec(context.Background(), "truncate flowcore.workflow_definition cascade"); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	return NewCatalog(testPool)
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
}

// twoStepDefinition builds a valid definition — one status, two steps
// (manager review -> director review) with routing and terminal actions, entry
// at manager review. Ids are explicit so tests can target them.
func twoStepDefinition(name string) (WorkflowDefinition, definitionIDs) {
	ids := definitionIDs{
		workflow:     uuid.Must(uuid.NewV7()),
		status:       uuid.Must(uuid.NewV7()),
		managerStep:  uuid.Must(uuid.NewV7()),
		directorStep: uuid.Must(uuid.NewV7()),
	}
	definition := WorkflowDefinition{
		ID:                      ids.workflow,
		Name:                    name,
		InitialStepDefinitionID: &ids.managerStep,
		Statuses:                []WorkflowStatusDefinition{{ID: ids.status, Name: "in progress"}},
		Steps: []StepDefinition{
			{ID: ids.managerStep, WorkflowStatusDefinitionID: ids.status, Name: "manager review", Actions: []ActionDefinition{
				{Name: "approve", NextStepDefinitionID: &ids.directorStep},
				{Name: "reject", TerminalWorkflowStatusDefinitionID: &ids.status},
			}},
			{ID: ids.directorStep, WorkflowStatusDefinitionID: ids.status, Name: "director review", Actions: []ActionDefinition{
				{Name: "approve", TerminalWorkflowStatusDefinitionID: &ids.status},
			}},
		},
	}

	return definition, ids
}

func mustCreate(t *testing.T, catalog *Catalog, definition WorkflowDefinition) WorkflowDefinition {
	t.Helper()
	created, err := catalog.Create(context.Background(), definition)
	if err != nil {
		t.Fatalf("Create(%q): %v", definition.Name, err)
	}

	return created
}
