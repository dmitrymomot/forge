package respond_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/web/render"
	"github.com/dmitrymomot/forge/web/respond"
)

// BenchmarkWrapText measures the whole value-returning path against writing the same
// bytes directly, which is the overhead the pattern costs per request.
func BenchmarkWrapText(b *testing.B) {
	h := respond.New().Wrap(func(*http.Request) (respond.Response, error) {
		return respond.Text("hello"), nil
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	b.ReportAllocs()
	for b.Loop() {
		h.ServeHTTP(w, req)
	}
}

func BenchmarkRenderTextDirect(b *testing.B) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = render.Text(w, http.StatusOK, "hello")
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	b.ReportAllocs()
	for b.Loop() {
		h.ServeHTTP(w, req)
	}
}

func BenchmarkWrapJSON(b *testing.B) {
	payload := map[string]string{"status": "ok"}
	h := respond.New().Wrap(func(*http.Request) (respond.Response, error) {
		return respond.JSON(payload), nil
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	b.ReportAllocs()
	for b.Loop() {
		h.ServeHTTP(w, req)
	}
}

func BenchmarkWrapSeeOther(b *testing.B) {
	h := respond.New().Wrap(func(*http.Request) (respond.Response, error) {
		return respond.SeeOther("/invoices"), nil
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	b.ReportAllocs()
	for b.Loop() {
		h.ServeHTTP(w, req)
	}
}
