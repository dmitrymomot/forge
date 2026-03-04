package sqlitedb

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithTx_Commit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := Open(ctx, Config{Path: ":memory:"}, WithMigrations(testMigrations))
	require.NoError(t, err)
	defer db.Close()

	err = WithTx(ctx, db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO test_items (name) VALUES (?)", "committed")
		return err
	})
	require.NoError(t, err)

	// Verify data persisted after commit
	var name string
	err = db.QueryRowContext(ctx, "SELECT name FROM test_items WHERE name = ?", "committed").Scan(&name)
	require.NoError(t, err)
	assert.Equal(t, "committed", name)
}

func TestWithTx_Rollback(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := Open(ctx, Config{Path: ":memory:"}, WithMigrations(testMigrations))
	require.NoError(t, err)
	defer db.Close()

	errFail := errors.New("intentional failure")
	err = WithTx(ctx, db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO test_items (name) VALUES (?)", "rolled-back")
		if err != nil {
			return err
		}
		return errFail
	})
	require.ErrorIs(t, err, errFail)

	// Verify data was rolled back
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM test_items WHERE name = ?", "rolled-back").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestWithTx_PanicRollback(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := Open(ctx, Config{Path: ":memory:"}, WithMigrations(testMigrations))
	require.NoError(t, err)
	defer db.Close()

	// Verify panic is re-raised and data is rolled back
	assert.PanicsWithValue(t, "boom", func() {
		_ = WithTx(ctx, db, func(tx *sql.Tx) error {
			_, _ = tx.ExecContext(ctx, "INSERT INTO test_items (name) VALUES (?)", "panicked")
			panic("boom")
		})
	})

	// Verify data was rolled back
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM test_items WHERE name = ?", "panicked").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}
