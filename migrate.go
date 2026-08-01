package flowcore

import (
	"context"
	"embed"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// The migration files are embedded so a client can apply them from application
// code. FlowCore ships a schema, and a client should not have to install a
// command-line tool to get it — the same files serve both paths, and the Makefile
// drives the CLI for local development.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// migrationTableName keeps FlowCore's migration history in its own table, and is
// schema-qualified for a reason that is easy to get wrong.
//
// The name itself is not cosmetic: if FlowCore used goose's default table name
// and the client also uses goose, the two histories would share one table and
// corrupt each other's state.
//
// The `public.` prefix is load-bearing too. Postgres's default search_path is
// `"$user", public`, and `"$user"` resolves to the connecting role — so on a
// database whose role is named `flowcore`, current_schema() is `public` before
// migration 00001 runs and `flowcore` afterwards, because that migration creates
// the schema. An unqualified version table therefore gets written to `public` on
// the first run and looked for in `flowcore` on the second, where goose creates a
// second, empty one and then cannot reconcile the two.
//
// Worse, a version table inside `flowcore` would be destroyed by 00001's own
// down-migration, which drops the schema.
//
// Qualifying it pins the table regardless of role name or search_path, which is
// the same rule every other query in this library already follows. It cannot live
// inside `flowcore`: goose creates the version table before running any
// migration, and the schema does not exist yet.
//
// The Makefile passes the same value to the CLI; the two must stay in step.
const migrationTableName = "public.flowcore_goose_db_version"

// migrationLockID is the Postgres advisory lock Migrate holds while it runs, so
// that several application instances starting at once cannot migrate at the same
// time. One wins and migrates, the rest wait and then find nothing to do.
//
// It is FlowCore's own number rather than goose's default, for the same reason
// the version table is: a client who also migrates with goose should not contend
// with this library, or be blocked by it.
const migrationLockID int64 = 7208051224601

// Migrate applies FlowCore's schema migrations to the database behind pool,
// bringing it to the latest version. It is idempotent: migrations already applied
// are skipped, so calling it at start-up is safe.
//
// The client needs no command-line tool. The alternative is the goose CLI against
// the migrations directory, which must be given the same table name — see the
// Makefile.
//
// The pool is not closed and remains usable afterwards; the *sql.DB opened over
// it is closed here, which pgx documents as leaving the pool untouched.
//
// It requires CREATE privilege on the database, since the first migration creates
// the flowcore schema.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	// goose walks the root of the fs it is given, so the embedded tree is
	// re-rooted at the migrations directory rather than the module root.
	files, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return err
	}

	// goose is written against database/sql, so the pool is adapted rather than
	// used natively. This is scoped to migration only — the repository layer uses
	// pgxpool and the native pgx API.
	db := stdlib.OpenDBFromPool(pool)
	defer func() { _ = db.Close() }()

	// The provider carries the table name as instance state. goose's package-level
	// setters would be wrong here: they are global, so a library setting them
	// would silently reconfigure a client that also uses goose in the same process.
	// Advisory-locked so that concurrent start-ups serialize. Without it, two
	// instances can both read the same current version and both try to apply the
	// same migration, and the loser fails on DDL that already exists.
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockID(migrationLockID))
	if err != nil {
		return err
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, db, files,
		goose.WithTableName(migrationTableName),
		goose.WithSessionLocker(locker))
	if err != nil {
		return err
	}

	if _, err := provider.Up(ctx); err != nil {
		return err
	}

	return nil
}
