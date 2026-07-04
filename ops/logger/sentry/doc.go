// Package sentry builds a logger.New-style *slog.Logger that also reports to Sentry in
// parallel, at its own MinLevel, while keeping the Sentry SDK out of the core logger's
// import graph.
//
// # Usage
//
//	log, flush, err := sentry.New(
//		sentry.WithConfig(sentry.Config{
//			Config:   logger.Config{Level: "info", Format: "json"},
//			DSN:      os.Getenv("SENTRY_DSN"), // empty → plain logger
//			MinLevel: "warn",
//		}),
//		sentry.WithContextExtractors(reqIDExtractor),
//	)
//	defer flush(context.Background()) // flushes buffered events; no-op when Sentry is inactive
//
// Error-level (and above) records are reported to Sentry as Issues via
// sentry.CaptureException; the MinLevel..error range is sent as Sentry Logs when EnableLogs
// is set. (The SDK's deprecated log-to-event conversion is not used.)
//
// Config embeds logger.Config so primary-logger and Sentry settings env-load together.
// An empty DSN returns a plain logger and a no-op Flush; a Sentry init failure returns a
// usable logger plus an ErrSentryInit-wrapped error. Flush is always non-nil, so deferring
// it is safe regardless of the error. Call New once per process.
package sentry
