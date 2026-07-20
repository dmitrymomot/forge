package prometheus_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/ops/metrics"
	"github.com/dmitrymomot/forge/ops/metrics/prometheus"
)

func BenchmarkCounterIncLabeled(b *testing.B) {
	c := prometheus.New().Counter("ops_total", "b.", "method", "status")
	b.ReportAllocs()
	for b.Loop() {
		c.Inc("GET", "200")
	}
}

func BenchmarkHistogramObserveLabeled(b *testing.B) {
	h := prometheus.New().Histogram("latency_seconds", "b.", nil, "method", "status")
	b.ReportAllocs()
	for b.Loop() {
		h.Observe(0.042, "GET", "200")
	}
}

func BenchmarkMiddleware(b *testing.B) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", func(http.ResponseWriter, *http.Request) {})
	h := metrics.NewMiddleware(prometheus.New())(mux)
	r := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	w := httptest.NewRecorder()
	b.ReportAllocs()
	for b.Loop() {
		h.ServeHTTP(w, r)
	}
}
