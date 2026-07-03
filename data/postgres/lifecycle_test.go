package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/data/postgres"
)

func TestClose_NilLoggerTolerated(t *testing.T) {
	// Close must not panic on a nil pool or a nil logger (defensive in main's defer).
	assert.NotPanics(t, func() { postgres.Close(nil, nil) })

	// A non-nil pool with a nil logger must still close without panicking and
	// without emitting a log line. pgxpool.NewWithConfig connects lazily, so no
	// server is needed to construct (and immediately close) the pool.
	cfg, err := pgxpool.ParseConfig("postgres://u:p@127.0.0.1:1/db")
	require.NoError(t, err)
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	require.NoError(t, err)
	assert.NotPanics(t, func() { postgres.Close(pool, nil) })
}

func TestHealthcheck_Integration(t *testing.T) {
	dsn := os.Getenv("FORGE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set FORGE_TEST_POSTGRES_DSN")
	}
	cfg := postgres.DefaultConfig()
	cfg.URL = dsn
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(t, err)
	defer postgres.Close(pool, nil)

	check := postgres.Healthcheck(pool)
	require.NotNil(t, check)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, check(ctx), "a live pool must report healthy")

	// After Close, the same closure must report an ErrHealthcheck-wrapped failure.
	postgres.Close(pool, nil)
	assert.ErrorIs(t, check(ctx), postgres.ErrHealthcheck)
}
