//go:build integration

package migration_test

import (
	"context"
	"database/sql"
	"testing"
	"testing/fstest"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/testkit/pgtest"
)

func TestUp_EmptyFS_IsNoop(t *testing.T) {
	db := openDB(t, pgtest.DSN(t))

	// An fsys with no migration files must succeed as a no-op, not error out, so an
	// app that embeds an empty migrations dir still boots.
	m := migration.New(fstest.MapFS{})
	require.NoError(t, m.Up(context.Background(), db))
}

func TestUp_AppliesMigration_Integration(t *testing.T) {
	db := openDB(t, pgtest.DSN(t))
	t.Cleanup(func() {
		_, _ = db.Exec(`DROP TABLE IF EXISTS widgets`)
		_, _ = db.Exec(`DROP TABLE IF EXISTS schema_migrations`)
	})

	m := migration.New(oneMigration)
	ctx := context.Background()

	require.NoError(t, m.Up(ctx, db))

	// The migration's table now exists.
	var exists bool
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'widgets')`,
	).Scan(&exists))
	assert.True(t, exists, "the migration must have created the widgets table")

	// The default goose version table is named schema_migrations.
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'schema_migrations')`,
	).Scan(&exists))
	assert.True(t, exists, "the default version table must be schema_migrations")

	// A second Up is an idempotent no-op (no pending migrations).
	require.NoError(t, m.Up(ctx, db), "re-running Up with nothing pending must succeed")
}

func openDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	require.NoError(t, db.Ping())
	t.Cleanup(func() { _ = db.Close() })
	return db
}
