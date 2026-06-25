package sentry

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLevelsFrom(t *testing.T) {
	assert.Equal(t, []slog.Level{slog.LevelWarn, slog.LevelError}, levelsFrom(slog.LevelWarn))
	assert.Equal(t,
		[]slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError},
		levelsFrom(slog.LevelDebug))
}

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
