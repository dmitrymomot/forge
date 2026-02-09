package middlewares_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge"
	"github.com/dmitrymomot/forge/middlewares"
	"github.com/dmitrymomot/forge/pkg/ratelimit"
)

func newTestLimiter(t *testing.T, limit int64, window time.Duration) (*ratelimit.Limiter, *ratelimit.MemoryCounter) {
	t.Helper()
	counter := ratelimit.NewMemoryCounter(ratelimit.MemoryConfig{CleanupInterval: -1})
	t.Cleanup(func() { counter.Close() })
	limiter, err := ratelimit.New(counter, limit, window)
	require.NoError(t, err)
	return limiter, counter
}

func TestRateLimit_AllowedRequest(t *testing.T) {
	t.Parallel()

	t.Run("sets rate limit headers on allowed request", func(t *testing.T) {
		t.Parallel()

		limiter, _ := newTestLimiter(t, 10, time.Minute)

		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithMiddleware(middlewares.RateLimit(limiter)),
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "10", rec.Header().Get("X-RateLimit-Limit"))
		require.Equal(t, "9", rec.Header().Get("X-RateLimit-Remaining"))
		require.NotEmpty(t, rec.Header().Get("X-RateLimit-Reset"))
		require.Empty(t, rec.Header().Get("Retry-After"))
	})
}

func TestRateLimit_Exceeded(t *testing.T) {
	t.Parallel()

	t.Run("returns 429 with Retry-After when rate limited", func(t *testing.T) {
		t.Parallel()

		limiter, _ := newTestLimiter(t, 2, time.Minute)

		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithErrorHandler(testErrorHandler),
			forge.WithMiddleware(middlewares.RateLimit(limiter)),
			forge.WithHandlers(handler),
		)

		// Exhaust the limit
		for range 2 {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.RemoteAddr = "10.0.0.1:9999"
			rec := httptest.NewRecorder()
			app.Router().ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code)
		}

		// Third request should be rate limited
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "10.0.0.1:9999"
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusTooManyRequests, rec.Code)
		require.NotEmpty(t, rec.Header().Get("Retry-After"))

		retryAfter, err := strconv.Atoi(rec.Header().Get("Retry-After"))
		require.NoError(t, err)
		require.Greater(t, retryAfter, 0)
	})
}

func TestRateLimit_CustomKeyFunc(t *testing.T) {
	t.Parallel()

	t.Run("uses custom key function", func(t *testing.T) {
		t.Parallel()

		limiter, _ := newTestLimiter(t, 1, time.Minute)

		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithErrorHandler(testErrorHandler),
			forge.WithMiddleware(middlewares.RateLimit(limiter,
				middlewares.WithRateLimitKeyFunc(func(r *http.Request) string {
					return r.Header.Get("X-API-Key")
				}),
			)),
			forge.WithHandlers(handler),
		)

		// First request with key "a" - allowed
		req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
		req1.Header.Set("X-API-Key", "key-a")
		rec1 := httptest.NewRecorder()
		app.Router().ServeHTTP(rec1, req1)
		require.Equal(t, http.StatusOK, rec1.Code)

		// First request with key "b" - allowed (different key)
		req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
		req2.Header.Set("X-API-Key", "key-b")
		rec2 := httptest.NewRecorder()
		app.Router().ServeHTTP(rec2, req2)
		require.Equal(t, http.StatusOK, rec2.Code)

		// Second request with key "a" - rate limited
		req3 := httptest.NewRequest(http.MethodGet, "/test", nil)
		req3.Header.Set("X-API-Key", "key-a")
		rec3 := httptest.NewRecorder()
		app.Router().ServeHTTP(rec3, req3)
		require.Equal(t, http.StatusTooManyRequests, rec3.Code)
	})
}

func TestRateLimit_SkipFunc(t *testing.T) {
	t.Parallel()

	t.Run("skip function bypasses rate limiting", func(t *testing.T) {
		t.Parallel()

		limiter, _ := newTestLimiter(t, 1, time.Minute)

		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithErrorHandler(testErrorHandler),
			forge.WithMiddleware(middlewares.RateLimit(limiter,
				middlewares.WithRateLimitSkipFunc(func(c forge.Context) bool {
					return c.Request().URL.Path == "/test" && c.Request().Method == http.MethodGet
				}),
			)),
			forge.WithHandlers(handler),
		)

		// Multiple GET requests should all succeed (skipped)
		for range 5 {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.RemoteAddr = "10.0.0.1:9999"
			rec := httptest.NewRecorder()
			app.Router().ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code)
			require.Empty(t, rec.Header().Get("X-RateLimit-Limit"), "skipped requests should not set rate limit headers")
		}
	})
}

func TestRateLimit_CustomErrorHandler(t *testing.T) {
	t.Parallel()

	t.Run("custom error handler is called on rate limit", func(t *testing.T) {
		t.Parallel()

		limiter, _ := newTestLimiter(t, 1, time.Minute)

		var errorHandlerCalled bool
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithErrorHandler(testErrorHandler),
			forge.WithMiddleware(middlewares.RateLimit(limiter,
				middlewares.WithRateLimitErrorHandler(func(c forge.Context, info ratelimit.Info) error {
					errorHandlerCalled = true
					return c.Error(http.StatusServiceUnavailable, "custom rate limit response")
				}),
			)),
			forge.WithHandlers(handler),
		)

		// Exhaust limit
		req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
		req1.RemoteAddr = "10.0.0.1:9999"
		rec1 := httptest.NewRecorder()
		app.Router().ServeHTTP(rec1, req1)
		require.Equal(t, http.StatusOK, rec1.Code)

		// Second request triggers custom error handler
		req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
		req2.RemoteAddr = "10.0.0.1:9999"
		rec2 := httptest.NewRecorder()
		app.Router().ServeHTTP(rec2, req2)

		require.True(t, errorHandlerCalled)
		require.Equal(t, http.StatusServiceUnavailable, rec2.Code)
	})
}

// failingCounter is a Counter that always returns an error.
type failingCounter struct{}

func (f *failingCounter) Increment(_ context.Context, _ string, _ time.Time, _ time.Duration, _ int64) (int64, error) {
	return 0, errors.New("counter unavailable")
}
func (f *failingCounter) Get(_ context.Context, _ string, _ time.Time) (int64, error) {
	return 0, errors.New("counter unavailable")
}
func (f *failingCounter) Close() error { return nil }

func TestRateLimit_FailOpen(t *testing.T) {
	t.Parallel()

	t.Run("counter error fails open and allows request", func(t *testing.T) {
		t.Parallel()

		limiter, err := ratelimit.New(&failingCounter{}, 10, time.Minute)
		require.NoError(t, err)

		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithMiddleware(middlewares.RateLimit(limiter)),
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "10.0.0.1:9999"
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "ok", rec.Body.String())
	})
}

func TestRateLimit_GetRateLimitInfo(t *testing.T) {
	t.Parallel()

	t.Run("returns info stored in context", func(t *testing.T) {
		t.Parallel()

		limiter, _ := newTestLimiter(t, 10, time.Minute)

		var capturedInfo *ratelimit.Info
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				capturedInfo = middlewares.GetRateLimitInfo(c)
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithMiddleware(middlewares.RateLimit(limiter)),
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "10.0.0.1:9999"
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.NotNil(t, capturedInfo)
		require.Equal(t, int64(10), capturedInfo.Limit)
		require.Equal(t, int64(9), capturedInfo.Remaining)
		require.True(t, capturedInfo.IsAllowed())
	})

	t.Run("returns nil when middleware not applied", func(t *testing.T) {
		t.Parallel()

		var capturedInfo *ratelimit.Info
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				capturedInfo = middlewares.GetRateLimitInfo(c)
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Nil(t, capturedInfo)
	})
}

func TestRateLimit_RemainingDecrementsCorrectly(t *testing.T) {
	t.Parallel()

	t.Run("remaining decrements with each request", func(t *testing.T) {
		t.Parallel()

		limiter, _ := newTestLimiter(t, 5, time.Minute)

		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithMiddleware(middlewares.RateLimit(limiter)),
			forge.WithHandlers(handler),
		)

		for i := range 5 {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.RemoteAddr = "10.0.0.2:9999"
			rec := httptest.NewRecorder()
			app.Router().ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code)
			remaining, err := strconv.ParseInt(rec.Header().Get("X-RateLimit-Remaining"), 10, 64)
			require.NoError(t, err)
			require.Equal(t, int64(4-i), remaining)
		}
	})
}
