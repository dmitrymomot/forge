package migration_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"testing/fstest"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/data/migration"
)

// mig builds a one-file goose migration creating table. It is shared with the
// integration tier (group_integration_test.go).
func mig(table string) fstest.MapFS {
	return fstest.MapFS{
		"00001_init.sql": &fstest.MapFile{Data: []byte(
			"-- +goose Up\nCREATE TABLE IF NOT EXISTS " + table + " (id int);\n" +
				"-- +goose Down\nDROP TABLE IF EXISTS " + table + ";\n")},
	}
}

// TestGroup_RejectsCollidingTables verifies Up validates distinct version
// tables before touching the db, so it needs no live connection: sql.Open is
// lazy and this path returns before any query is issued.
func TestGroup_RejectsCollidingTables(t *testing.T) {
	db, err := sql.Open("pgx", "postgres://invalid/db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	t.Run("both empty tables default to the same table", func(t *testing.T) {
		g := migration.Group(
			migration.Source(mig("grp_a"), ""),
			migration.Source(mig("grp_b"), ""),
		)
		err := g.Up(ctx, db)
		require.Error(t, err)
		assert.True(t, errors.Is(err, migration.ErrDuplicateSource))
	})

	t.Run("explicit tables match", func(t *testing.T) {
		g := migration.Group(
			migration.Source(mig("grp_a"), "dup"),
			migration.Source(mig("grp_b"), "dup"),
		)
		err := g.Up(ctx, db)
		require.Error(t, err)
		assert.True(t, errors.Is(err, migration.ErrDuplicateSource))
	})
}
