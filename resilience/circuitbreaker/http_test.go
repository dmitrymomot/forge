package circuitbreaker_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/resilience/circuitbreaker"
)

// runMW serves one request through mw(h) and returns the recorder.
func runMW(mw func(http.Handler) http.Handler, h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mw(h).ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

// status returns a handler that writes the given status code.
func status(code int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) })
}

func TestMiddlewarePassesThroughWhenClosed(t *testing.T) {
	rec := runMW(circuitbreaker.Middleware(circuitbreaker.New()), status(200), "GET", "/")
	assert.Equal(t, 200, rec.Code)
}

func TestMiddlewareTripsAndReturns503WithRetryAfter(t *testing.T) {
	b := circuitbreaker.New(circuitbreaker.WithFailureThreshold(1), circuitbreaker.WithOpenTimeout(30*time.Second))
	mw := circuitbreaker.Middleware(b)

	assert.Equal(t, 500, runMW(mw, status(500), "GET", "/").Code) // downstream 500 reaches client
	rec := runMW(mw, status(500), "GET", "/")                     // breaker now open
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("Retry-After"))
}

func TestMiddlewareCustomFailurePredicate(t *testing.T) {
	b := circuitbreaker.New(circuitbreaker.WithFailureThreshold(1))
	mw := circuitbreaker.Middleware(b, circuitbreaker.WithFailurePredicate(func(s int) bool { return s == 429 }))

	_ = runMW(mw, status(429), "GET", "/") // 429 counts as failure -> opens
	assert.Equal(t, http.StatusServiceUnavailable, runMW(mw, status(429), "GET", "/").Code)
}

func TestMiddlewareCustomOpenResponder(t *testing.T) {
	b := circuitbreaker.New(circuitbreaker.WithFailureThreshold(1))
	mw := circuitbreaker.Middleware(b, circuitbreaker.WithOpenResponder(
		func(w http.ResponseWriter, _ *http.Request, _ time.Duration) {
			w.WriteHeader(http.StatusTooManyRequests)
		}))

	_ = runMW(mw, status(500), "GET", "/")
	assert.Equal(t, http.StatusTooManyRequests, runMW(mw, status(500), "GET", "/").Code)
}

func TestGroupKeyStaticKey(t *testing.T) {
	g := circuitbreaker.NewGroup(circuitbreaker.WithBreakerOptions(circuitbreaker.WithFailureThreshold(1)))
	mw := circuitbreaker.GroupKey(g, "checkout")

	_ = runMW(mw, status(500), "GET", "/")
	assert.Equal(t, http.StatusServiceUnavailable, runMW(mw, status(500), "GET", "/").Code)
	assert.Equal(t, circuitbreaker.StateOpen, g.State("checkout"))
}

func TestGroupKeyEmptyBypasses(t *testing.T) {
	g := circuitbreaker.NewGroup()
	assert.Equal(t, 204, runMW(circuitbreaker.GroupKey(g, ""), status(204), "GET", "/").Code)
	assert.Equal(t, 0, g.Len())
}

func TestGroupMiddlewareKeyByHostIndependent(t *testing.T) {
	g := circuitbreaker.NewGroup(circuitbreaker.WithBreakerOptions(circuitbreaker.WithFailureThreshold(1)))
	mw := circuitbreaker.GroupMiddleware(g, circuitbreaker.KeyByHost)

	trip := func(host string) int {
		rec := httptest.NewRecorder()
		mw(status(500)).ServeHTTP(rec, httptest.NewRequest("GET", "http://"+host+"/", nil))
		return rec.Code
	}
	_ = trip("a.example")
	_ = trip("a.example")                   // a.example now open
	assert.Equal(t, 500, trip("b.example")) // b.example independent -> its 500 reaches client
}
