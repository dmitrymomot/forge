package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/postgres"
)

func TestClose_NilLoggerTolerated(t *testing.T) {
	// Close must not panic on a nil pool or a nil logger (defensive in main's defer).
	assert.NotPanics(t, func() { postgres.Close(nil, nil) })
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
