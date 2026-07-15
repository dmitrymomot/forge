package sentry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
