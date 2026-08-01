package flowcore

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// querier is the slice of pgx shared by *pgxpool.Pool and pgx.Tx. Store helpers
// take a querier so the same helper runs autocommit against the pool or inside a
// transaction; only the service methods own Begin/Commit, helpers never do.
//
// A querier parameter says a helper *composes* into either. It says nothing about
// atomicity, and cannot: a single statement is atomic by itself, so such a helper
// is correct on the pool and inside a transaction alike.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// txQuerier is a querier that must be a transaction.
//
// A helper issuing several statements is not like one issuing a single statement.
// Run on the pool, each of its statements commits independently, so a failure
// part-way leaves state no caller intended: a workflow with steps but no opening
// visit, or a closed step visit with no successor and therefore a run that can
// never advance again. Reads tear the same way — status from before a transition
// beside the step from after it.
//
// Nothing in a querier signature distinguishes those two cases, so the multi-
// statement helpers take txQuerier instead and the compiler enforces it. Conn is
// the discriminator rather than anything meaningful: pgx.Tx has it, *pgxpool.Pool
// does not, so passing a pool fails to build instead of silently corrupting a run.
//
// Begin is deliberately still absent, exactly as it is from querier. This does not
// let a helper start a transaction — it only lets one require that a caller
// already did.
type txQuerier interface {
	querier
	Conn() *pgx.Conn
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
