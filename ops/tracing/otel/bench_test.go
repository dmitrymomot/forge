package otel_test

import (
	"log/slog"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/dmitrymomot/forge/ops/tracing/otel"
)

func BenchmarkStartEnd(b *testing.B) {
	tp := sdktrace.NewTracerProvider() // no exporter: measures the adapter + SDK span path
	b.Cleanup(func() { _ = tp.Shutdown(b.Context()) })
	tr := otel.New(tp)
	ctx, parent := tr.Start(b.Context(), "parent")
	defer parent.End()
	b.ResetTimer()
	for b.Loop() {
		_, span := tr.Start(ctx, "child")
		span.End()
	}
}

func BenchmarkSetAttributes(b *testing.B) {
	tp := sdktrace.NewTracerProvider()
	b.Cleanup(func() { _ = tp.Shutdown(b.Context()) })
	tr := otel.New(tp)
	_, span := tr.Start(b.Context(), "s")
	defer span.End()
	attrs := []slog.Attr{
		slog.String("http.request.method", "GET"),
		slog.Int("http.response.status_code", 200),
		slog.Group("g", slog.String("inner", "x")),
	}
	b.ResetTimer()
	for b.Loop() {
		span.SetAttributes(attrs...)
	}
}
