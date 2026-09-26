// Package store is the console's own data: submissions, their details, documents,
// the staff roster, and which workflow is active for which kind of submission.
//
// None of it is FlowCore's. The library holds the workflow graph and the record
// of work performed; everything here is what the application is about, which the
// library deliberately does not hold.
package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// migrationTableName keeps the console's migration history out of FlowCore's.
//
// The library records its own in `public.flowcore_goose_db_version` for exactly
// this reason. Two independent schemas in one database means two independent
// histories; sharing a version table would have each rolling back the other.
const migrationTableName = "public.console_goose_db_version"

// Migrate applies the console's schema. The library's Migrate applies its own,
// and the two do not know about each other.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	// goose walks the root of the filesystem it is given, so the embedded tree is
	// narrowed to the migrations directory.
	files, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return err
	}

	database := stdlib.OpenDBFromPool(pool)
	defer func() { _ = database.Close() }()

	provider, err := goose.NewProvider(goose.DialectPostgres, database, files,
		goose.WithTableName(migrationTableName))
	if err != nil {
		return fmt.Errorf("console migrate: %w", err)
	}

	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("console migrate: %w", err)
	}

	return nil
}
