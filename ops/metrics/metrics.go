package metrics

import (
	"context"
	"math"
	"slices"
	"strconv"
	"time"
)

// DefaultBuckets are the default histogram bucket upper bounds, tuned for
// request durations in seconds. They mirror the prometheus client defaults so
// the expvar and prometheus recorders aggregate identically.
var DefaultBuckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}

// Recorder creates named instruments. Implementations are safe for concurrent
// use and idempotent: requesting an instrument under an existing name returns
// the existing instrument when the kind, label names, and buckets match, and
// panics otherwise (programmer error). Metric names must match
// [a-zA-Z_:][a-zA-Z0-9_:]* and label names [a-zA-Z_][a-zA-Z0-9_]*; invalid or
// duplicate names panic at creation, mirroring prometheus registration
// semantics so an adapter swap never changes behavior.
type Recorder interface {
	// Counter returns a monotonically increasing counter.
	Counter(name, help string, labelNames ...string) Counter
	// Gauge returns a value that can go up and down.
	Gauge(name, help string, labelNames ...string) Gauge
	// Histogram returns a distribution observer with the given upper-inclusive
	// bucket bounds. Nil or empty buckets mean DefaultBuckets; bounds must be
	// strictly increasing (a trailing +Inf is stripped — the implicit overflow
	// bucket always exists).
	Histogram(name, help string, buckets []float64, labelNames ...string) Histogram
	// GaugeFunc registers a gauge whose value fn produces at collection time.
	// It is the pull counterpart of Gauge: use it when a live source already
	// owns the number (pool depth, queue length, a row count) so nothing has to
	// push updates on a timer. A nil fn panics.
	GaugeFunc(name, help string, fn GaugeFunc)
}

// GaugeFunc reads one value at collection time — a /debug/vars render or a
// prometheus scrape. The context is bounded by the recorder's collect timeout
// (WithCollectTimeout, default DefaultCollectTimeout), so a query behind it can
// never stall the endpoint. Returning an error (or a non-finite value) drops the
// gauge from that collection and counts one failure under
// CollectFailuresMetric; because the counter may already have been rendered,
// the failure surfaces on the following collection.
//
// fn runs on the collecting goroutine and may run concurrently with itself when
// two scrapers overlap, so it must be safe for concurrent use.
type GaugeFunc func(ctx context.Context) (float64, error)

const (
	// DefaultCollectTimeout bounds a GaugeFunc call when WithCollectTimeout is unset.
	DefaultCollectTimeout = 2 * time.Second

	// CollectFailuresMetric counts GaugeFunc calls that failed, labeled by gauge name.
	CollectFailuresMetric = "gauge_collect_failures_total"

	collectFailuresHelp  = "GaugeFunc reads that failed or produced a non-finite value."
	collectFailuresLabel = "gauge"
)

// CollectFailuresCounter returns the counter every Recorder increments when a
// GaugeFunc read fails, so the metric's name, help, and label are defined once and
// read identically on every backend. Call it before taking any registry lock: it
// creates an ordinary instrument through rec.
func CollectFailuresCounter(rec Recorder) Counter {
	return rec.Counter(CollectFailuresMetric, collectFailuresHelp, collectFailuresLabel)
}

// ResolveCollectTimeout returns d, or DefaultCollectTimeout when d is not positive.
// Every Recorder implementation runs its configured timeout through it so an
// adapter swap never changes how long a stalled GaugeFunc blocks collection.
func ResolveCollectTimeout(d time.Duration) time.Duration {
	if d <= 0 {
		return DefaultCollectTimeout
	}
	return d
}

// CollectGauge calls fn under a timeout-bounded context and reports whether the
// result is usable. A failed or non-finite read counts one failure against
// failures and returns ok=false, so both recorders drop the same reads and count
// them identically.
func CollectGauge(fn GaugeFunc, name string, timeout time.Duration, failures Counter) (float64, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	v, err := fn(ctx)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		failures.Inc(name)
		return 0, false
	}
	return v, true
}

// Counter is a monotonically increasing value. labelValues must match the
// instrument's label names in count and order; a mismatch panics.
type Counter interface {
	// Add increments the counter by delta; a negative delta panics.
	Add(delta float64, labelValues ...string)
	// Inc is Add(1).
	Inc(labelValues ...string)
}

// Gauge is a value that can go up and down. labelValues must match the
// instrument's label names in count and order; a mismatch panics.
type Gauge interface {
	Set(value float64, labelValues ...string)
	Add(delta float64, labelValues ...string)
}

// Histogram observes a distribution of values. labelValues must match the
// instrument's label names in count and order; a mismatch panics.
type Histogram interface {
	Observe(value float64, labelValues ...string)
}

// NormalizeBuckets validates histogram bounds and returns a defensive copy;
// nil or empty means DefaultBuckets. Bounds must be strictly increasing with
// no NaN or the call panics; a trailing +Inf is stripped (the implicit
// overflow bucket always exists). Every Recorder implementation runs its
// buckets through this before storing or comparing them, so bucket semantics
// and schema-mismatch checks are identical across backends.
func NormalizeBuckets(buckets []float64) []float64 {
	if len(buckets) == 0 {
		buckets = DefaultBuckets
	}
	var prev float64
	for i, b := range buckets {
		if math.IsNaN(b) || (i > 0 && b <= prev) {
			panic("metrics: histogram buckets must be strictly increasing")
		}
		prev = b
	}
	if n := len(buckets); n > 0 && math.IsInf(prev, +1) {
		buckets = buckets[:n-1]
	}
	return slices.Clone(buckets)
}

// MustValidNames panics unless name matches [a-zA-Z_:][a-zA-Z0-9_:]* and every
// label name matches [a-zA-Z_][a-zA-Z0-9_]* with no duplicates. Every Recorder
// implementation calls it at instrument creation so naming is enforced
// uniformly regardless of what the backend itself would accept (newer
// prometheus clients allow UTF-8 names; forge does not).
func MustValidNames(name string, labelNames ...string) {
	if !validMetricName(name) {
		panic("metrics: invalid metric name " + strconv.Quote(name))
	}
	for i, ln := range labelNames {
		if !validLabelName(ln) {
			panic("metrics: invalid label name " + strconv.Quote(ln))
		}
		if slices.Contains(labelNames[:i], ln) {
			panic("metrics: duplicate label name " + strconv.Quote(ln))
		}
	}
}

func validMetricName(s string) bool {
	for i, r := range s {
		if isNameRune(r, i) || r == ':' {
			continue
		}
		return false
	}
	return s != ""
}

func validLabelName(s string) bool {
	for i, r := range s {
		if !isNameRune(r, i) {
			return false
		}
	}
	return s != ""
}

func isNameRune(r rune, i int) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
		return true
	case r >= '0' && r <= '9':
		return i > 0
	}
	return false
}
