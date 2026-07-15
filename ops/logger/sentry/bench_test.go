package sentry_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/ops/logger/sentry"
)

// BenchmarkNewHandlerDisabled measures NewHandler with the default (empty-DSN) config,
// which short-circuits to the disabled handler without touching the global Sentry hub.
func BenchmarkNewHandlerDisabled(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_, _, _ = sentry.NewHandler()
	}
}

// BenchmarkDisabledHandlerHandle measures the per-record cost of the disabled handler's
// Handle, i.e. the overhead paid on every log call when Sentry is inactive.
func BenchmarkDisabledHandlerHandle(b *testing.B) {
	h, _, err := sentry.NewHandler() // empty DSN -> disabled handler
	if err != nil {
		b.Fatalf("NewHandler: %v", err)
	}
	ctx := context.Background()
	rec := slog.NewRecord(time.Time{}, slog.LevelError, "x", 0)

	b.ReportAllocs()
	for b.Loop() {
		_ = h.Handle(ctx, rec)
	}
}
