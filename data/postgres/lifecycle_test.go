package postgres_test

import (
	"context"
	"testing"

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
