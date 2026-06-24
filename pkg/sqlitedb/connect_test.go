package sqlitedb

import (
	"context"
	"database/sql"
	"embed"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/migrations/*.sql
var testMigrations embed.FS

func TestOpen_Success(t *testing.T) {
	t.Parallel()

	db, err := Open(context.Background(), Config{Path: ":memory:"})
	require.NoError(t, err)
	defer db.Close()

	// Verify connection works
	err = db.PingContext(context.Background())
	require.NoError(t, err)
}

func TestOpen_EmptyPath(t *testing.T) {
	t.Parallel()

	_, err := Open(context.Background(), Config{Path: ""})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestOpen_PragmasApplied(t *testing.T) {
	t.Parallel()

	db, err := Open(context.Background(), Config{
		Path:          ":memory:",
		BusyTimeoutMS: 3000,
		Synchronous:   "full",
		CacheSize:     -10000,
		ForeignKeys:   true,
	})
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()

	// WAL mode is silently ignored for :memory:, but verify other PRAGMAs
	var synchronous string
	err = db.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&synchronous)
	require.NoError(t, err)
	// synchronous=full returns "2"
	assert.Equal(t, "2", synchronous)

	var busyTimeout int
	err = db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout)
	require.NoError(t, err)
	assert.Equal(t, 3000, busyTimeout)

	var cacheSize int
	err = db.QueryRowContext(ctx, "PRAGMA cache_size").Scan(&cacheSize)
	require.NoError(t, err)
	assert.Equal(t, -10000, cacheSize)

	var foreignKeys int
	err = db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys)
	require.NoError(t, err)
	assert.Equal(t, 1, foreignKeys)
}

func TestOpen_ForeignKeysDisabled(t *testing.T) {
	t.Parallel()

	db, err := Open(context.Background(), Config{
		Path:        ":memory:",
		ForeignKeys: false,
	})
	require.NoError(t, err)
	defer db.Close()

	var foreignKeys int
	err = db.QueryRowContext(context.Background(), "PRAGMA foreign_keys").Scan(&foreignKeys)
	require.NoError(t, err)
	assert.Equal(t, 0, foreignKeys)
}

func TestOpen_Defaults(t *testing.T) {
	t.Parallel()

	db, err := Open(context.Background(), Config{Path: ":memory:"})
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()

	// Verify busy_timeout default (5000ms)
	var busyTimeout int
	err = db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout)
	require.NoError(t, err)
	assert.Equal(t, 5000, busyTimeout)

	// Verify foreign_keys default (on, since ForeignKeys zero-value is false
	// but applyDefaults doesn't change it — this tests the zero-value behavior)
	var foreignKeys int
	err = db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys)
	require.NoError(t, err)
	// ForeignKeys defaults to false (zero value), so foreign_keys=0
	assert.Equal(t, 0, foreignKeys)
}

func TestOpen_PragmasAppliedToAllConnections(t *testing.T) {
	t.Parallel()

	// File-based (not :memory:) so a single *sql.DB can back multiple physical
	// connections. This is the regression guard for per-connection PRAGMAs being
	// applied to only one pooled connection.
	dbPath := filepath.Join(t.TempDir(), "multiconn.db")
	ctx := context.Background()

	db, err := Open(ctx, Config{
		Path:          dbPath,
		MaxOpenConns:  2,
		MaxIdleConns:  2,
		BusyTimeoutMS: 7000,
		ForeignKeys:   true,
	})
	require.NoError(t, err)
	defer db.Close()

	// Pin two distinct physical connections at the same time so the pool is
	// forced to open a second one; holding both prevents reuse of the first.
	conn1, err := db.Conn(ctx)
	require.NoError(t, err)
	defer conn1.Close()

	conn2, err := db.Conn(ctx)
	require.NoError(t, err)
	defer conn2.Close()

	for i, c := range []*sql.Conn{conn1, conn2} {
		var foreignKeys int
		require.NoError(t, c.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys))
		require.Equalf(t, 1, foreignKeys, "foreign_keys must be enabled on connection %d", i+1)

		var busyTimeout int
		require.NoError(t, c.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout))
		require.Equalf(t, 7000, busyTimeout, "busy_timeout must be set on connection %d", i+1)
	}
}

func TestOpen_WithMigrations(t *testing.T) {
	t.Parallel()

	db, err := Open(context.Background(), Config{Path: ":memory:"},
		WithMigrations(testMigrations),
	)
	require.NoError(t, err)
	defer db.Close()

	// Verify the test_items table was created by the migration
	_, err = db.ExecContext(context.Background(),
		"INSERT INTO test_items (name) VALUES (?)", "hello")
	require.NoError(t, err)

	var name string
	err = db.QueryRowContext(context.Background(),
		"SELECT name FROM test_items WHERE name = ?", "hello").Scan(&name)
	require.NoError(t, err)
	assert.Equal(t, "hello", name)
}
