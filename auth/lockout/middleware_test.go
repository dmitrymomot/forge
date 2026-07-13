package lockout_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/lockout"
)

func mwRequest(t *testing.T, mw func(http.Handler) http.Handler, next http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	mw(next).ServeHTTP(rec, req)
	return rec
}

func keyU(*http.Request) string { return "u@example.com" }

func TestMiddlewarePassesUnlocked(t *testing.T) {
	t.Parallel()
	lk := newLocker(t)
	var gotKey string
	var gotOK bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotOK = lockout.KeyFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	rec := mwRequest(t, lk.Middleware(keyU), next)
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, gotOK)
	require.Equal(t, "u@example.com", gotKey)
}

func TestMiddlewareBlocksLocked(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(1))
	ctx := context.Background()
	_, err := lk.Fail(ctx, "u@example.com")
	require.NoError(t, err)
	_, err = lk.Fail(ctx, "u@example.com") // locked for 1m
	require.NoError(t, err)

	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next must not run while locked")
	})
	rec := mwRequest(t, lk.Middleware(keyU), next)
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.NotEmpty(t, rec.Header().Get("Retry-After"))
}

func TestMiddlewareEmptyKeySkips(t *testing.T) {
	t.Parallel()
	lk := newLocker(t)
	var ok bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, ok = lockout.KeyFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	rec := mwRequest(t, lk.Middleware(func(*http.Request) string { return "" }), next)
	require.Equal(t, http.StatusOK, rec.Code)
	require.False(t, ok, "no key must be stashed when the check is skipped")
}

func TestMiddlewareFailsClosedOnStoreError(t *testing.T) {
	t.Parallel()
	lk, err := lockout.New(failingCounters{}, failingLocks{})
	require.NoError(t, err)
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next must not run on store error (fail closed)")
	})

	rec := mwRequest(t, lk.Middleware(keyU), next)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestMiddlewareFailOpenOption(t *testing.T) {
	t.Parallel()
	lk, err := lockout.New(failingCounters{}, failingLocks{})
	require.NoError(t, err)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := mwRequest(t, lk.Middleware(keyU, lockout.WithFailOpen()), next)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestMiddlewareCustomResponder(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(1))
	ctx := context.Background()
	_, err := lk.Fail(ctx, "u@example.com")
	require.NoError(t, err)
	_, err = lk.Fail(ctx, "u@example.com")
	require.NoError(t, err)

	responder := func(w http.ResponseWriter, _ *http.Request, res lockout.Result) {
		require.True(t, res.Locked)
		http.Error(w, "custom", http.StatusLocked)
	}
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	rec := mwRequest(t, lk.Middleware(keyU, lockout.WithResponder(responder)), next)
	require.Equal(t, http.StatusLocked, rec.Code)
	require.NotEmpty(t, rec.Header().Get("Retry-After"), "Retry-After set before the responder runs")
}
