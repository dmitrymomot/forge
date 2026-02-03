package htmx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/pkg/htmx"
)

func TestIsHTMX(t *testing.T) {
	t.Parallel()

	t.Run("returns true when HX-Request header is true", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("HX-Request", "true")

		assert.True(t, htmx.IsHTMX(req))
	})

	t.Run("returns false when HX-Request header is missing", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		assert.False(t, htmx.IsHTMX(req))
	})

	t.Run("returns false when HX-Request header is false", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("HX-Request", "false")

		assert.False(t, htmx.IsHTMX(req))
	})

	t.Run("returns false when HX-Request header is invalid value", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("HX-Request", "1")

		assert.False(t, htmx.IsHTMX(req))
	})

	t.Run("returns false when HX-Request header is empty string", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("HX-Request", "")

		assert.False(t, htmx.IsHTMX(req))
	})

	t.Run("handles case sensitivity", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("HX-Request", "True")

		assert.False(t, htmx.IsHTMX(req), "should be case-sensitive")
	})
}
