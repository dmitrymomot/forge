package debug_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/ops/debug"
)

func BenchmarkSnapshot(b *testing.B) {
	for b.Loop() {
		_ = debug.Snapshot()
	}
}

func BenchmarkStatsHandler(b *testing.B) {
	h := debug.Handler()
	req := httptest.NewRequest(http.MethodGet, "/debug/stats", nil)
	b.ReportAllocs()
	for b.Loop() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
	}
}

// BenchmarkIndexBasicAuth measures the guard + mux dispatch overhead on the
// cheapest route, isolating it from snapshot/profile cost.
func BenchmarkIndexBasicAuth(b *testing.B) {
	h := debug.Handler(debug.WithBasicAuth(map[string]string{"ops": "s3cret"}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("ops", "s3cret")
	b.ReportAllocs()
	for b.Loop() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
	}
}
