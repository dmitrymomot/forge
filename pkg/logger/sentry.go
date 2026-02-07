package logger

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/getsentry/sentry-go"
	sentryslog "github.com/getsentry/sentry-go/slog"
)

// SentryConfig holds Sentry integration configuration.
type SentryConfig struct {
	DSN         string `env:"DSN"`
	Environment string `env:"ENVIRONMENT" envDefault:"production"`
	MinLevel    string `env:"MIN_LEVEL"   envDefault:"warn"`
}

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

// NewWithSentry creates a logger that sends logs to both stdout and Sentry.
// If DSN is empty, only stdout logging is enabled (graceful fallback for local dev).
// Context extractors are applied to logs sent to both destinations.
func NewWithSentry(cfg SentryConfig, extractors ...ContextExtractor) *slog.Logger {
	stdoutHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})

	// If no DSN, fall back to stdout only
	if cfg.DSN == "" {
		return slog.New(NewLogHandlerDecorator(stdoutHandler, extractors...))
	}

	// Initialize Sentry SDK
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:         cfg.DSN,
		Environment: cfg.Environment,
		EnableLogs:  true,
	}); err != nil {
		// Graceful degradation: log to stdout if Sentry init fails
		slog.New(stdoutHandler).Error("failed to initialize Sentry", slog.String("error", err.Error()))
		return slog.New(NewLogHandlerDecorator(stdoutHandler, extractors...))
	}

	minLevel := parseMinLevel(cfg.MinLevel)

	// Determine which levels to send to Sentry
	eventLevel := []slog.Level{slog.LevelError}
	logLevel := []slog.Level{slog.LevelWarn, slog.LevelError}
	if minLevel == slog.LevelError {
		logLevel = []slog.Level{slog.LevelError}
	}

	sentryHandler := sentryslog.Option{
		EventLevel: eventLevel, // Errors create Issues in Sentry
		LogLevel:   logLevel,   // Logs stored for context/search
	}.NewSentryHandler(context.Background())

	// Combine stdout + Sentry handlers
	combinedHandler := newMultiHandler(stdoutHandler, sentryHandler)

	// Wrap with decorator so context extractors work for both destinations
	return slog.New(NewLogHandlerDecorator(combinedHandler, extractors...))
}
