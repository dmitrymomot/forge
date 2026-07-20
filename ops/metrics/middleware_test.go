package metrics_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/metrics"
	"github.com/dmitrymomot/forge/web/middleware"
)

func requestCounts(t *testing.T, snap func() map[string]any) map[string]any {
	t.Helper()
	m, ok := snap()["http_requests_total"].(map[string]any)
	require.True(t, ok)
	return m
}

func TestMiddlewareRecordsPatternPathAndStatus(t *testing.T) {
	rec, snap := newRecorder(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	h := middleware.Wrap(mux, metrics.NewMiddleware(rec))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users/42", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users/7", nil))

	counts := requestCounts(t, snap)
	assert.InDelta(t, 2, counts[`method="GET",path="/users/{id}",status="201"`], 1e-9)

	hist, ok := snap()["http_request_duration_seconds"].(map[string]any)
	require.True(t, ok)
	count, _, _ := histSnapshot(t, hist[`method="GET",path="/users/{id}",status="201"`])
	assert.InDelta(t, 2, count, 1e-9)
}

func TestMiddlewareUnmatchedPathAndImplicitStatus(t *testing.T) {
	rec, snap := newRecorder(t)
	h := metrics.NewMiddleware(rec)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/whatever", nil))

	counts := requestCounts(t, snap)
	assert.InDelta(t, 1, counts[`method="GET",path="unmatched",status="200"`], 1e-9)
}

func TestMiddlewareSkip(t *testing.T) {
	rec, snap := newRecorder(t)
	h := metrics.NewMiddleware(rec, metrics.WithSkip(func(r *http.Request) bool {
		return r.URL.Path == "/livez"
	}))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/livez", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/real", nil))

	assert.Len(t, requestCounts(t, snap), 1)
}

func TestMiddlewareMethodNormalization(t *testing.T) {
	rec, snap := newRecorder(t)
	h := metrics.NewMiddleware(rec)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("PROPFIND", "/x", nil))

	counts := requestCounts(t, snap)
	assert.InDelta(t, 1, counts[`method="OTHER",path="unmatched",status="200"`], 1e-9)
}

func TestMiddlewareInFlight(t *testing.T) {
	rec, snap := newRecorder(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	h := metrics.NewMiddleware(rec)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(entered)
		<-release
	}))

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/slow", nil))
	}()
	<-entered
	assert.InDelta(t, 1, snap()["http_requests_in_flight"], 1e-9)
	close(release)
	<-done
	assert.InDelta(t, 0, snap()["http_requests_in_flight"], 1e-9)
}

type ctxKey struct{}

func TestMiddlewareContextLabel(t *testing.T) {
	rec, snap := newRecorder(t)
	mw := metrics.NewMiddleware(rec, metrics.WithContextLabel("tenant", func(ctx context.Context) string {
		s, _ := ctx.Value(ctxKey{}).(string)
		return s
	}))
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	r := httptest.NewRequest(http.MethodGet, "/a", nil)
	h.ServeHTTP(httptest.NewRecorder(), r.WithContext(context.WithValue(r.Context(), ctxKey{}, "acme")))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/a", nil))

	counts := requestCounts(t, snap)
	assert.InDelta(t, 1, counts[`method="GET",path="unmatched",status="200",tenant="acme"`], 1e-9)
	assert.InDelta(t, 1, counts[`method="GET",path="unmatched",status="200",tenant="unknown"`], 1e-9, "missing scope fails closed")
}

func TestMiddlewareCustomBucketsAndPathFunc(t *testing.T) {
	rec, snap := newRecorder(t)
	mw := metrics.NewMiddleware(rec,
		metrics.WithDurationBuckets([]float64{1, 2}),
		metrics.WithPathFunc(func(*http.Request) string { return "/custom" }),
	)
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

	hist, ok := snap()["http_request_duration_seconds"].(map[string]any)
	require.True(t, ok)
	_, _, buckets := histSnapshot(t, hist[`method="GET",path="/custom",status="200"`])
	assert.Len(t, buckets, 3)
	assert.Contains(t, buckets, "1")
	assert.Contains(t, buckets, "2")
}

func TestMiddlewareConfigPanics(t *testing.T) {
	assert.Panics(t, func() { metrics.NewMiddleware(nil) })
	assert.Panics(t, func() { metrics.WithContextLabel("", func(context.Context) string { return "" }) })
	assert.Panics(t, func() { metrics.WithContextLabel("tenant", nil) })
}

func TestNoopRecorder(t *testing.T) {
	rec := metrics.NewNoop()
	require.NotNil(t, rec)
	assert.NotPanics(t, func() {
		rec.Counter("c_total", "c.", "l").Add(-5, "ignores", "everything")
		rec.Gauge("g", "g.").Set(1)
		rec.Histogram("h", "h.", []float64{2, 1}).Observe(3)
		h := metrics.NewMiddleware(rec)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	})
}
