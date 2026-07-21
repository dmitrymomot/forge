package risk_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/web/risk"
)

func benchEngine(b *testing.B, n int, opts ...risk.Option[string]) *risk.Engine[string] {
	b.Helper()
	all := make([]risk.Option[string], 0, n+2)
	for range n {
		all = append(all, risk.WithScorer(constScorer(0.1)))
	}
	all = append(all, risk.WithGate[string](0.8))
	all = append(all, opts...)
	e, err := risk.New(all...)
	if err != nil {
		b.Fatal(err)
	}
	return e
}

func BenchmarkCheckPass(b *testing.B) {
	ctx := context.Background()
	for _, n := range []int{1, 4, 16} {
		b.Run(map[int]string{1: "1scorer", 4: "4scorers", 16: "16scorers"}[n], func(b *testing.B) {
			e := benchEngine(b, n)
			b.ReportAllocs()
			for b.Loop() {
				if err := e.Check(ctx, "visit"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCheckPassMean(b *testing.B) {
	ctx := context.Background()
	e := benchEngine(b, 4, risk.WithStrategy[string](risk.Mean))
	b.ReportAllocs()
	for b.Loop() {
		if err := e.Check(ctx, "visit"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCheckPassWeighted(b *testing.B) {
	ctx := context.Background()
	e := benchEngine(b, 4, risk.WithWeights[string](1, 2, 3, 4))
	b.ReportAllocs()
	for b.Loop() {
		if err := e.Check(ctx, "visit"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCheckTrip(b *testing.B) {
	ctx := context.Background()
	e, err := risk.New(
		risk.WithScorer(constScorer(0.9)),
		risk.WithScorer(constScorer(0.1)),
		risk.WithScorer(constScorer(0.2)),
		risk.WithScorer(constScorer(0.3)),
		risk.WithGate[string](0.8),
	)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if e.Check(ctx, "visit") == nil {
			b.Fatal("expected trip")
		}
	}
}

func BenchmarkScore(b *testing.B) {
	ctx := context.Background()
	e := benchEngine(b, 4)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := e.Score(ctx, "visit"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMiddlewarePass(b *testing.B) {
	e := benchEngine(b, 4)
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	h := risk.Middleware(e, func(r *http.Request) string { return r.URL.Path })(next)
	req := httptest.NewRequest(http.MethodGet, "/click", nil)
	rec := httptest.NewRecorder()
	b.ReportAllocs()
	for b.Loop() {
		h.ServeHTTP(rec, req)
	}
}
