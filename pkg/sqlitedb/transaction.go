package sqlitedb

import (
	"context"
	"database/sql"
)

// WithTx executes fn within a database transaction.
// If fn returns an error, the transaction is rolled back.
// If fn panics, the transaction is rolled back and the panic is re-raised.
// If fn succeeds, the transaction is committed.
func WithTx(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}
