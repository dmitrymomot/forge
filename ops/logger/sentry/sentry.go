package sentry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/dmitrymomot/forge/ops/logger"
)

// disabledHandler is returned whenever Sentry is inactive (empty DSN, invalid config, or
// init failure). Enabled always reports false, so it is safe — and free — to pass to
// logger.WithHandler unconditionally.
type disabledHandler struct{}

func (disabledHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (disabledHandler) Handle(context.Context, slog.Record) error { return nil }
func (disabledHandler) WithAttrs([]slog.Attr) slog.Handler        { return disabledHandler{} }
func (disabledHandler) WithGroup(string) slog.Handler             { return disabledHandler{} }

// NewHandler builds the Sentry slog.Handler for logger.WithHandler. It ALWAYS returns a
// usable handler and a non-nil Flush: an empty DSN yields a disabled handler and no error;
// an invalid config or SDK init failure yields a disabled handler plus the error, so the
// app keeps logging while the problem is surfaced. Records at Error and above become
// Sentry Issues; MinLevel..error become Sentry Logs when EnableLogs is set. Call NewHandler
// once per process (it initializes the global Sentry hub).
func NewHandler(opts ...Option) (slog.Handler, Flush, error) {
	return newHandlerWith(realSentryHandler, opts...)
}

// newHandlerWith is the test seam: NewHandler passes realSentryHandler; tests pass a fake.
func newHandlerWith(buildHandler func(Config) (slog.Handler, error), opts ...Option) (slog.Handler, Flush, error) {
	c := defaultConfig()
	for _, opt := range opts {
		opt(&c)
	}
	if len(c.errs) > 0 {
		return disabledHandler{}, noopFlush, errors.Join(c.errs...)
	}
	if err := c.Validate(); err != nil {
		return disabledHandler{}, noopFlush, err
	}
	if c.DSN == "" {
		return disabledHandler{}, noopFlush, nil
	}
	sh, err := buildHandler(c.Config)
	if err != nil {
		return disabledHandler{}, noopFlush, fmt.Errorf("%w: %v", ErrSentryInit, err)
	}
	return sh, flush, nil
}

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
