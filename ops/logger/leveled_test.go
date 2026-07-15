package logger_test

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/logger"
)

func TestWithLeveledHandlerGatesBelowMin(t *testing.T) {
	rl, rec := logger.NewRecorder()
	log, err := logger.New(
		logger.WithOutput(io.Discard),
		logger.WithLevel(slog.LevelDebug),
		logger.WithLeveledHandler(slog.LevelWarn, rl.Handler()),
	)
	require.NoError(t, err)

	log.Info("below")
	log.Warn("at")
	log.Error("above")

	recs := rec.Records()
	require.Len(t, recs, 2)
	assert.Equal(t, "at", recs[0].Message)
	assert.Equal(t, "above", recs[1].Message)
}

func TestWithLeveledHandlerKeepsGateThroughWithAttrsAndGroup(t *testing.T) {
	rl, rec := logger.NewRecorder()
	log, err := logger.New(
		logger.WithOutput(io.Discard),
		logger.WithLevel(slog.LevelDebug),
		logger.WithLeveledHandler(slog.LevelWarn, rl.Handler()),
	)
	require.NoError(t, err)

	derived := log.With("env", "prod").WithGroup("http")
	derived.Info("below") // still gated after WithAttrs/WithGroup
	derived.Warn("kept", "status", 200)

	recs := rec.Records()
	require.Len(t, recs, 1)
	assert.Equal(t, "kept", recs[0].Message)
	assert.Equal(t, "prod", recs[0].Attrs["env"])
	assert.Equal(t, int64(200), recs[0].Attrs["http.status"])
}

func TestWithLeveledHandlerNilRejected(t *testing.T) {
	_, err := logger.New(logger.WithLeveledHandler(slog.LevelWarn, nil))
	require.Error(t, err)
	assert.ErrorIs(t, err, logger.ErrInvalidConfig)
}
