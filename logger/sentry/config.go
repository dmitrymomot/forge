package sentry

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/dmitrymomot/forge/logger"
)

// Config carries both the primary-logger settings (embedded logger.Config) and the
// Sentry-specific settings, so the whole thing env-loads in one shot. The env struct
// tags are inert strings — this package imports no config loader.
type Config struct {
	DSN         string `env:"DSN"`
	Environment string `env:"ENVIRONMENT"`
	// MinLevel is Sentry's OWN minimum level — the lowest level forwarded to Sentry,
	// independent of the primary destination's level. "debug"|"info"|"warn"/"warning"|"error".
	MinLevel string `env:"MIN_LEVEL"`
	logger.Config

	EnableLogs bool `env:"ENABLE_LOGS"`
}

// DefaultConfig returns the optimal defaults, including the embedded logger defaults.
func DefaultConfig() Config {
	return Config{
		Config:      logger.DefaultConfig(),
		Environment: "production",
		MinLevel:    "warn",
	}
}

// Validate validates the embedded logger.Config and the MinLevel. It wraps with
// double-%w so a bad primary Level/Format matches both sentry.ErrInvalidConfig and
// logger.ErrInvalidConfig. An empty DSN is valid (Sentry is then disabled in New).
func (c Config) Validate() error {
	if err := c.Config.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
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
