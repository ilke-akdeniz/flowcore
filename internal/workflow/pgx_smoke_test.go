package workflow

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestPostgresConnection is a temporary sanity check that the local Postgres,
// pgx, and the Go toolchain all work together. Delete it once real repository
// tests exist.
func TestPostgresConnection(t *testing.T) {
	dsn := os.Getenv("FLOWCORE_TEST_DSN")
	if dsn == "" {
		dsn = "postgres://flowcore:flowcore@localhost:5432/flowcore"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connecting to postgres: %v", err)
	}
	defer conn.Close(ctx)

	var got int
	if err := conn.QueryRow(ctx, "select 1").Scan(&got); err != nil {
		t.Fatalf("querying: %v", err)
	}

	if got != 1 {
		t.Fatalf("select 1 returned %d, want 1", got)
	}
}
