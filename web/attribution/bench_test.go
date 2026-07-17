package attribution_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/web/attribution"
	"github.com/dmitrymomot/forge/web/cookie"
	"github.com/dmitrymomot/forge/web/middleware"
)

func benchTracker(b *testing.B) *attribution.Tracker {
	b.Helper()
	ks, err := keyset.New(keyset.WithPrimary(1, make([]byte, 32)))
	if err != nil {
		b.Fatal(err)
	}
	codec, err := cookie.New(ks)
	if err != nil {
		b.Fatal(err)
	}
	return attribution.New(codec)
}

var benchNoop = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

func BenchmarkMiddleware_NoQuery(b *testing.B) {
	h := middleware.Wrap(benchNoop, benchTracker(b).Middleware())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	b.ReportAllocs()
	for b.Loop() {
		h.ServeHTTP(rec, req)
	}
}

func BenchmarkMiddleware_UnrelatedQuery(b *testing.B) {
	h := middleware.Wrap(benchNoop, benchTracker(b).Middleware())
	req := httptest.NewRequest(http.MethodGet, "/?page=2&sort=asc&q=shoes", nil)
	rec := httptest.NewRecorder()
	b.ReportAllocs()
	for b.Loop() {
		h.ServeHTTP(rec, req)
	}
}

func BenchmarkMiddleware_Capture(b *testing.B) {
	h := middleware.Wrap(benchNoop, benchTracker(b).Middleware())
	req := httptest.NewRequest(http.MethodGet, "/?utm_source=google&utm_medium=cpc&utm_campaign=launch&gclid=CjkKCQjw", nil)
	b.ReportAllocs()
	for b.Loop() {
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
}

func BenchmarkTouch_CookieRead(b *testing.B) {
	tr := benchTracker(b)
	rec := httptest.NewRecorder()
	h := middleware.Wrap(benchNoop, tr.Middleware())
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?utm_source=google&utm_medium=cpc&gclid=CjkKCQjw", nil))
	req := httptest.NewRequest(http.MethodPost, "/signup", nil)
	for _, ck := range rec.Result().Cookies() {
		req.AddCookie(ck)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := tr.Touch(req); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPixel(b *testing.B) {
	h := benchTracker(b).Pixel()
	req := httptest.NewRequest(http.MethodGet, "/pixel.gif?utm_source=email", nil)
	b.ReportAllocs()
	for b.Loop() {
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
}
