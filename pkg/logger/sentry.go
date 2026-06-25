package logger

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	sentryslog "github.com/getsentry/sentry-go/slog"
)

// SentryConfig holds Sentry integration configuration.
type SentryConfig struct {
	DSN         string `env:"DSN"`
	Environment string `env:"ENVIRONMENT" envDefault:"production"`
	// MinLevel sets the minimum level for stdout logging and the lowest level forwarded to
	// Sentry as log entries. Accepts "debug", "info", "warn"/"warning", or "error"
	// (case-insensitive); unknown values default to "warn".
	MinLevel string `env:"MIN_LEVEL" envDefault:"warn"`
	// EnableLogs controls whether non-event log entries are forwarded to Sentry as log
	// entries (in addition to errors, which always create Issues). Defaults to false so
	// log forwarding is opt-in.
	EnableLogs bool `env:"ENABLE_LOGS"`
}

// SentryCloser flushes any buffered Sentry events and should be called before the program
// exits to avoid dropping events. It is safe to call when Sentry was not initialized (empty
// DSN or init failure), in which case it is a no-op and returns nil.
type SentryCloser func(timeout time.Duration) error

// parseMinLevel converts a string level name to slog.Level.
// Supports: "debug", "info", "warn"/"warning", "error".
// Defaults to slog.LevelWarn for unknown values.
func parseMinLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelWarn
	}
}

// noopCloser is returned when Sentry is not initialized; it never has events to flush.
func noopCloser(time.Duration) error { return nil }

// NewWithSentry creates a logger that sends logs to both stdout and Sentry.
// If DSN is empty, only stdout logging is enabled (graceful fallback for local dev).
// Context extractors are applied to logs sent to both destinations.
//
// MinLevel controls the stdout handler's minimum level (and the lowest level forwarded to
// Sentry). The returned SentryCloser flushes buffered Sentry events and must be called
// before the program exits; it is a no-op when Sentry is not active.
func NewWithSentry(cfg SentryConfig, extractors ...ContextExtractor) (*slog.Logger, SentryCloser) {
	minLevel := parseMinLevel(cfg.MinLevel)

	stdoutHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: minLevel,
	})

	// If no DSN, fall back to stdout only.
	if cfg.DSN == "" {
		return slog.New(NewLogHandlerDecorator(stdoutHandler, extractors...)), noopCloser
	}

	// Initialize Sentry SDK.
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:         cfg.DSN,
		Environment: cfg.Environment,
		EnableLogs:  cfg.EnableLogs,
	}); err != nil {
		// Graceful degradation: log to stdout if Sentry init fails.
		slog.New(stdoutHandler).Error("failed to initialize Sentry", slog.String("error", err.Error()))
		return slog.New(NewLogHandlerDecorator(stdoutHandler, extractors...)), noopCloser
	}

	// Determine which levels to send to Sentry. Events (Issues) are always created for
	// errors; log entries are forwarded from MinLevel up to error.
	eventLevel := []slog.Level{slog.LevelError}
	logLevel := levelsFrom(minLevel)

	sentryHandler := sentryslog.Option{
		EventLevel: eventLevel, // Errors create Issues in Sentry
		LogLevel:   logLevel,   // Logs stored for context/search
	}.NewSentryHandler(context.Background())

	// Combine stdout + Sentry handlers.
	combinedHandler := slog.NewMultiHandler(stdoutHandler, sentryHandler)

	// Wrap with decorator so context extractors work for both destinations.
	logger := slog.New(NewLogHandlerDecorator(combinedHandler, extractors...))

	closer := func(timeout time.Duration) error {
		if !sentry.Flush(timeout) {
			return ErrSentryFlushTimeout
		}
		return nil
	}
	return logger, closer
}

// levelsFrom returns the slog levels at or above min, among the standard
// debug/info/warn/error levels. Used to forward log entries to Sentry from MinLevel up.
func levelsFrom(min slog.Level) []slog.Level {
	all := []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError}
	levels := make([]slog.Level, 0, len(all))
	for _, l := range all {
		if l >= min {
			levels = append(levels, l)
		}
	}
	return levels
}
