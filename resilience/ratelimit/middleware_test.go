package ratelimit_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/ratelimit"
)

func TestMiddleware_HeadersAnd429(t *testing.T) {
	l := ratelimit.New(ratelimit.NewMemoryStore(), ratelimit.WithLimit(1, time.Minute))
	key := func(*http.Request) string { return "fixed" }
	h := l.Middleware(key)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusOK, rec1.Code)
	assert.Equal(t, "1", rec1.Header().Get("RateLimit-Limit"))

	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusTooManyRequests, rec2.Code)
	assert.NotEmpty(t, rec2.Header().Get("Retry-After"))
}

// TestMiddleware_ResetIsDeltaSecondsOnAllow guards against RateLimit-Reset
// being hardcoded to "0" on the allowed path: it must always report
// seconds-until-window-reset, per the IETF draft header semantics mandated by
// the design spec.
func TestMiddleware_ResetIsDeltaSecondsOnAllow(t *testing.T) {
	mk := clock.NewMock(time.Unix(0, 0))
	l := ratelimit.New(
		ratelimit.NewMemoryStore(ratelimit.WithMemoryClock(mk)),
		ratelimit.WithLimit(5, time.Minute),
		ratelimit.WithClock(mk),
	)
	key := func(*http.Request) string { return "fixed" }
	h := l.Middleware(key)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "60", rec.Header().Get("RateLimit-Reset"))
	assert.NotEqual(t, "0", rec.Header().Get("RateLimit-Reset"))
}

// erroringStore is a black-box Store double whose Incr always errors, used to
// exercise the middleware's fail-open path.
type erroringStore struct{}

func (erroringStore) Incr(context.Context, string, int64, time.Duration) (int64, error) {
	return 0, errors.New("store unavailable")
}

func (erroringStore) Get(context.Context, string) (int64, error) { return 0, nil }

func (erroringStore) Reset(context.Context, string) error { return nil }

func (erroringStore) Close() error { return nil }

// TestMiddleware_FailsOpenOnStoreError asserts that a Store error serves the
// request via next (no 5xx, no limiting) and writes no RateLimit-* headers.
func TestMiddleware_FailsOpenOnStoreError(t *testing.T) {
	l := ratelimit.New(erroringStore{}, ratelimit.WithLimit(1, time.Minute))
	key := func(*http.Request) string { return "fixed" }
	h := l.Middleware(key)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get("RateLimit-Limit"))
	assert.Empty(t, rec.Header().Get("RateLimit-Remaining"))
	assert.Empty(t, rec.Header().Get("RateLimit-Reset"))
}

// TestMiddleware_WithResponderOverride asserts a custom WithResponder replaces
// the default 429 plain-text body on a denied request.
func TestMiddleware_WithResponderOverride(t *testing.T) {
	const customBody = "custom rate limit response"
	responder := func(w http.ResponseWriter, _ *http.Request, _ ratelimit.Result) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(customBody))
	}

	l := ratelimit.New(ratelimit.NewMemoryStore(), ratelimit.WithLimit(1, time.Minute))
	key := func(*http.Request) string { return "fixed" }
	h := l.Middleware(key, ratelimit.WithResponder(responder))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusOK, rec1.Code)

	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec2.Code)
	assert.Equal(t, customBody, rec2.Body.String())
}
