package htmx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/pkg/htmx"
	"github.com/stretchr/testify/assert"
)

func TestRedirect(t *testing.T) {
	t.Parallel()

	t.Run("HTMX request sets HX-Redirect header and 200 status", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("HX-Request", "true")

		htmx.Redirect(rec, req, "/target")

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "/target", rec.Header().Get("HX-Redirect"))
		assert.Empty(t, rec.Header().Get("Location"))
	})

	t.Run("non-HTMX request uses standard HTTP redirect", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		htmx.Redirect(rec, req, "/target")

		assert.Equal(t, http.StatusFound, rec.Code)
		assert.Equal(t, "/target", rec.Header().Get("Location"))
		assert.Empty(t, rec.Header().Get("HX-Redirect"))
	})

	t.Run("handles empty redirect URL", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("HX-Request", "true")

		htmx.Redirect(rec, req, "")

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "", rec.Header().Get("HX-Redirect"))
	})

	t.Run("handles special characters in URL", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("HX-Request", "true")

		htmx.Redirect(rec, req, "/search?q=test&page=1#results")

		assert.Equal(t, "/search?q=test&page=1#results", rec.Header().Get("HX-Redirect"))
	})

	t.Run("handles absolute URLs", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("HX-Request", "true")

		htmx.Redirect(rec, req, "https://example.com/path")

		assert.Equal(t, "https://example.com/path", rec.Header().Get("HX-Redirect"))
	})
}

func TestRedirectWithStatus(t *testing.T) {
	t.Parallel()

	t.Run("HTMX request ignores custom status and uses 200", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("HX-Request", "true")

		htmx.RedirectWithStatus(rec, req, "/target", http.StatusMovedPermanently)

		assert.Equal(t, http.StatusOK, rec.Code, "HTMX redirect must use 200")
		assert.Equal(t, "/target", rec.Header().Get("HX-Redirect"))
	})

	t.Run("non-HTMX request respects custom status code", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		htmx.RedirectWithStatus(rec, req, "/target", http.StatusMovedPermanently)

		assert.Equal(t, http.StatusMovedPermanently, rec.Code)
		assert.Equal(t, "/target", rec.Header().Get("Location"))
	})

	t.Run("supports 303 See Other status", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/test", nil)

		htmx.RedirectWithStatus(rec, req, "/target", http.StatusSeeOther)

		assert.Equal(t, http.StatusSeeOther, rec.Code)
		assert.Equal(t, "/target", rec.Header().Get("Location"))
	})

	t.Run("supports 307 Temporary Redirect status", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/test", nil)

		htmx.RedirectWithStatus(rec, req, "/target", http.StatusTemporaryRedirect)

		assert.Equal(t, http.StatusTemporaryRedirect, rec.Code)
	})
}

func TestRedirectBack(t *testing.T) {
	t.Parallel()

	t.Run("redirects to URL from redirect query parameter", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test?redirect=/dashboard", nil)
		req.Header.Set("HX-Request", "true")

		htmx.RedirectBack(rec, req, "/fallback")

		assert.Equal(t, "/dashboard", rec.Header().Get("HX-Redirect"))
	})

	t.Run("uses fallback when redirect parameter is missing", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("HX-Request", "true")

		htmx.RedirectBack(rec, req, "/fallback")

		assert.Equal(t, "/fallback", rec.Header().Get("HX-Redirect"))
	})

	t.Run("uses fallback when redirect parameter is empty", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test?redirect=", nil)
		req.Header.Set("HX-Request", "true")

		htmx.RedirectBack(rec, req, "/fallback")

		assert.Equal(t, "/fallback", rec.Header().Get("HX-Redirect"))
	})

	t.Run("works with non-HTMX requests", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test?redirect=/profile", nil)

		htmx.RedirectBack(rec, req, "/fallback")

		assert.Equal(t, http.StatusFound, rec.Code)
		assert.Equal(t, "/profile", rec.Header().Get("Location"))
	})

	t.Run("handles URL-encoded redirect parameter", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test?redirect=%2Fsearch%3Fq%3Dtest", nil)
		req.Header.Set("HX-Request", "true")

		htmx.RedirectBack(rec, req, "/fallback")

		assert.Equal(t, "/search?q=test", rec.Header().Get("HX-Redirect"))
	})

	t.Run("prefers first redirect parameter when multiple provided", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test?redirect=/first&redirect=/second", nil)
		req.Header.Set("HX-Request", "true")

		htmx.RedirectBack(rec, req, "/fallback")

		assert.Equal(t, "/first", rec.Header().Get("HX-Redirect"))
	})

	t.Run("handles absolute URLs in redirect parameter", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test?redirect=https://example.com", nil)
		req.Header.Set("HX-Request", "true")

		htmx.RedirectBack(rec, req, "/fallback")

		assert.Equal(t, "https://example.com", rec.Header().Get("HX-Redirect"))
	})
}
