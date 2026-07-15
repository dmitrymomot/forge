//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/testkit/pgtest"
)

func TestHealthcheck_Integration(t *testing.T) {
	cfg := postgres.DefaultConfig()
	cfg.URL = pgtest.DSN(t)
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
