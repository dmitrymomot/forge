package db

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// txBeginner is the minimal surface WithTx needs from a connection pool.
// It exists as an internal seam so the transaction lifecycle (commit,
// rollback, re-panic) can be unit-tested without a live database.
// *pgxpool.Pool satisfies it.
type txBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// WithTx executes fn within a database transaction.
// If fn returns an error, the transaction is rolled back.
// If fn panics, the transaction is rolled back and the panic is re-raised.
// If fn succeeds, the transaction is committed.
func WithTx(ctx context.Context, pool *pgxpool.Pool, fn func(tx pgx.Tx) error) error {
	return withTx(ctx, pool, fn)
}

// withTx contains the transaction lifecycle logic against the minimal
// txBeginner interface so it can be exercised with a fake beginner in tests.
func withTx(ctx context.Context, pool txBeginner, fn func(tx pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}

	return tx.Commit(ctx)
}
