package subroute_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/web/subroute"
)

// BenchmarkMux_Direct is the no-mount baseline: the same route registered
// directly on one mux. The Mount benchmarks read against this delta.
func BenchmarkMux_Direct(b *testing.B) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/users/{id}", func(http.ResponseWriter, *http.Request) {})

	req := httptest.NewRequest(http.MethodGet, "/admin/users/42", nil)
	rec := httptest.NewRecorder()
	b.ReportAllocs()
	for b.Loop() {
		mux.ServeHTTP(rec, req)
	}
}

func BenchmarkMount_Static(b *testing.B) {
	inner := http.NewServeMux()
	inner.HandleFunc("GET /users/{id}", func(http.ResponseWriter, *http.Request) {})
	mux := http.NewServeMux()
	subroute.Mount(mux, "/admin", inner)

	req := httptest.NewRequest(http.MethodGet, "/admin/users/42", nil)
	rec := httptest.NewRecorder()
	b.ReportAllocs()
	for b.Loop() {
		mux.ServeHTTP(rec, req)
	}
}

func BenchmarkMount_Wildcard(b *testing.B) {
	inner := http.NewServeMux()
	inner.HandleFunc("GET /reports/{id}", func(http.ResponseWriter, *http.Request) {})
	mux := http.NewServeMux()
	subroute.Mount(mux, "/app/{tenant}/dashboard", inner)

	req := httptest.NewRequest(http.MethodGet, "/app/acme/dashboard/reports/7", nil)
	rec := httptest.NewRecorder()
	b.ReportAllocs()
	for b.Loop() {
		mux.ServeHTTP(rec, req)
	}
}
