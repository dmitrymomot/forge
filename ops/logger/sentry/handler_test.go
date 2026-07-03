package sentry

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLevelsFrom(t *testing.T) {
	assert.Equal(t, []slog.Level{slog.LevelWarn, slog.LevelError}, levelsFrom(slog.LevelWarn))
	assert.Equal(t,
		[]slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError},
		levelsFrom(slog.LevelDebug))
}

func TestSentryOptionMapping(t *testing.T) {
	// Covers the level/AddSource mapping that realSentryHandler used to inline. Unit-testing
	// it here avoids initializing the global Sentry hub (which realSentryHandler does).
	warn := DefaultConfig()
	warn.MinLevel = "warn"
	warn.AddSource = true
	opt := sentryOption(warn)
	assert.Equal(t, []slog.Level{slog.LevelWarn, slog.LevelError}, opt.LogLevel,
		"LogLevel must be MinLevel..error")
	assert.True(t, opt.AddSource, "AddSource must mirror the embedded logger.Config")

	debug := DefaultConfig()
	debug.MinLevel = "debug"
	debug.AddSource = false
	opt2 := sentryOption(debug)
	assert.Equal(t,
		[]slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError},
		opt2.LogLevel)
	assert.False(t, opt2.AddSource)
}
