package sentry

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func allLevels() []slog.Level {
	return []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError}
}

func TestNewHandlerEmptyDSNReturnsDisabledHandler(t *testing.T) {
	h, fl, err := NewHandler() // DefaultConfig has empty DSN
	require.NoError(t, err)
	require.NotNil(t, h)
	for _, l := range allLevels() {
		assert.False(t, h.Enabled(context.Background(), l), "disabled handler must report Enabled=false at %v", l)
	}
	// The disabled handler is inert but safe through the full slog.Handler surface.
	derived := h.WithAttrs([]slog.Attr{slog.String("k", "v")}).WithGroup("g")
	require.NoError(t, derived.Handle(context.Background(), slog.NewRecord(time.Time{}, slog.LevelError, "x", 0)))
	require.NoError(t, fl(context.Background()))
}

func TestNewHandlerInitFailureReturnsDisabledHandlerPlusError(t *testing.T) {
	build := func(Config) (slog.Handler, error) { return nil, errors.New("init boom") }
	cfg := DefaultConfig()
	cfg.DSN = "https://publicKey@o0.ingest.sentry.io/0" // non-empty → build is attempted

	h, fl, err := newHandlerWith(build, WithConfig(cfg))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSentryInit)
	require.NotNil(t, h)
	assert.False(t, h.Enabled(context.Background(), slog.LevelError))
	require.NoError(t, fl(context.Background()))
}

func TestNewHandlerInvalidConfigReturnsDisabledHandlerPlusError(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MinLevel = "loud"
	h, fl, err := NewHandler(WithConfig(cfg))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)
	require.NotNil(t, h)
	assert.False(t, h.Enabled(context.Background(), slog.LevelError))
	require.NoError(t, fl(context.Background()))
}

func TestNewHandlerSuccessReturnsBuiltHandlerAndRealFlush(t *testing.T) {
	fake := slog.NewJSONHandler(io.Discard, nil)
	build := func(Config) (slog.Handler, error) { return fake, nil }
	cfg := DefaultConfig()
	cfg.DSN = "https://publicKey@o0.ingest.sentry.io/0"

	h, fl, err := newHandlerWith(build, WithConfig(cfg))
	require.NoError(t, err)
	assert.Equal(t, fake, h) // the built handler is returned as-is, unwrapped
	require.NotNil(t, fl)
}
