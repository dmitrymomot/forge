// perf_bench_test.go — performance benchmarks for the contextHandler hot paths.
// Run with: go test -bench=. -benchmem -run=^$ ./logger/
// Kept as a documented performance contract for future refactors of decorator.go.
package logger

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

type benchCtxKey struct{}

// realExtractor reads a value from context — realistic cost, not a no-op.
func realExtractor(ctx context.Context) (slog.Attr, bool) {
	if v, ok := ctx.Value(benchCtxKey{}).(string); ok {
		return slog.String("trace_id", v), true
	}
	return slog.Attr{}, false
}

// alwaysMissExtractor always returns ok=false (extracted-empty path).
func alwaysMissExtractor(context.Context) (slog.Attr, bool) {
	return slog.Attr{}, false
}

func discardJSONHandler() slog.Handler {
	return slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug})
}

func ctxWithTraceID() context.Context {
	return context.WithValue(context.Background(), benchCtxKey{}, "abc-123-def-456")
}

// BenchmarkBaseline_PlainSlog measures plain slog with no decorator, for comparison.
func BenchmarkBaseline_PlainSlog(b *testing.B) {
	log := slog.New(discardJSONHandler())
	b.ReportAllocs()
	for b.Loop() {
		log.Info("hello", "k", "v")
	}
}

// BenchmarkNoExtractors_DecoratorInstalled measures the contextHandler in the chain with
// an empty extractors slice, so every call takes the Handle short-circuit.
func BenchmarkNoExtractors_DecoratorInstalled(b *testing.B) {
	// New() only wraps when extractors > 0, so we build directly to have the
	// contextHandler in the chain but with an empty extractors slice.
	h := newContextHandler(discardJSONHandler()) // len(extractors)==0
	log := slog.New(h)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		log.InfoContext(ctx, "hello", "k", "v")
	}
}

// BenchmarkNoExtractors_ViaNew goes through the public New() to confirm the decorator is
// NOT installed when no extractors are configured.
func BenchmarkNoExtractors_ViaNew(b *testing.B) {
	log, _ := New(WithOutput(io.Discard), WithFormat("json"))
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		log.InfoContext(ctx, "hello", "k", "v")
	}
}

// BenchmarkFastPath_ExtractorMiss measures an extractor present but the context value
// missing, so the extracted slice stays nil.
func BenchmarkFastPath_ExtractorMiss(b *testing.B) {
	h := newContextHandler(discardJSONHandler(), alwaysMissExtractor)
	log := slog.New(h)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		log.InfoContext(ctx, "hello", "k", "v")
	}
}

// BenchmarkFastPath_1Extractor_NoGroup measures one extractor with a context value hit
// and no group active.
func BenchmarkFastPath_1Extractor_NoGroup(b *testing.B) {
	h := newContextHandler(discardJSONHandler(), realExtractor)
	log := slog.New(h)
	ctx := ctxWithTraceID()
	b.ReportAllocs()
	for b.Loop() {
		log.InfoContext(ctx, "hello", "k", "v")
	}
}

// BenchmarkFastPath_3Extractors_NoGroup measures three extractors with a context value
// hit and no group active.
func BenchmarkFastPath_3Extractors_NoGroup(b *testing.B) {
	ex2 := func(context.Context) (slog.Attr, bool) {
		return slog.String("user_id", "u42"), true
	}
	ex3 := func(context.Context) (slog.Attr, bool) {
		return slog.String("svc", "auth"), true
	}
	h := newContextHandler(discardJSONHandler(), realExtractor, ex2, ex3)
	log := slog.New(h)
	ctx := ctxWithTraceID()
	b.ReportAllocs()
	for b.Loop() {
		log.InfoContext(ctx, "hello", "k", "v")
	}
}

// BenchmarkGroupPath_1Group measures the group path, where at least one WithGroup is
// active and the whole chain is rebuilt on every Handle call to keep extracted attrs at
// root.
func BenchmarkGroupPath_1Group(b *testing.B) {
	base := newContextHandler(discardJSONHandler(), realExtractor)
	h := base.WithGroup("request")
	log := slog.New(h)
	ctx := ctxWithTraceID()
	b.ReportAllocs()
	for b.Loop() {
		log.InfoContext(ctx, "hello", "k", "v")
	}
}

func BenchmarkGroupPath_1Group_1Attrs(b *testing.B) {
	base := newContextHandler(discardJSONHandler(), realExtractor)
	h := base.WithGroup("request").WithAttrs([]slog.Attr{slog.String("env", "prod")})
	log := slog.New(h)
	ctx := ctxWithTraceID()
	b.ReportAllocs()
	for b.Loop() {
		log.InfoContext(ctx, "hello", "k", "v")
	}
}

func BenchmarkGroupPath_3Ops(b *testing.B) {
	// Simulates: WithGroup("request") -> WithAttrs([env]) -> WithGroup("db")
	base := newContextHandler(discardJSONHandler(), realExtractor)
	h := base.
		WithGroup("request").
		WithAttrs([]slog.Attr{slog.String("env", "prod")}).
		WithGroup("db")
	log := slog.New(h)
	ctx := ctxWithTraceID()
	b.ReportAllocs()
	for b.Loop() {
		log.InfoContext(ctx, "hello", "k", "v")
	}
}

func BenchmarkGroupPath_5Ops(b *testing.B) {
	base := newContextHandler(discardJSONHandler(), realExtractor)
	h := base.
		WithGroup("request").
		WithAttrs([]slog.Attr{slog.String("env", "prod"), slog.String("ver", "1.2.3")}).
		WithGroup("db").
		WithAttrs([]slog.Attr{slog.String("dsn", "postgres://localhost/app")}).
		WithGroup("cache")
	log := slog.New(h)
	ctx := ctxWithTraceID()
	b.ReportAllocs()
	for b.Loop() {
		log.InfoContext(ctx, "hello", "k", "v")
	}
}
