package hostrouter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func benchHandler() http.Handler {
	return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
}

func benchRequest(host string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "http://x/", nil)
	req.Host = host
	return req
}

func BenchmarkNormalizeHost(b *testing.B) {
	cases := []string{"foo.example.com", "example.com:8080", "[::1]:8080", "API.Example.COM"}
	for _, c := range cases {
		b.Run(c, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = normalizeHost(c)
			}
		})
	}
}

func BenchmarkServeHTTP_Exact(b *testing.B) {
	r := mustNew(b, WithHost("api.example.com", benchHandler()))
	req, rec := benchRequest("api.example.com"), httptest.NewRecorder()
	b.ReportAllocs()
	for b.Loop() {
		r.ServeHTTP(rec, req)
	}
}

func BenchmarkServeHTTP_Wildcard(b *testing.B) {
	r := mustNew(b, WithHost("*.example.com", benchHandler()))
	req, rec := benchRequest("foo.example.com"), httptest.NewRecorder()
	b.ReportAllocs()
	for b.Loop() {
		r.ServeHTTP(rec, req)
	}
}

func BenchmarkServeHTTP_WildcardNoContext(b *testing.B) {
	r := mustNew(b, WithHost("*.example.com", benchHandler()), WithoutMatchContext())
	req, rec := benchRequest("foo.example.com"), httptest.NewRecorder()
	b.ReportAllocs()
	for b.Loop() {
		r.ServeHTTP(rec, req)
	}
}

func BenchmarkServeHTTP_Fallback(b *testing.B) {
	r := mustNew(b, WithHost("api.example.com", benchHandler()), WithFallback(benchHandler()))
	req, rec := benchRequest("unknown.com"), httptest.NewRecorder()
	b.ReportAllocs()
	for b.Loop() {
		r.ServeHTTP(rec, req)
	}
}

func BenchmarkServeHTTP_Lookup(b *testing.B) {
	h := benchHandler()
	r := mustNew(b,
		WithHost("api.example.com", h),
		WithLookup(func(context.Context, string) (http.Handler, error) { return h, nil }),
	)
	req, rec := benchRequest("shop.customer.tld"), httptest.NewRecorder()
	b.ReportAllocs()
	for b.Loop() {
		r.ServeHTTP(rec, req)
	}
}

// BenchmarkServeHTTP_LookupNotConfigured pins the cost of the lookup branch for the
// common single-tenant case, where no WithLookup is registered at all.
func BenchmarkServeHTTP_LookupNotConfigured(b *testing.B) {
	r := mustNew(b, WithHost("api.example.com", benchHandler()), WithFallback(benchHandler()))
	req, rec := benchRequest("shop.customer.tld"), httptest.NewRecorder()
	b.ReportAllocs()
	for b.Loop() {
		r.ServeHTTP(rec, req)
	}
}

func BenchmarkServeHTTP_Parallel(b *testing.B) {
	r := mustNew(b, WithHost("*.example.com", benchHandler()))
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		req, rec := benchRequest("foo.example.com"), httptest.NewRecorder()
		for pb.Next() {
			r.ServeHTTP(rec, req)
		}
	})
}

// TestZeroAllocFallback locks the no-match path at zero allocations. The fallback is
// a no-op handler so the measurement reflects routing only, not the 404 writer.
func TestZeroAllocFallback(t *testing.T) {
	r := mustNew(t, WithHost("api.example.com", benchHandler()), WithFallback(benchHandler()))
	req, rec := benchRequest("unknown.com"), httptest.NewRecorder()
	avg := testing.AllocsPerRun(100, func() { r.ServeHTTP(rec, req) })
	assert.Zero(t, avg, "fallback routing must not allocate")
}

// TestZeroAllocWithoutMatchContext locks the matched path at zero allocations when
// context injection is disabled.
func TestZeroAllocWithoutMatchContext(t *testing.T) {
	r := mustNew(t, WithHost("*.example.com", benchHandler()), WithoutMatchContext())
	req, rec := benchRequest("foo.example.com"), httptest.NewRecorder()
	avg := testing.AllocsPerRun(100, func() { r.ServeHTTP(rec, req) })
	assert.Zero(t, avg, "matched path with WithoutMatchContext must not allocate")
}
