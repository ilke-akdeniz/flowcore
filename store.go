package flowcore

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// querier is the slice of pgx shared by *pgxpool.Pool and pgx.Tx. Store helpers
// take a querier so the same helper runs autocommit against the pool or inside a
// transaction; only the aggregate create and the deep read own Begin/Commit,
// helpers never do.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// The store is the boundary that translates database errors into the domain
// taxonomy: each helper maps its own error by intent — mapInsertErr on inserts,
// mapWriteErr on updates, mapDeleteErr on deletes — and reports a missing row as
// NotFoundError. Above the store, Catalog composes these helpers.
//
// Catalog opens a transaction only where one is needed: Create, to write a whole
// definition tree atomically, and Get, for one consistent snapshot across its
// four reads. Every other method is a single statement, which is already its own
// transaction. That includes DeleteWorkflowDefinition — its cascade needs no
// wrapper, because the deferred reference FKs let the whole cascade resolve
// before their checks run at the statement's implicit commit.
