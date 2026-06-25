package sentry

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlushNoDeadlineDoesNotTreatAsExpired(t *testing.T) {
	// A no-deadline ctx must use the 2s default (positive timeout), NOT fall into the
	// expired branch. The SDK's no-client Flush return is irrelevant — we only assert the
	// result is not a ctx-expiry error, which is deterministic.
	err := flush(context.Background())
	assert.NotErrorIs(t, err, context.DeadlineExceeded)
	assert.NotErrorIs(t, err, context.Canceled)
}

func TestFlushPastDeadlineReturnsCtxErr(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	err := flush(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestFlushCanceledContextReturnsCtxErr(t *testing.T) {
	// A canceled context WITHOUT a deadline must be honored immediately, not wait out the
	// 2s default. flush returns the context's cancellation error.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := flush(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}
