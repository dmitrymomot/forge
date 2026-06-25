package sentry

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/dmitrymomot/forge/logger"
)

// New builds a logger that writes to the primary destination and, when DSN is non-empty,
// also reports to Sentry: records at Error and above become Issues (via
// sentry.CaptureException), and records from MinLevel up to error become Sentry Logs when
// EnableLogs is set. Empty DSN returns a plain logger and a no-op
// Flush; an init failure returns a usable plain logger plus an ErrSentryInit-wrapped
// error. The returned Flush is always non-nil (a no-op when Sentry is inactive), so it is
// safe to defer regardless of the error; on a fatal config error the logger is nil and the
// error is set — check it before logging. Call New once per process (it initializes the
// global Sentry hub).
func New(opts ...Option) (*slog.Logger, Flush, error) {
	return newWith(realSentryHandler, opts...)
}

// newWith is the test seam: New passes realSentryHandler; tests pass a fake builder.
func newWith(buildHandler func(Config) (slog.Handler, error), opts ...Option) (*slog.Logger, Flush, error) {
	c := defaultConfig()
	for _, opt := range opts {
		opt(&c)
	}
	if len(c.errs) > 0 {
		return nil, noopFlush, errors.Join(c.errs...)
	}
	if err := c.Validate(); err != nil {
		return nil, noopFlush, err
	}

	loggerOpts := []logger.Option{logger.WithConfig(c.Config.Config)} // embedded logger.Config
	if len(c.extractors) > 0 {
		loggerOpts = append(loggerOpts, logger.WithContextExtractors(c.extractors...))
	}
	if c.output != nil {
		loggerOpts = append(loggerOpts, logger.WithOutput(c.output))
	}

	if c.DSN == "" { // Sentry disabled — plain logger, no-op flush
		l, err := logger.New(loggerOpts...)
		return l, noopFlush, err
	}

	sh, err := buildHandler(c.Config)
	if err != nil { // graceful: keep logging, surface the error
		l, lerr := logger.New(loggerOpts...)
		if lerr != nil {
			return nil, noopFlush, lerr
		}
		return l, noopFlush, fmt.Errorf("%w: %v", ErrSentryInit, err)
	}

	l, err := logger.New(append(loggerOpts, logger.WithHandler(sh))...)
	if err != nil {
		return nil, noopFlush, err
	}
	return l, flush, nil
}
