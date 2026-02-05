package middlewares_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/internal"
	"github.com/dmitrymomot/forge/middlewares"
)

func TestCORS(t *testing.T) {
	t.Parallel()

	t.Run("default configuration allows all origins", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Origin", "http://example.com")
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		mw := middlewares.CORS()
		handler := mw(func(c internal.Context) error {
			return c.NoContent(http.StatusOK)
		})

		err := handler(ctx)
		require.NoError(t, err)
		require.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("no CORS headers when Origin header is missing", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		mw := middlewares.CORS()
		handler := mw(func(c internal.Context) error {
			return c.NoContent(http.StatusOK)
		})

		err := handler(ctx)
		require.NoError(t, err)
		require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("specific origins list", func(t *testing.T) {
		t.Parallel()

		mw := middlewares.CORS(
			middlewares.WithAllowOrigins("http://allowed.com", "http://also-allowed.com"),
		)

		t.Run("allows listed origin", func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Origin", "http://allowed.com")
			rec := httptest.NewRecorder()
			ctx := newTestContext(rec, req)

			handler := mw(func(c internal.Context) error {
				return c.NoContent(http.StatusOK)
			})

			err := handler(ctx)
			require.NoError(t, err)
			require.Equal(t, "http://allowed.com", rec.Header().Get("Access-Control-Allow-Origin"))
		})

		t.Run("blocks unlisted origin", func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Origin", "http://blocked.com")
			rec := httptest.NewRecorder()
			ctx := newTestContext(rec, req)

			handler := mw(func(c internal.Context) error {
				return c.NoContent(http.StatusOK)
			})

			err := handler(ctx)
			require.NoError(t, err)
			require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
		})
	})

	t.Run("AllowOriginFunc overrides AllowOrigins", func(t *testing.T) {
		t.Parallel()

		// AllowOrigins has "http://static.com" but AllowOriginFunc only allows "http://dynamic.com"
		mw := middlewares.CORS(
			middlewares.WithAllowOrigins("http://static.com"),
			middlewares.WithAllowOriginFunc(func(origin string) bool {
				return origin == "http://dynamic.com"
			}),
		)

		t.Run("allows origin via func", func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Origin", "http://dynamic.com")
			rec := httptest.NewRecorder()
			ctx := newTestContext(rec, req)

			handler := mw(func(c internal.Context) error {
				return c.NoContent(http.StatusOK)
			})

			err := handler(ctx)
			require.NoError(t, err)
			require.Equal(t, "http://dynamic.com", rec.Header().Get("Access-Control-Allow-Origin"))
		})

		t.Run("blocks origin from static list when func is set", func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Origin", "http://static.com")
			rec := httptest.NewRecorder()
			ctx := newTestContext(rec, req)

			handler := mw(func(c internal.Context) error {
				return c.NoContent(http.StatusOK)
			})

			err := handler(ctx)
			require.NoError(t, err)
			// Static origin is blocked because AllowOriginFunc returns false
			require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
		})
	})

	t.Run("AllowOriginFunc returns false blocks origin", func(t *testing.T) {
		t.Parallel()

		mw := middlewares.CORS(
			middlewares.WithAllowOriginFunc(func(origin string) bool {
				return false // Always reject
			}),
		)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Origin", "http://any.com")
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		handler := mw(func(c internal.Context) error {
			return c.NoContent(http.StatusOK)
		})

		err := handler(ctx)
		require.NoError(t, err)
		require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("preflight request handling", func(t *testing.T) {
		t.Parallel()

		mw := middlewares.CORS(
			middlewares.WithAllowMethods("GET", "POST", "PUT"),
			middlewares.WithAllowHeaders("Content-Type", "X-Custom-Header"),
			middlewares.WithMaxAge(1*time.Hour),
		)

		req := httptest.NewRequest(http.MethodOptions, "/", nil)
		req.Header.Set("Origin", "http://example.com")
		req.Header.Set("Access-Control-Request-Method", "POST")
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		handlerCalled := false
		handler := mw(func(c internal.Context) error {
			handlerCalled = true
			return c.NoContent(http.StatusOK)
		})

		err := handler(ctx)
		require.NoError(t, err)

		// Preflight should return early without calling handler
		require.False(t, handlerCalled)
		require.Equal(t, http.StatusNoContent, rec.Code)

		// Check CORS headers
		require.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
		require.Equal(t, "GET, POST, PUT", rec.Header().Get("Access-Control-Allow-Methods"))
		require.Equal(t, "Content-Type, X-Custom-Header", rec.Header().Get("Access-Control-Allow-Headers"))
		require.Equal(t, "3600", rec.Header().Get("Access-Control-Max-Age"))
	})

	t.Run("credentials mode echoes origin instead of wildcard", func(t *testing.T) {
		t.Parallel()

		mw := middlewares.CORS(
			middlewares.WithAllowCredentials(),
		)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Origin", "http://example.com")
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		handler := mw(func(c internal.Context) error {
			return c.NoContent(http.StatusOK)
		})

		err := handler(ctx)
		require.NoError(t, err)

		// With credentials, must echo origin instead of "*"
		require.Equal(t, "http://example.com", rec.Header().Get("Access-Control-Allow-Origin"))
		require.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))
	})

	t.Run("expose headers", func(t *testing.T) {
		t.Parallel()

		mw := middlewares.CORS(
			middlewares.WithExposeHeaders("X-Custom-Response", "X-Request-Id"),
		)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Origin", "http://example.com")
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		handler := mw(func(c internal.Context) error {
			return c.NoContent(http.StatusOK)
		})

		err := handler(ctx)
		require.NoError(t, err)
		require.Equal(t, "X-Custom-Response, X-Request-Id", rec.Header().Get("Access-Control-Expose-Headers"))
	})

	t.Run("Vary header is always set", func(t *testing.T) {
		t.Parallel()

		mw := middlewares.CORS()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Origin", "http://example.com")
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		handler := mw(func(c internal.Context) error {
			return c.NoContent(http.StatusOK)
		})

		err := handler(ctx)
		require.NoError(t, err)
		require.Contains(t, rec.Header().Values("Vary"), "Origin")
	})

	t.Run("preflight adds additional Vary headers", func(t *testing.T) {
		t.Parallel()

		mw := middlewares.CORS()

		req := httptest.NewRequest(http.MethodOptions, "/", nil)
		req.Header.Set("Origin", "http://example.com")
		req.Header.Set("Access-Control-Request-Method", "POST")
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		handler := mw(func(c internal.Context) error {
			return c.NoContent(http.StatusOK)
		})

		err := handler(ctx)
		require.NoError(t, err)

		varyHeaders := rec.Header().Values("Vary")
		require.Contains(t, varyHeaders, "Origin")
		require.Contains(t, varyHeaders, "Access-Control-Request-Method")
		require.Contains(t, varyHeaders, "Access-Control-Request-Headers")
	})

	t.Run("multiple options combined", func(t *testing.T) {
		t.Parallel()

		mw := middlewares.CORS(
			middlewares.WithAllowOrigins("http://app.example.com"),
			middlewares.WithAllowMethods("GET", "POST"),
			middlewares.WithAllowHeaders("Content-Type", "Authorization"),
			middlewares.WithExposeHeaders("X-Request-Id"),
			middlewares.WithAllowCredentials(),
			middlewares.WithMaxAge(30*time.Minute),
		)

		req := httptest.NewRequest(http.MethodOptions, "/", nil)
		req.Header.Set("Origin", "http://app.example.com")
		req.Header.Set("Access-Control-Request-Method", "POST")
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		handler := mw(func(c internal.Context) error {
			return c.NoContent(http.StatusOK)
		})

		err := handler(ctx)
		require.NoError(t, err)

		require.Equal(t, "http://app.example.com", rec.Header().Get("Access-Control-Allow-Origin"))
		require.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))
		require.Equal(t, "GET, POST", rec.Header().Get("Access-Control-Allow-Methods"))
		require.Equal(t, "Content-Type, Authorization", rec.Header().Get("Access-Control-Allow-Headers"))
		require.Equal(t, "X-Request-Id", rec.Header().Get("Access-Control-Expose-Headers"))
		require.Equal(t, "1800", rec.Header().Get("Access-Control-Max-Age"))
	})

	t.Run("handler is called for actual requests", func(t *testing.T) {
		t.Parallel()

		mw := middlewares.CORS()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Origin", "http://example.com")
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		handlerCalled := false
		handler := mw(func(c internal.Context) error {
			handlerCalled = true
			return c.String(http.StatusOK, "response")
		})

		err := handler(ctx)
		require.NoError(t, err)
		require.True(t, handlerCalled)
		require.Equal(t, "response", rec.Body.String())
	})

	t.Run("specific origins echoes actual origin not wildcard", func(t *testing.T) {
		t.Parallel()

		mw := middlewares.CORS(
			middlewares.WithAllowOrigins("http://app1.example.com", "http://app2.example.com"),
		)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Origin", "http://app1.example.com")
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		handler := mw(func(c internal.Context) error {
			return c.NoContent(http.StatusOK)
		})

		err := handler(ctx)
		require.NoError(t, err)
		// Should echo the specific origin, not "*"
		require.Equal(t, "http://app1.example.com", rec.Header().Get("Access-Control-Allow-Origin"))
	})
}
