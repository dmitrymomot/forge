package sqlitedb

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrate_Success(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := Open(ctx, Config{Path: ":memory:"})
	require.NoError(t, err)
	defer db.Close()

	err = Migrate(ctx, db, testMigrations, slog.Default())
	require.NoError(t, err)

	// Verify table was created
	_, err = db.ExecContext(ctx, "INSERT INTO test_items (name) VALUES (?)", "migrated")
	require.NoError(t, err)

	var name string
	err = db.QueryRowContext(ctx, "SELECT name FROM test_items WHERE name = ?", "migrated").Scan(&name)
	require.NoError(t, err)
	assert.Equal(t, "migrated", name)
}

func TestMigrate_Idempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := Open(ctx, Config{Path: ":memory:"})
	require.NoError(t, err)
	defer db.Close()

	err = Migrate(ctx, db, testMigrations, slog.Default())
	require.NoError(t, err)

	// Run again — should not error
	err = Migrate(ctx, db, testMigrations, slog.Default())
	require.NoError(t, err)
}

func TestMigrate_NilLogger(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := Open(ctx, Config{Path: ":memory:"})
	require.NoError(t, err)
	defer db.Close()

	// nil logger should not panic
	err = Migrate(ctx, db, testMigrations, nil)
	require.NoError(t, err)
}
