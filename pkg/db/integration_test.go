//go:build integration

package db

import (
	"context"
	"embed"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/id"
)

// defaultTestDSN matches the postgres service in the repo's docker-compose.yml,
// which `just test-integration` brings up. Override with TEST_DATABASE_URL.
const defaultTestDSN = "postgres://forge:forge@localhost:5432/forge_test?sslmode=disable"

//go:embed 00001_db_test_widgets.sql
var testMigrations embed.FS

func testDSN() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	return defaultTestDSN
}

func TestIntegration_OpenMigrateWithTx(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := Open(ctx, Config{URL: testDSN()}, WithMigrations(testMigrations))
	require.NoError(t, err, "Open with migrations must succeed against the integration DB")
	t.Cleanup(pool.Close)

	// Migrate ran via Open: the migration table and target table must exist.
	var widgetsExists bool
	err = pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'db_test_widgets')`,
	).Scan(&widgetsExists)
	require.NoError(t, err)
	require.True(t, widgetsExists, "Migrate must have created db_test_widgets")

	var migTableExists bool
	err = pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'schema_migrations')`,
	).Scan(&migTableExists)
	require.NoError(t, err)
	require.True(t, migTableExists, "Migrate must track state in the hardcoded schema_migrations table")

	// Start from a clean slate.
	_, err = pool.Exec(ctx, `TRUNCATE db_test_widgets`)
	require.NoError(t, err)

	t.Run("commit persists rows", func(t *testing.T) {
		committedID := id.NewULID()
		err := WithTx(ctx, pool, func(tx pgx.Tx) error {
			_, e := tx.Exec(ctx, `INSERT INTO db_test_widgets (id, name) VALUES ($1, $2)`, committedID, "committed")
			return e
		})
		require.NoError(t, err, "successful WithTx must commit")

		var name string
		err = pool.QueryRow(ctx, `SELECT name FROM db_test_widgets WHERE id = $1`, committedID).Scan(&name)
		require.NoError(t, err, "committed row must be visible outside the transaction")
		require.Equal(t, "committed", name)
	})

	t.Run("error rolls back rows", func(t *testing.T) {
		rolledBackID := id.NewULID()
		sentinel := errors.New("forced rollback")

		err := WithTx(ctx, pool, func(tx pgx.Tx) error {
			if _, e := tx.Exec(ctx, `INSERT INTO db_test_widgets (id, name) VALUES ($1, $2)`, rolledBackID, "rolled-back"); e != nil {
				return e
			}
			return sentinel
		})
		require.ErrorIs(t, err, sentinel, "WithTx must surface the fn error")

		var count int
		err = pool.QueryRow(ctx, `SELECT count(*) FROM db_test_widgets WHERE id = $1`, rolledBackID).Scan(&count)
		require.NoError(t, err)
		require.Zero(t, count, "row inserted in a failed transaction must be rolled back")
	})

	t.Run("panic rolls back rows and re-panics", func(t *testing.T) {
		panicID := id.NewULID()

		require.Panics(t, func() {
			_ = WithTx(ctx, pool, func(tx pgx.Tx) error {
				_, _ = tx.Exec(ctx, `INSERT INTO db_test_widgets (id, name) VALUES ($1, $2)`, panicID, "panic")
				panic("boom")
			})
		}, "panic inside WithTx must propagate")

		var count int
		err = pool.QueryRow(ctx, `SELECT count(*) FROM db_test_widgets WHERE id = $1`, panicID).Scan(&count)
		require.NoError(t, err)
		require.Zero(t, count, "row inserted before a panic must be rolled back")
	})
}

func TestIntegration_Healthcheck(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := Open(ctx, Config{URL: testDSN()})
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	require.NoError(t, Healthcheck(pool)(ctx), "healthcheck must pass against a live DB")
}
