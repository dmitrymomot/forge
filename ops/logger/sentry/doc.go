// Package sentry provides a Sentry slog.Handler for logger.WithHandler, keeping the
// Sentry SDK out of the core logger's import graph.
//
// # Usage
//
//	sentryHandler, flush, err := sentry.NewHandler(sentry.WithConfig(sentry.Config{
//		DSN:      os.Getenv("SENTRY_DSN"), // empty → disabled handler, no error
//		MinLevel: "warn",
//	}))
//	if err != nil {
//		// non-fatal: sentryHandler is disabled but safe to use — keep going
//	}
//	log, err := logger.New(logger.WithHandler(sentryHandler)) // or logger.NewAsync
//	defer flush(context.Background()) // ships buffered events; no-op when inactive
//
// Error-level (and above) records are reported to Sentry as Issues via
// sentry.CaptureException; the MinLevel..error range is sent as Sentry Logs when
// EnableLogs is set. (The SDK's deprecated log-to-event conversion is not used.)
//
// NewHandler ALWAYS returns a usable handler and a non-nil Flush: an empty DSN yields a
// disabled handler (Enabled reports false) and no error; an invalid config or SDK init
// failure yields a disabled handler plus the error. Sentry being down never takes logging
// down with it. With logger.NewAsync, call the logger's CloseFunc before flush so buffered
// records reach the handler before events ship. Call NewHandler once per process (it
// initializes the global Sentry hub).
package sentry
