// Package sentry builds a logger.New-style *slog.Logger that also reports to Sentry in
// parallel, at its own MinLevel, while keeping the Sentry SDK out of the core logger's
// import graph.
//
//	log, flush, err := sentry.New(
//		sentry.WithConfig(sentry.Config{
//			Config:   logger.Config{Level: "info", Format: "json"},
//			DSN:      os.Getenv("SENTRY_DSN"), // empty → plain logger
//			MinLevel: "warn",
//		}),
//		sentry.WithContextExtractors(reqIDExtractor),
//	)
//	defer flush(ctx) // flushes buffered events; no-op when Sentry is inactive
//
// Config embeds logger.Config so primary-logger and Sentry settings env-load together.
// An empty DSN returns a plain logger and a no-op Flush; a Sentry init failure returns a
// usable logger plus an ErrSentryInit-wrapped error. Call New once per process.
package sentry
