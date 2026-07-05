package ratelimit_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

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
