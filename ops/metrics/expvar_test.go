package metrics_test

import (
	"encoding/json"
	"expvar"
	"math"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/metrics"
)

// newRecorder publishes under a unique expvar name (expvar is process-global)
// and returns the recorder plus a snapshot func decoding the published JSON.
func newRecorder(t *testing.T, opts ...metrics.Option) (metrics.Recorder, func() map[string]any) {
	t.Helper()
	name := "test_" + t.Name()
	rec := metrics.New(append([]metrics.Option{metrics.WithName(name)}, opts...)...)
	return rec, func() map[string]any {
		v := expvar.Get(name)
		require.NotNil(t, v)
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(v.String()), &m))
		return m
	}
}

func TestCounterUnlabeled(t *testing.T) {
	rec, snap := newRecorder(t)
	c := rec.Counter("jobs_total", "Jobs.")
	c.Inc()
	c.Add(2.5)

	assert.InDelta(t, 3.5, snap()["jobs_total"], 1e-9)
}

func TestCounterLabeled(t *testing.T) {
	rec, snap := newRecorder(t)
	c := rec.Counter("requests_total", "Requests.", "method", "status")
	c.Inc("GET", "200")
	c.Inc("GET", "200")
	c.Inc("POST", "500")

	m, ok := snap()["requests_total"].(map[string]any)
	require.True(t, ok)
	assert.InDelta(t, 2, m[`method="GET",status="200"`], 1e-9)
	assert.InDelta(t, 1, m[`method="POST",status="500"`], 1e-9)
}

func TestCounterNegativeDeltaPanics(t *testing.T) {
	rec, _ := newRecorder(t)
	c := rec.Counter("c_total", "c")
	assert.Panics(t, func() { c.Add(-1) })
}

func TestLabelCountMismatchPanics(t *testing.T) {
	rec, _ := newRecorder(t)
	c := rec.Counter("labeled_total", "c", "method")
	assert.Panics(t, func() { c.Inc() })
	assert.Panics(t, func() { c.Inc("GET", "extra") })

	u := rec.Counter("unlabeled_total", "c")
	assert.Panics(t, func() { u.Inc("GET") })

	g := rec.Gauge("g", "g", "queue")
	assert.Panics(t, func() { g.Set(1) })
	h := rec.Histogram("h", "h", nil, "kind")
	assert.Panics(t, func() { h.Observe(1) })
}

func TestGaugeSetAdd(t *testing.T) {
	rec, snap := newRecorder(t)
	g := rec.Gauge("depth", "Depth.")
	g.Set(10)
	g.Add(-3)
	assert.InDelta(t, 7, snap()["depth"], 1e-9)

	lg := rec.Gauge("queue_depth", "Depth per queue.", "queue")
	lg.Add(2, "email")
	lg.Set(5, "email")
	lg.Set(1, "sms")
	m, ok := snap()["queue_depth"].(map[string]any)
	require.True(t, ok)
	assert.InDelta(t, 5, m[`queue="email"`], 1e-9)
	assert.InDelta(t, 1, m[`queue="sms"`], 1e-9)
}

func histSnapshot(t *testing.T, raw any) (count float64, sum float64, buckets map[string]any) {
	t.Helper()
	m, ok := raw.(map[string]any)
	require.True(t, ok)
	count, _ = m["count"].(float64)
	sum, _ = m["sum"].(float64)
	buckets, _ = m["buckets"].(map[string]any)
	return count, sum, buckets
}

func TestHistogramObserve(t *testing.T) {
	rec, snap := newRecorder(t)
	h := rec.Histogram("latency_seconds", "Latency.", []float64{0.1, 1, 10})
	h.Observe(0.05)
	h.Observe(0.1) // upper-inclusive: lands in the 0.1 bucket
	h.Observe(5)
	h.Observe(100) // overflow → +Inf only

	count, sum, buckets := histSnapshot(t, snap()["latency_seconds"])
	assert.InDelta(t, 4, count, 1e-9)
	assert.InDelta(t, 105.15, sum, 1e-9)
	assert.InDelta(t, 2, buckets["0.1"], 1e-9) // cumulative
	assert.InDelta(t, 2, buckets["1"], 1e-9)
	assert.InDelta(t, 3, buckets["10"], 1e-9)
	assert.InDelta(t, 4, buckets["+Inf"], 1e-9)
}

func TestHistogramLabeled(t *testing.T) {
	rec, snap := newRecorder(t)
	h := rec.Histogram("job_seconds", "Job time.", []float64{1}, "kind")
	h.Observe(0.5, "email")
	h.Observe(2, "email")
	h.Observe(0.1, "export")

	m, ok := snap()["job_seconds"].(map[string]any)
	require.True(t, ok)
	count, sum, buckets := histSnapshot(t, m[`kind="email"`])
	assert.InDelta(t, 2, count, 1e-9)
	assert.InDelta(t, 2.5, sum, 1e-9)
	assert.InDelta(t, 1, buckets["1"], 1e-9)
	assert.InDelta(t, 2, buckets["+Inf"], 1e-9)
	count, _, _ = histSnapshot(t, m[`kind="export"`])
	assert.InDelta(t, 1, count, 1e-9)
}

func TestHistogramDefaultBuckets(t *testing.T) {
	rec, snap := newRecorder(t)
	rec.Histogram("d_seconds", "d.", nil).Observe(0.003)

	_, _, buckets := histSnapshot(t, snap()["d_seconds"])
	assert.Len(t, buckets, len(metrics.DefaultBuckets)+1)
	assert.InDelta(t, 1, buckets["0.005"], 1e-9)
}

func TestHistogramTrailingInfStripped(t *testing.T) {
	rec, snap := newRecorder(t)
	rec.Histogram("inf_seconds", "d.", []float64{1, math.Inf(1)}).Observe(0.5)

	_, _, buckets := histSnapshot(t, snap()["inf_seconds"])
	assert.Len(t, buckets, 2) // "1" and the implicit "+Inf", not two Infs
}

func TestHistogramInvalidBucketsPanic(t *testing.T) {
	rec, _ := newRecorder(t)
	assert.Panics(t, func() { rec.Histogram("bad1", "b", []float64{1, 1}) })
	assert.Panics(t, func() { rec.Histogram("bad2", "b", []float64{2, 1}) })
}

func TestIdempotentRegistration(t *testing.T) {
	rec, snap := newRecorder(t)
	a := rec.Counter("hits_total", "Hits.", "path")
	b := rec.Counter("hits_total", "Hits.", "path")
	a.Inc("/")
	b.Inc("/")

	m, ok := snap()["hits_total"].(map[string]any)
	require.True(t, ok)
	assert.InDelta(t, 2, m[`path="/"`], 1e-9)
}

func TestSchemaMismatchPanics(t *testing.T) {
	rec, _ := newRecorder(t)
	rec.Counter("thing_total", "t.", "a")
	assert.Panics(t, func() { rec.Gauge("thing_total", "t.") }, "kind mismatch")
	assert.Panics(t, func() { rec.Counter("thing_total", "t.", "b") }, "label names mismatch")

	rec.Histogram("h_seconds", "h.", []float64{1, 2})
	assert.Panics(t, func() { rec.Histogram("h_seconds", "h.", []float64{1, 3}) }, "buckets mismatch")
}

func TestInvalidNamesPanic(t *testing.T) {
	rec, _ := newRecorder(t)
	assert.Panics(t, func() { rec.Counter("", "e.") })
	assert.Panics(t, func() { rec.Counter("bad-name", "e.") })
	assert.Panics(t, func() { rec.Counter("1leading", "e.") })
	assert.Panics(t, func() { rec.Counter("ok_total", "e.", "bad:label") })
	assert.Panics(t, func() { rec.Counter("ok2_total", "e.", "dup", "dup") })
	assert.NotPanics(t, func() { rec.Counter("ns:sub_total", "colons ok in metric names") })
}

func TestConcurrentRecording(t *testing.T) {
	rec, snap := newRecorder(t)
	c := rec.Counter("par_total", "p.", "worker")
	h := rec.Histogram("par_seconds", "p.", []float64{0.5})
	g := rec.Gauge("par_depth", "p.")

	var wg sync.WaitGroup
	for w := range 8 {
		wg.Go(func() {
			label := []string{"a", "b"}[w%2]
			for range 1000 {
				c.Inc(label)
				h.Observe(0.25)
				g.Add(1)
			}
		})
	}
	wg.Wait()

	m, ok := snap()["par_total"].(map[string]any)
	require.True(t, ok)
	assert.InDelta(t, 4000, m[`worker="a"`], 1e-9)
	assert.InDelta(t, 4000, m[`worker="b"`], 1e-9)
	count, sum, _ := histSnapshot(t, snap()["par_seconds"])
	assert.InDelta(t, 8000, count, 1e-9)
	assert.InDelta(t, 2000, sum, 1e-6)
	assert.InDelta(t, 8000, snap()["par_depth"], 1e-9)
}

// Guards the /debug/vars document: expvar.Float would render NaN/+Inf raw and
// make the whole endpoint invalid JSON; forge renders non-finite as null.
func TestNonFiniteValuesRenderNull(t *testing.T) {
	rec, snap := newRecorder(t)
	rec.Gauge("ratio", "r.").Set(math.NaN())
	rec.Gauge("lratio", "r.", "q").Set(math.Inf(1), "a")
	rec.Counter("boom_total", "b.").Add(math.Inf(1)) // +Inf passes the negative-delta check
	rec.Histogram("h_seconds", "h.", []float64{1}).Observe(math.Inf(1))

	m := snap() // snap fails outright if the document is not valid JSON
	require.Contains(t, m, "ratio")
	assert.Nil(t, m["ratio"])
	assert.Nil(t, m["boom_total"])
	lm, ok := m["lratio"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, lm, `q="a"`)
	assert.Nil(t, lm[`q="a"`])
	hm, ok := m["h_seconds"].(map[string]any)
	require.True(t, ok)
	assert.Nil(t, hm["sum"], "non-finite histogram sum renders null")
	assert.InDelta(t, 1, hm["count"], 1e-9)
	buckets, ok := hm["buckets"].(map[string]any)
	require.True(t, ok)
	assert.InDelta(t, 1, buckets["+Inf"], 1e-9)
}

func TestLabelValueQuoting(t *testing.T) {
	rec, snap := newRecorder(t)
	c := rec.Counter("quoted_total", "q.", "path")
	c.Inc(`/a"b\c`)

	m, ok := snap()["quoted_total"].(map[string]any)
	require.True(t, ok)
	assert.InDelta(t, 1, m[`path="/a\"b\\c"`], 1e-9)
}
