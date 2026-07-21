package tracing_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/ops/tracing"
)

func BenchmarkParseTraceparent(b *testing.B) {
	for b.Loop() {
		if _, err := tracing.ParseTraceparent(sampleTraceparent); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTraceparent(b *testing.B) {
	sc, err := tracing.ParseTraceparent(sampleTraceparent)
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		if sc.Traceparent() == "" {
			b.Fatal("empty")
		}
	}
}

func BenchmarkStartChild(b *testing.B) {
	tr := tracing.New()
	ctx, parent := tr.Start(b.Context(), "parent")
	defer parent.End()
	b.ResetTimer()
	for b.Loop() {
		_, span := tr.Start(ctx, "child")
		span.End()
	}
}

func BenchmarkSpanContextFromContext(b *testing.B) {
	tr := tracing.New()
	ctx, span := tr.Start(b.Context(), "s")
	defer span.End()
	b.ResetTimer()
	for b.Loop() {
		if !tracing.SpanContextFromContext(ctx).IsValid() {
			b.Fatal("invalid")
		}
	}
}

func BenchmarkLogExtractor(b *testing.B) {
	tr := tracing.New()
	ctx, span := tr.Start(b.Context(), "s")
	defer span.End()
	b.ResetTimer()
	for b.Loop() {
		if _, ok := tracing.LogExtractor(ctx); !ok {
			b.Fatal("missing")
		}
	}
}

func BenchmarkPropagationHeaders(b *testing.B) {
	tr := tracing.New()
	ctx, span := tr.Start(b.Context(), "s")
	defer span.End()
	b.ResetTimer()
	for b.Loop() {
		if tracing.PropagationHeaders(ctx) == nil {
			b.Fatal("nil")
		}
	}
}

func BenchmarkMiddleware(b *testing.B) {
	h := tracing.NewMiddleware(tracing.New())(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("traceparent", sampleTraceparent)
	w := httptest.NewRecorder()
	b.ResetTimer()
	for b.Loop() {
		h.ServeHTTP(w, r)
	}
}
