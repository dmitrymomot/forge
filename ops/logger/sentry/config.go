package sentry

import (
	"fmt"
	"log/slog"
	"strings"
)

// Config carries the Sentry-specific settings. The env struct tags are inert strings —
// this package imports no config loader. Logger settings live in logger.Config; the two
// env blocks (LOG_*, SENTRY_*) load independently.
type Config struct {
	DSN         string `env:"SENTRY_DSN"`
	Environment string `env:"SENTRY_ENVIRONMENT"`
	// MinLevel is the lowest level forwarded to Sentry Logs, independent of any logger
	// destination's level. "debug"|"info"|"warn"/"warning"|"error".
	MinLevel string `env:"SENTRY_MIN_LEVEL"`
	// EnableLogs opts in to Sentry Logs for the MinLevel..error range; Issues for Error
	// and above are reported regardless.
	EnableLogs bool `env:"SENTRY_ENABLE_LOGS"`
	// AddSource includes the source file:line in records sent to Sentry Logs.
	AddSource bool `env:"SENTRY_ADD_SOURCE"`
}

// DefaultConfig returns the optimal defaults and is the single source of truth for them.
func DefaultConfig() Config {
	return Config{
		Environment: "production",
		MinLevel:    "warn",
	}
}

// Validate reports whether the field values are usable, returning an
// ErrInvalidConfig-wrapped error otherwise. An empty DSN is valid (Sentry is then
// disabled in NewHandler).
func (c Config) Validate() error {
	if _, ok := levelByName(c.MinLevel); !ok {
		return fmt.Errorf("%w: unknown MinLevel %q", ErrInvalidConfig, c.MinLevel)
	}
	return nil
}

// levelByName maps a level name to a slog.Level, reporting whether it is known. Mirrors
// logger.levelByName — copied, not coupled, per CLAUDE.md (no cross-package level helper).
func levelByName(s string) (slog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	default:
		return 0, false
	}
}

// parseLevel maps a validated MinLevel name to its slog.Level (assumes Validate passed).
func parseLevel(s string) slog.Level {
	lvl, _ := levelByName(s)
	return lvl
}
