//go:build integration

package migration_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/testkit/pgtest"
)

// openPoolDB opens a *sql.DB backed by a postgres.Open pool, matching how
// GroupMigrator is wired in production (via postgres.WithMigrator).
func openPoolDB(t *testing.T) *sql.DB {
	cfg := postgres.DefaultConfig()
	cfg.URL = pgtest.DSN(t)
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestGroup_AppliesEachUnderOwnTable(t *testing.T) {
	db := openPoolDB(t)
	ctx := context.Background()
	_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS grp_a, grp_b; DROP TABLE IF EXISTS gv_a, gv_b")

	g := migration.Group(
		migration.Source(mig("grp_a"), "gv_a"),
		migration.Source(mig("grp_b"), "gv_b"),
	)
	require.NoError(t, g.Up(ctx, db))

	for _, tbl := range []string{"grp_a", "grp_b", "gv_a", "gv_b"} {
		var n int
		require.NoError(t, db.QueryRowContext(ctx,
			"SELECT count(*) FROM information_schema.tables WHERE table_name=$1", tbl).Scan(&n))
		assert.Equal(t, 1, n, "table %s should exist", tbl)
	}
}
