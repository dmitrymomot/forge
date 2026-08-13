package htmx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/web/htmx"
)

func benchHTMXRequest() *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/cart", nil)
	r.Header.Set("HX-Request", "true")
	r.Header.Set("HX-Target", "cart")
	r.Header.Set("HX-Trigger", "add-button")
	return r
}

func BenchmarkIsRequest(b *testing.B) {
	r := benchHTMXRequest()
	b.ReportAllocs()
	for b.Loop() {
		_ = htmx.IsRequest(r)
	}
}

func BenchmarkTarget(b *testing.B) {
	r := benchHTMXRequest()
	b.ReportAllocs()
	for b.Loop() {
		_ = htmx.Target(r)
	}
}

func BenchmarkRedirect(b *testing.B) {
	r := benchHTMXRequest()
	b.ReportAllocs()
	for b.Loop() {
		htmx.Redirect(httptest.NewRecorder(), r, "/invoices")
	}
}

func BenchmarkTrigger(b *testing.B) {
	w := httptest.NewRecorder()
	b.ReportAllocs()
	for b.Loop() {
		htmx.Trigger(w, "cart:updated")
	}
}

// BenchmarkAudible measures the buffering the rewrite needs. The non-htmx path is the
// one most requests take, and it never buffers.
func BenchmarkAudible(b *testing.B) {
	h := htmx.NewAudible()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("fragment"))
	}))

	b.Run("htmx request", func(b *testing.B) {
		r := benchHTMXRequest()
		w := httptest.NewRecorder()
		b.ReportAllocs()
		for b.Loop() {
			h.ServeHTTP(w, r)
		}
	})

	b.Run("plain request", func(b *testing.B) {
		r := httptest.NewRequest(http.MethodGet, "/cart", nil)
		w := httptest.NewRecorder()
		b.ReportAllocs()
		for b.Loop() {
			h.ServeHTTP(w, r)
		}
	})
}
