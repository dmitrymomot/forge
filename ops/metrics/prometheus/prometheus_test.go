package prometheus_test

import (
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/metrics"
	"github.com/dmitrymomot/forge/ops/metrics/prometheus"
)

func scrape(t *testing.T, rec *prometheus.Recorder) string {
	t.Helper()
	rr := httptest.NewRecorder()
	rec.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, rr.Code)
	body, err := io.ReadAll(rr.Result().Body)
	require.NoError(t, err)
	return string(body)
}

func TestRecorderScrape(t *testing.T) {
	rec := prometheus.New()

	c := rec.Counter("signups_total", "Completed signups.", "plan")
	c.Inc("pro")
	c.Add(2, "free")
	g := rec.Gauge("queue_depth", "Depth.")
	g.Set(10)
	g.Add(-3)
	h := rec.Histogram("job_seconds", "Job time.", []float64{1, 5}, "kind")
	h.Observe(0.5, "email")
	h.Observe(7, "email")

	body := scrape(t, rec)
	assert.Contains(t, body, `signups_total{plan="pro"} 1`)
	assert.Contains(t, body, `signups_total{plan="free"} 2`)
	assert.Contains(t, body, "queue_depth 7")
	assert.Contains(t, body, `job_seconds_bucket{kind="email",le="1"} 1`)
	assert.Contains(t, body, `job_seconds_bucket{kind="email",le="+Inf"} 2`)
	assert.Contains(t, body, `job_seconds_sum{kind="email"} 7.5`)
	assert.Contains(t, body, "go_goroutines", "default registry includes the Go collector")
}

func TestDefaultHistogramBuckets(t *testing.T) {
	rec := prometheus.New()
	rec.Histogram("d_seconds", "d.", nil).Observe(0.003)

	assert.Contains(t, scrape(t, rec), `d_seconds_bucket{le="0.005"} 1`)
}

func TestWithRegistryAndNamespace(t *testing.T) {
	reg := prom.NewRegistry()
	rec := prometheus.New(prometheus.WithRegistry(reg), prometheus.WithNamespace("checkout"))
	rec.Counter("orders_total", "Orders.").Inc()

	body := scrape(t, rec)
	assert.Contains(t, body, "checkout_orders_total 1")
	assert.NotContains(t, body, "go_goroutines", "caller-owned registry gets no auto collectors")
}

func TestIdempotentRegistration(t *testing.T) {
	rec := prometheus.New()
	a := rec.Counter("hits_total", "Hits.", "path")
	b := rec.Counter("hits_total", "Hits.", "path")
	a.Inc("/")
	b.Inc("/")

	assert.Contains(t, scrape(t, rec), `hits_total{path="/"} 2`)
}

func TestSchemaMismatchPanics(t *testing.T) {
	rec := prometheus.New()
	rec.Counter("thing_total", "t.", "a")
	assert.Panics(t, func() { rec.Gauge("thing_total", "t.") }, "kind mismatch")
	assert.Panics(t, func() { rec.Counter("thing_total", "t.", "b") }, "label names mismatch")

	rec.Histogram("h_seconds", "h.", []float64{1, 2})
	assert.Panics(t, func() { rec.Histogram("h_seconds", "h.", []float64{1, 3}) }, "buckets mismatch")
}

// Bucket handling must match the expvar recorder: NaN panics, and a trailing
// +Inf is normalized away before the schema-mismatch comparison.
func TestBucketNormalizationParity(t *testing.T) {
	rec := prometheus.New()
	assert.Panics(t, func() { rec.Histogram("nan_seconds", "h.", []float64{math.NaN(), 1}) })

	rec.Histogram("inf_seconds", "h.", []float64{1, math.Inf(1)})
	assert.NotPanics(t, func() { rec.Histogram("inf_seconds", "h.", []float64{1}) })
}

func TestClientContractPanics(t *testing.T) {
	rec := prometheus.New()
	assert.Panics(t, func() { rec.Counter("bad-name", "b.") }, "invalid metric name")
	c := rec.Counter("ok_total", "ok.", "method")
	assert.Panics(t, func() { c.Inc() }, "label count mismatch")
	assert.Panics(t, func() { c.Add(-1, "GET") }, "negative counter delta")
}

func TestImplementsRecorderSeam(t *testing.T) {
	var rec metrics.Recorder = prometheus.New()
	h := metrics.NewMiddleware(rec)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

	promRec, ok := rec.(*prometheus.Recorder)
	require.True(t, ok)
	body := scrape(t, promRec)
	assert.Contains(t, body, `http_requests_total{method="GET",path="unmatched",status="200"} 1`)
	assert.True(t, strings.Contains(body, "http_requests_in_flight 0"))
}
