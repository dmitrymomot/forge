package logger

import (
	"fmt"
	"log/slog"
	"strings"
)

// Format selects the slog handler. FormatText is the default.
type Format string

const (
	FormatText Format = "text"
	FormatJSON Format = "json"
)

// Config holds the serializable settings for New. The env struct tags are inert
// strings — this package imports no config loader.
type Config struct {
	// Level is the minimum level for the primary destination:
	// "debug", "info", "warn"/"warning", "error" (case-insensitive).
	Level string `env:"LEVEL"`
	// Format selects the handler: "text" or "json" (case-insensitive).
	Format string `env:"FORMAT"`
	// File, when non-empty, makes the primary destination this file INSTEAD of stdout.
	// Parent directories and the file are created if absent. Empty means stdout.
	File string `env:"FILE"`
	// AddSource includes the source file:line in records (slog AddSource).
	AddSource bool `env:"ADD_SOURCE"`
}

// DefaultConfig returns the optimal defaults and is the single source of truth for them.
func DefaultConfig() Config {
	return Config{
		Level:  "info",
		Format: "text",
	}
}

// Validate reports whether the field values are usable, returning an
// ErrInvalidConfig-wrapped error otherwise. File is not checked here (created by New).
func (c Config) Validate() error {
	if _, ok := levelByName(c.Level); !ok {
		return fmt.Errorf("%w: unknown Level %q", ErrInvalidConfig, c.Level)
	}
	if _, ok := formatByName(c.Format); !ok {
		return fmt.Errorf("%w: unknown Format %q", ErrInvalidConfig, c.Format)
	}
	return nil
}

// levelByName maps a level name to a slog.Level, reporting whether it is known.
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

// parseLevel maps a validated level name to its slog.Level (assumes Validate passed).
func parseLevel(s string) slog.Level {
	lvl, _ := levelByName(s)
	return lvl
}

// formatByName maps a format name to a Format, reporting whether it is known.
func formatByName(s string) (Format, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "text":
		return FormatText, true
	case "json":
		return FormatJSON, true
	default:
		return "", false
	}
}

// parseFormat maps a validated format name to its Format (assumes Validate passed).
func parseFormat(s string) Format {
	f, _ := formatByName(s)
	return f
}
