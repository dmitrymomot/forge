package loadshed_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/resilience/loadshed"
)

func TestMiddleware_ShedReturns503(t *testing.T) {
	s := loadshed.New(
		loadshed.WithCriteria(loadshed.Concurrency(1)),
		loadshed.WithThreshold(0.0),
		loadshed.WithFloor(0.0),
		loadshed.WithRand(func() float64 { return 0.0 }),
	)
	// Occupy the single slot deterministically. A Shedder is a probabilistic
	// admission controller sampling pressure, not a hard semaphore, so two
	// concurrent Acquires racing on inflight=0 can both admit; pre-filling the
	// slot in-line makes pressure 1.0 for the request under test with no race.
	tk, ok := s.Acquire(context.Background())
	assert.True(t, ok)
	defer tk.Release()

	h := s.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestMiddleware_SkipBypasses(t *testing.T) {
	s := loadshed.New(
		loadshed.WithCriteria(loadshed.Concurrency(1)),
		loadshed.WithThreshold(0.0), loadshed.WithFloor(0.0),
		loadshed.WithRand(func() float64 { return 0.0 }),
	)
	h := s.Middleware(loadshed.WithSkip(func(*http.Request) bool { return true }))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	assert.Equal(t, 200, rec.Code)
}
