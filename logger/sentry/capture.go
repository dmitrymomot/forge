package sentry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	sentry "github.com/getsentry/sentry-go"
)

// sentryCapturer reports a record to Sentry as an exception. It is a seam so the capture
// path is unit-testable without initializing the global Sentry hub.
type sentryCapturer func(ctx context.Context, rec slog.Record, attrs []slog.Attr)

// captureHandler reports records at or above issueLevel to Sentry as exceptions (Issues)
// via sentry.CaptureException — the non-deprecated replacement for sentryslog's removed
// EventLevel log->event conversion — then delegates every record to next (the Logs handler).
type captureHandler struct {
	next    slog.Handler
	capture sentryCapturer
	attrs   []slog.Attr
	level   slog.Level
}

func (h *captureHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level || h.next.Enabled(ctx, level)
}

func (h *captureHandler) Handle(ctx context.Context, rec slog.Record) error {
	if rec.Level >= h.level {
		h.capture(ctx, rec, h.attrs)
	}
	return h.next.Handle(ctx, rec)
}

func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	merged := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	merged = append(merged, h.attrs...)
	merged = append(merged, attrs...)
	return &captureHandler{next: h.next.WithAttrs(attrs), capture: h.capture, attrs: merged, level: h.level}
}

func (h *captureHandler) WithGroup(name string) slog.Handler {
	return &captureHandler{next: h.next.WithGroup(name), capture: h.capture, attrs: h.attrs, level: h.level}
}

// errorFromRecord builds the error passed to CaptureException: the first error-valued
// attribute wrapped with the record message (preserving the original for grouping and any
// stack trace), or the message alone when no error attribute is present.
func errorFromRecord(rec slog.Record) error {
	var inner error
	rec.Attrs(func(a slog.Attr) bool {
		if e, ok := a.Value.Any().(error); ok {
			inner = e
			return false
		}
		return true
	})
	if inner != nil {
		return fmt.Errorf("%s: %w", rec.Message, inner)
	}
	return errors.New(rec.Message)
}

// captureException is the production sentryCapturer: it reports the record to the context's
// hub (or the current hub) as an exception, attaching the handler's accumulated attributes
// and the record's own attributes under a "log" context for triage.
func captureException(ctx context.Context, rec slog.Record, attrs []slog.Attr) {
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub()
	}
	hub.WithScope(func(scope *sentry.Scope) {
		data := sentry.Context{}
		for _, a := range attrs {
			data[a.Key] = a.Value.Any()
		}
		rec.Attrs(func(a slog.Attr) bool {
			data[a.Key] = a.Value.Any()
			return true
		})
		if len(data) > 0 {
			scope.SetContext("log", data)
		}
		hub.CaptureException(errorFromRecord(rec))
	})
}
