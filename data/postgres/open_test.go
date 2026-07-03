package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/data/postgres"
)

func TestOpen_RetryExhausted(t *testing.T) {
	// Port 1 is unreachable; with 2 attempts and tiny waits Open must give up fast
	// and return ErrConnect joined with the last driver error.
	cfg := postgres.DefaultConfig()
	cfg.URL = "postgres://u:p@127.0.0.1:1/db"
	cfg.RetryAttempts = 2
	cfg.RetryInterval = time.Millisecond
	cfg.ConnectTimeout = 100 * time.Millisecond

	start := time.Now()
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.ErrorIs(t, err, postgres.ErrConnect)
	assert.Nil(t, pool)
	assert.Less(t, elapsed, 5*time.Second, "two short attempts must not block long")
}

func TestOpen_ContextCancelled(t *testing.T) {
	cfg := postgres.DefaultConfig()
	cfg.URL = "postgres://u:p@127.0.0.1:1/db"
	cfg.RetryAttempts = 100
	cfg.RetryInterval = time.Second
	cfg.ConnectTimeout = 100 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	start := time.Now()
	pool, err := postgres.Open(ctx, postgres.WithConfig(cfg))
	require.Error(t, err)
	assert.ErrorIs(t, err, postgres.ErrConnect)
	assert.Nil(t, pool)
	assert.Less(t, time.Since(start), 2*time.Second, "a cancelled ctx must short-circuit the retry loop")
}
