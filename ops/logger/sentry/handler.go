package sentry

import (
	"context"
	"log/slog"

	sentry "github.com/getsentry/sentry-go"
	sentryslog "github.com/getsentry/sentry-go/slog"
)

// issueLevel is the minimum slog level reported to Sentry as an exception (Issue).
const issueLevel = slog.LevelError

// levelsFrom returns the standard slog levels at or above min, low to high.
func levelsFrom(min slog.Level) []slog.Level {
	all := []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError}
	out := make([]slog.Level, 0, len(all))
	for _, l := range all {
		if l >= min {
			out = append(out, l)
		}
	}
	return out
}

// sentryOption builds the sentryslog handler options from cfg — the Sentry Logs side only.
// EventLevel is set empty (not nil) to DISABLE sentryslog's deprecated log->event (Issue)
// conversion; errors become Issues via captureHandler/sentry.CaptureException instead.
// Drop the EventLevel line once sentry-go removes the field (slated for v0.48.0); leaving
// it nil would make the SDK re-enable the deprecated path with its [Error,Fatal] default.
// Factored out so the level/AddSource mapping is unit-testable without the global hub.
func sentryOption(cfg Config) sentryslog.Option {
	return sentryslog.Option{
		EventLevel: []slog.Level{},                       // disable deprecated log->Issue conversion
		LogLevel:   levelsFrom(parseLevel(cfg.MinLevel)), // MinLevel..error → Sentry Logs
		AddSource:  cfg.AddSource,                        // mirror the primary's AddSource
	}
}

// realSentryHandler initializes the Sentry SDK and builds the handler: a Sentry Logs
// handler (sentryslog) wrapped by captureHandler, which sends Error+ records to Sentry as
// Issues via CaptureException. Confirmed against sentry-go v0.47.0. The thin glue here
// mutates the global hub via sentry.Init; the testable logic lives in sentryOption,
// captureHandler, and errorFromRecord.
func realSentryHandler(cfg Config) (slog.Handler, error) {
	// v0.47.0: ClientOptions has no EnableLogs; logs are on by default, gated by
	// DisableLogs — so our opt-in EnableLogs inverts. (DisableLogs gates Sentry Logs only;
	// Issues via CaptureException are unaffected, so errors are reported regardless.)
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:         cfg.DSN,
		Environment: cfg.Environment,
		DisableLogs: !cfg.EnableLogs,
	}); err != nil {
		return nil, err
	}
	logs := sentryOption(cfg).NewSentryHandler(context.Background())
	return &captureHandler{next: logs, capture: captureException, level: issueLevel}, nil
}
