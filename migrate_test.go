package flowcore

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestMigrateAppliesTheSchemaToAFreshDatabase exercises the client-facing promise:
// a schema applied from application code, with no command-line tool.
//
// It uses a database of its own rather than the suite's, because the suite's is
// already migrated — running Migrate against it would return no results and prove
// nothing about whether the embedded files actually apply.
func TestMigrateAppliesTheSchemaToAFreshDatabase(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database: set FLOWCORE_TEST_DSN and run `make migrate-test` (see Makefile)")
	}

	ctx := context.Background()
	const dbName = "flowcore_migrate_test"

	// CREATE DATABASE cannot run inside a transaction, so this goes to the pool.
	if _, err := testPool.Exec(ctx, "drop database if exists "+dbName); err != nil {
		t.Fatalf("dropping any leftover database: %v", err)
	}

	if _, err := testPool.Exec(ctx, "create database "+dbName); err != nil {
		t.Fatalf("create database: %v", err)
	}

	dsn := os.Getenv("FLOWCORE_TEST_DSN")
	if dsn == "" {
		dsn = defaultTestDSN
	}

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parsing the test DSN: %v", err)
	}

	config.ConnConfig.Database = dbName

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("connecting to the fresh database: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		if _, err := testPool.Exec(context.Background(), "drop database if exists "+dbName); err != nil {
			t.Errorf("dropping the temporary database: %v", err)
		}
	})

	// The schema does not exist yet.
	if tables := countFlowcoreTables(t, pool); tables != 0 {
		t.Fatalf("fresh database already has %d flowcore tables", tables)
	}

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	if tables := countFlowcoreTables(t, pool); tables != 8 {
		t.Errorf("after Migrate: %d flowcore tables, want 8", tables)
	}

	// Exactly one version table, in public. Two would mean search_path resolved
	// differently across runs — the failure the qualified name exists to prevent.
	var schemas []string
	rows, err := pool.Query(ctx,
		`select table_schema from information_schema.tables
		 where table_name = 'flowcore_goose_db_version' order by table_schema`)
	if err != nil {
		t.Fatalf("checking the version table: %v", err)
	}

	for rows.Next() {
		var schema string
		if err := rows.Scan(&schema); err != nil {
			t.Fatalf("scanning: %v", err)
		}

		schemas = append(schemas, schema)
	}

	rows.Close()

	if len(schemas) != 1 || schemas[0] != "public" {
		t.Errorf("version table schemas = %v, want exactly [public]", schemas)
	}

	var usesGooseDefault bool
	if err := pool.QueryRow(ctx,
		`select exists (select 1 from information_schema.tables where table_name = 'goose_db_version')`).
		Scan(&usesGooseDefault); err != nil {
		t.Fatalf("checking for goose's default table: %v", err)
	}

	if usesGooseDefault {
		t.Error("goose's default version table was created; a client using goose would collide with it")
	}

	// Idempotent: a second call is a no-op rather than an error.
	if err := Migrate(ctx, pool); err != nil {
		t.Errorf("second Migrate should be a no-op: %v", err)
	}

	// The pool survives Migrate closing the *sql.DB it opened over it.
	if _, err := pool.Exec(ctx, "select 1"); err != nil {
		t.Errorf("pool unusable after Migrate: %v", err)
	}
}

func countFlowcoreTables(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()

	var count int
	if err := pool.QueryRow(context.Background(),
		`select count(*) from information_schema.tables where table_schema = 'flowcore'`).Scan(&count); err != nil {
		t.Fatalf("counting flowcore tables: %v", err)
	}

	return count
}

// Migrate is documented as safe to call from every instance at start-up, which
// is only true because it holds an advisory lock. Without one, two instances can
// read the same current version and both try to apply the same migration, and the
// loser fails on DDL that already exists.
func TestMigrateIsSafeWhenInstancesStartTogether(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}

	ctx := context.Background()
	const dbName = "flowcore_concurrent_migrate_test"

	if _, err := testPool.Exec(ctx, "drop database if exists "+dbName); err != nil {
		t.Fatalf("dropping any leftover database: %v", err)
	}

	if _, err := testPool.Exec(ctx, "create database "+dbName); err != nil {
		t.Fatalf("create database: %v", err)
	}

	dsn := os.Getenv("FLOWCORE_TEST_DSN")
	if dsn == "" {
		dsn = defaultTestDSN
	}

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parsing the test DSN: %v", err)
	}

	config.ConnConfig.Database = dbName

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("connecting to the fresh database: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		if _, err := testPool.Exec(context.Background(), "drop database if exists "+dbName); err != nil {
			t.Errorf("dropping the temporary database: %v", err)
		}
	})

	// Five instances booting at once, as a rolling deploy would.
	const instances = 5

	var waitGroup sync.WaitGroup
	release := make(chan struct{})
	errs := make([]error, instances)

	for i := range errs {
		waitGroup.Add(1)

		go func() {
			defer waitGroup.Done()
			<-release
			errs[i] = Migrate(ctx, pool)
		}()
	}

	close(release)
	waitGroup.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("instance %d failed to migrate: %v", i, err)
		}
	}

	if tables := countFlowcoreTables(t, pool); tables != 8 {
		t.Errorf("after concurrent migration: %d flowcore tables, want 8", tables)
	}
}
