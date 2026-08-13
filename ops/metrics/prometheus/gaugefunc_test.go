package prometheus_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/ops/metrics"
	"github.com/dmitrymomot/forge/ops/metrics/prometheus"
)

var errSourceDown = errors.New("source down")

func TestGaugeFuncScrapesTheLiveValue(t *testing.T) {
	rec := prometheus.New()
	calls := 0
	rec.GaugeFunc("queue_depth", "Jobs waiting.", func(context.Context) (float64, error) {
		calls++
		return float64(calls * 10), nil
	})

	assert.Contains(t, scrape(t, rec), "queue_depth 10")
	assert.Contains(t, scrape(t, rec), "queue_depth 20")
}

func TestGaugeFuncCarriesHelpAndNamespace(t *testing.T) {
	rec := prometheus.New(prometheus.WithNamespace("app"))
	rec.GaugeFunc("queue_depth", "Jobs waiting.", func(context.Context) (float64, error) {
		return 3, nil
	})

	out := scrape(t, rec)
	assert.Contains(t, out, "# HELP app_queue_depth Jobs waiting.")
	assert.Contains(t, out, "# TYPE app_queue_depth gauge")
	assert.Contains(t, out, "app_queue_depth 3")
}

// TestGaugeFuncFailureEmitsNothing pins the stale-over-wrong choice: a read that
// failed publishes no sample rather than a zero that would look like real data.
func TestGaugeFuncFailureEmitsNothing(t *testing.T) {
	rec := prometheus.New()
	rec.GaugeFunc("rows", "Rows.", func(context.Context) (float64, error) {
		return 0, errSourceDown
	})

	assert.NotContains(t, scrape(t, rec), "\nrows ")
	assert.Contains(t, scrape(t, rec), metrics.CollectFailuresMetric+`{gauge="rows"}`)
}

func TestGaugeFuncNonFiniteEmitsNothing(t *testing.T) {
	rec := prometheus.New()
	rec.GaugeFunc("rows", "Rows.", func(context.Context) (float64, error) {
		return math.NaN(), nil
	})

	assert.NotContains(t, scrape(t, rec), "\nrows ")
	assert.Contains(t, scrape(t, rec), metrics.CollectFailuresMetric)
}

func TestGaugeFuncStalledReadIsBounded(t *testing.T) {
	rec := prometheus.New(prometheus.WithCollectTimeout(20 * time.Millisecond))
	rec.GaugeFunc("rows", "Rows.", func(ctx context.Context) (float64, error) {
		<-ctx.Done()
		return 0, ctx.Err()
	})

	start := time.Now()
	out := scrape(t, rec)
	assert.Less(t, time.Since(start), time.Second)
	assert.NotContains(t, out, "\nrows ")
}

func TestGaugeFuncNilPanics(t *testing.T) {
	rec := prometheus.New()
	assert.Panics(t, func() { rec.GaugeFunc("rows", "Rows.", nil) })
}

func TestGaugeFuncKindMismatchPanics(t *testing.T) {
	rec := prometheus.New()
	rec.Gauge("rows", "Rows.")
	assert.Panics(t, func() {
		rec.GaugeFunc("rows", "Rows.", func(context.Context) (float64, error) { return 1, nil })
	})
}

func TestGaugeFuncReregistrationKeepsTheFirstFunc(t *testing.T) {
	rec := prometheus.New()
	rec.GaugeFunc("rows", "Rows.", func(context.Context) (float64, error) { return 1, nil })
	rec.GaugeFunc("rows", "Rows.", func(context.Context) (float64, error) { return 2, nil })

	assert.Contains(t, scrape(t, rec), "rows 1")
}
