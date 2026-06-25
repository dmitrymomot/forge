package htmx_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/htmx"
)

func TestLocation(t *testing.T) {
	t.Parallel()

	t.Run("HTMX request sets HX-Location header", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("HX-Request", "true")

		htmx.Location(rec, req, "/dashboard")

		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "/dashboard", rec.Header().Get("HX-Location"))
		require.Empty(t, rec.Header().Get("Location"))
	})

	t.Run("non-HTMX request uses standard redirect", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		htmx.Location(rec, req, "/dashboard")

		require.Equal(t, http.StatusFound, rec.Code)
		require.Equal(t, "/dashboard", rec.Header().Get("Location"))
		require.Empty(t, rec.Header().Get("HX-Location"))
	})

	t.Run("handles empty path", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("HX-Request", "true")

		htmx.Location(rec, req, "")

		require.Equal(t, "", rec.Header().Get("HX-Location"))
	})

	t.Run("handles query parameters", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("HX-Request", "true")

		htmx.Location(rec, req, "/search?q=test&page=2")

		require.Equal(t, "/search?q=test&page=2", rec.Header().Get("HX-Location"))
	})

	t.Run("handles fragment identifiers", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("HX-Request", "true")

		htmx.Location(rec, req, "/page#section")

		require.Equal(t, "/page#section", rec.Header().Get("HX-Location"))
	})
}

func TestLocationTarget(t *testing.T) {
	t.Parallel()

	t.Run("HTMX request sets JSON location options with target", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("HX-Request", "true")

		htmx.LocationTarget(rec, req, "/content", "#main")

		require.Equal(t, http.StatusOK, rec.Code)

		var opts htmx.LocationOptions
		err := json.Unmarshal([]byte(rec.Header().Get("HX-Location")), &opts)
		require.NoError(t, err)

		require.Equal(t, "/content", opts.Path)
		require.Equal(t, "#main", opts.Target)
	})

	t.Run("non-HTMX request uses standard redirect", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		htmx.LocationTarget(rec, req, "/content", "#main")

		require.Equal(t, http.StatusFound, rec.Code)
		require.Equal(t, "/content", rec.Header().Get("Location"))
	})

	t.Run("handles empty target", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("HX-Request", "true")

		htmx.LocationTarget(rec, req, "/content", "")

		var opts htmx.LocationOptions
		err := json.Unmarshal([]byte(rec.Header().Get("HX-Location")), &opts)
		require.NoError(t, err)

		require.Equal(t, "/content", opts.Path)
		require.Empty(t, opts.Target)
	})

	t.Run("handles complex CSS selectors", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("HX-Request", "true")

		htmx.LocationTarget(rec, req, "/content", "div.container > ul:first-child")

		var opts htmx.LocationOptions
		err := json.Unmarshal([]byte(rec.Header().Get("HX-Location")), &opts)
		require.NoError(t, err)

		require.Equal(t, "div.container > ul:first-child", opts.Target)
	})
}

func TestLocationWithOptions(t *testing.T) {
	t.Parallel()

	t.Run("HTMX request serializes full options to JSON", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("HX-Request", "true")

		opts := htmx.LocationOptions{
			Path:   "/api/data",
			Target: "#content",
			Swap:   "innerHTML",
			Select: ".items",
		}

		htmx.LocationWithOptions(rec, req, opts)

		require.Equal(t, http.StatusOK, rec.Code)

		var result htmx.LocationOptions
		err := json.Unmarshal([]byte(rec.Header().Get("HX-Location")), &result)
		require.NoError(t, err)

		require.Equal(t, "/api/data", result.Path)
		require.Equal(t, "#content", result.Target)
		require.Equal(t, "innerHTML", result.Swap)
		require.Equal(t, ".items", result.Select)
	})

	t.Run("handles options with values map", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("HX-Request", "true")

		opts := htmx.LocationOptions{
			Path: "/search",
			Values: map[string]string{
				"q":    "test",
				"page": "1",
			},
		}

		htmx.LocationWithOptions(rec, req, opts)

		var result htmx.LocationOptions
		err := json.Unmarshal([]byte(rec.Header().Get("HX-Location")), &result)
		require.NoError(t, err)

		require.Equal(t, "test", result.Values["q"])
		require.Equal(t, "1", result.Values["page"])
	})

	t.Run("handles options with headers map", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("HX-Request", "true")

		opts := htmx.LocationOptions{
			Path: "/api",
			Headers: map[string]string{
				"X-Custom-Header": "value",
				"Authorization":   "Bearer token",
			},
		}

		htmx.LocationWithOptions(rec, req, opts)

		var result htmx.LocationOptions
		err := json.Unmarshal([]byte(rec.Header().Get("HX-Location")), &result)
		require.NoError(t, err)

		require.Equal(t, "value", result.Headers["X-Custom-Header"])
		require.Equal(t, "Bearer token", result.Headers["Authorization"])
	})

	t.Run("handles all option fields", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("HX-Request", "true")

		opts := htmx.LocationOptions{
			Path:    "/full",
			Source:  "#trigger",
			Event:   "click",
			Handler: "customHandler",
			Target:  "#target",
			Swap:    "outerHTML",
			Select:  ".content",
			Values: map[string]string{
				"key": "value",
			},
			Headers: map[string]string{
				"X-Test": "header",
			},
		}

		htmx.LocationWithOptions(rec, req, opts)

		var result htmx.LocationOptions
		err := json.Unmarshal([]byte(rec.Header().Get("HX-Location")), &result)
		require.NoError(t, err)

		require.Equal(t, "/full", result.Path)
		require.Equal(t, "#trigger", result.Source)
		require.Equal(t, "click", result.Event)
		require.Equal(t, "customHandler", result.Handler)
		require.Equal(t, "#target", result.Target)
		require.Equal(t, "outerHTML", result.Swap)
		require.Equal(t, ".content", result.Select)
		require.Equal(t, "value", result.Values["key"])
		require.Equal(t, "header", result.Headers["X-Test"])
	})

	t.Run("omits empty optional fields in JSON", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("HX-Request", "true")

		opts := htmx.LocationOptions{
			Path: "/minimal",
		}

		htmx.LocationWithOptions(rec, req, opts)

		jsonStr := rec.Header().Get("HX-Location")
		require.NotContains(t, jsonStr, "source")
		require.NotContains(t, jsonStr, "event")
		require.NotContains(t, jsonStr, "handler")
		require.NotContains(t, jsonStr, "target")
		require.NotContains(t, jsonStr, "swap")
		require.NotContains(t, jsonStr, "values")
		require.NotContains(t, jsonStr, "headers")
		require.NotContains(t, jsonStr, "select")
	})

	t.Run("non-HTMX request uses standard redirect with path", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		opts := htmx.LocationOptions{
			Path:   "/target",
			Target: "#ignored",
		}

		htmx.LocationWithOptions(rec, req, opts)

		require.Equal(t, http.StatusFound, rec.Code)
		require.Equal(t, "/target", rec.Header().Get("Location"))
		require.Empty(t, rec.Header().Get("HX-Location"))
	})

	t.Run("handles empty maps gracefully", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("HX-Request", "true")

		opts := htmx.LocationOptions{
			Path:    "/test",
			Values:  map[string]string{},
			Headers: map[string]string{},
		}

		htmx.LocationWithOptions(rec, req, opts)

		var result htmx.LocationOptions
		err := json.Unmarshal([]byte(rec.Header().Get("HX-Location")), &result)
		require.NoError(t, err)

		require.Equal(t, "/test", result.Path)
	})

	t.Run("handles nil maps gracefully", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("HX-Request", "true")

		opts := htmx.LocationOptions{
			Path:    "/test",
			Values:  nil,
			Headers: nil,
		}

		htmx.LocationWithOptions(rec, req, opts)

		var result htmx.LocationOptions
		err := json.Unmarshal([]byte(rec.Header().Get("HX-Location")), &result)
		require.NoError(t, err)

		require.Equal(t, "/test", result.Path)
	})

	t.Run("handles special characters in values", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("HX-Request", "true")

		opts := htmx.LocationOptions{
			Path: "/search",
			Values: map[string]string{
				"query": "test & verify <script>",
				"emoji": "🚀",
			},
		}

		htmx.LocationWithOptions(rec, req, opts)

		var result htmx.LocationOptions
		err := json.Unmarshal([]byte(rec.Header().Get("HX-Location")), &result)
		require.NoError(t, err)

		require.Equal(t, "test & verify <script>", result.Values["query"])
		require.Equal(t, "🚀", result.Values["emoji"])
	})
}
