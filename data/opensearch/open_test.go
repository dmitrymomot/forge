package opensearch_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgeos "github.com/dmitrymomot/forge/data/opensearch"
)

func TestOpen_RetryExhausted(t *testing.T) {
	// Port 1 is unreachable; with a tiny budget Open exhausts retries quickly and
	// returns ErrConnect (joined with the last driver/transport error).
	cfg := forgeos.DefaultConfig()
	cfg.Addresses = []string{"http://127.0.0.1:1"}
	cfg.RetryAttempts = 2
	cfg.RetryInterval = 2 * time.Millisecond
	cfg.RequestTimeout = 50 * time.Millisecond

	start := time.Now()
	_, err := forgeos.Open(t.Context(), forgeos.WithConfig(cfg))
	require.Error(t, err)
	assert.ErrorIs(t, err, forgeos.ErrConnect)
	// Two attempts + one backoff must not take anywhere near the per-call ceiling.
	assert.Less(t, time.Since(start), 10*time.Second)
}

func TestOpen_ContextCancelledMidBackoff(t *testing.T) {
	cfg := forgeos.DefaultConfig()
	cfg.Addresses = []string{"http://127.0.0.1:1"}
	cfg.RetryAttempts = 50
	cfg.RetryInterval = 200 * time.Millisecond
	cfg.RequestTimeout = 50 * time.Millisecond

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	_, err := forgeos.Open(ctx, forgeos.WithConfig(cfg))
	require.Error(t, err)
	// Cancellation surfaces as ErrConnect joined with ctx.Err(); the loop must not
	// run all 50 attempts.
	assert.ErrorIs(t, err, forgeos.ErrConnect)
}
