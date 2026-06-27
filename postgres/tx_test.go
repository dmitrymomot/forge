package postgres_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/postgres"
)

func TestRetryOption_Defaults(t *testing.T) {
	// The defaults are observable through behavior in the env-gated test; here we
	// assert the options are constructible and chainable without panic.
	assert.NotPanics(t, func() {
		_ = postgres.WithRetryAttempts(5)
		_ = postgres.WithRetryInterval(10 * time.Millisecond)
	})
}

func TestWithTx_Integration(t *testing.T) {
	dsn := os.Getenv("FORGE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set FORGE_TEST_POSTGRES_DSN")
	}
	pool := openTestPool(t, dsn)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `CREATE TEMP TABLE tx_test (id int PRIMARY KEY)`)
	require.NoError(t, err)

	// Commit path.
	require.NoError(t, postgres.WithTx(ctx, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO tx_test (id) VALUES (1)`)
		return err
	}))

	// Rollback path: fn returns an error => the row must not be visible.
	wantErr := errors.New("boom")
	err = postgres.WithTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO tx_test (id) VALUES (2)`); err != nil {
			return err
		}
		return wantErr
	})
	require.ErrorIs(t, err, wantErr)

	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM tx_test`).Scan(&count))
	assert.Equal(t, 1, count, "the rolled-back insert must not persist")
}

func TestWithTx_PanicRollsBackAndRepanics(t *testing.T) {
	dsn := os.Getenv("FORGE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set FORGE_TEST_POSTGRES_DSN")
	}
	pool := openTestPool(t, dsn)
	ctx := context.Background()

	assert.PanicsWithValue(t, "kaboom", func() {
		_ = postgres.WithTx(ctx, pool, func(_ pgx.Tx) error {
			panic("kaboom")
		})
	})
}

func TestWithTxRetry_RetriesOnSerializationFailure(t *testing.T) {
	dsn := os.Getenv("FORGE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set FORGE_TEST_POSTGRES_DSN")
	}
	pool := openTestPool(t, dsn)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS txr_test (id int PRIMARY KEY, n int)`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO txr_test (id, n) VALUES (1, 0) ON CONFLICT (id) DO UPDATE SET n = 0`)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DROP TABLE txr_test`) })

	// Two SERIALIZABLE transactions that read-then-write the same row force a 40001
	// on one of them; WithTxRetry must transparently retry it to success.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	start := make(chan struct{})
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = postgres.WithTxRetry(ctx, pool, func(tx pgx.Tx) error {
				var n int
				if err := tx.QueryRow(ctx, `SELECT n FROM txr_test WHERE id = 1`).Scan(&n); err != nil {
					return err
				}
				_, err := tx.Exec(ctx, `UPDATE txr_test SET n = $1 WHERE id = 1`, n+1)
				return err
			}, postgres.WithRetryAttempts(10), postgres.WithRetryInterval(time.Millisecond))
		}(i)
	}
	close(start)
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])

	var n int
	require.NoError(t, pool.QueryRow(ctx, `SELECT n FROM txr_test WHERE id = 1`).Scan(&n))
	assert.Equal(t, 2, n, "both serialized increments must land after retry")
}

func openTestPool(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	cfg := postgres.DefaultConfig()
	cfg.URL = dsn
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(func() { postgres.Close(pool, nil) })
	return pool
}
