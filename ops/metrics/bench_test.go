package metrics_test

import (
	"context"
	"expvar"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/dmitrymomot/forge/ops/metrics"
)

var benchSeq atomic.Int64

// benchRecorder mints a unique expvar name per call: benchmark functions are
// re-invoked as b.N scales, and expvar forbids republishing a name.
func benchRecorder(b *testing.B) metrics.Recorder {
	b.Helper()
	return metrics.New(metrics.WithName("bench_" + b.Name() + "_" + strconv.FormatInt(benchSeq.Add(1), 10)))
}

func BenchmarkCounterIncUnlabeled(b *testing.B) {
	c := benchRecorder(b).Counter("ops_total", "b.")
	b.ReportAllocs()
	for b.Loop() {
		c.Inc()
	}
}

func BenchmarkCounterIncLabeled(b *testing.B) {
	c := benchRecorder(b).Counter("ops_total", "b.", "method", "status")
	b.ReportAllocs()
	for b.Loop() {
		c.Inc("GET", "200")
	}
}

func BenchmarkGaugeSetLabeled(b *testing.B) {
	g := benchRecorder(b).Gauge("depth", "b.", "queue")
	b.ReportAllocs()
	for b.Loop() {
		g.Set(42, "email")
	}
}

func BenchmarkHistogramObserveLabeled(b *testing.B) {
	h := benchRecorder(b).Histogram("latency_seconds", "b.", nil, "method", "status")
	b.ReportAllocs()
	for b.Loop() {
		h.Observe(0.042, "GET", "200")
	}
}

func BenchmarkHistogramObserveParallel(b *testing.B) {
	h := benchRecorder(b).Histogram("par_seconds", "b.", nil, "method")
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			h.Observe(0.042, "GET")
		}
	})
}

func benchMiddleware(b *testing.B, rec metrics.Recorder) {
	b.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", func(http.ResponseWriter, *http.Request) {})
	h := metrics.NewMiddleware(rec)(mux)
	r := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	w := httptest.NewRecorder()
	b.ReportAllocs()
	for b.Loop() {
		h.ServeHTTP(w, r)
	}
}

func BenchmarkMiddlewareExpvar(b *testing.B) { benchMiddleware(b, benchRecorder(b)) }

func BenchmarkMiddlewareNoop(b *testing.B) { benchMiddleware(b, metrics.NewNoop()) }

func BenchmarkNoopCounter(b *testing.B) {
	c := metrics.NewNoop().Counter("ops_total", "b.", "method")
	b.ReportAllocs()
	for b.Loop() {
		c.Inc("GET")
	}
}

// BenchmarkGaugeFuncCollect measures one pull read, which is what a scrape pays per
// registered GaugeFunc. The context with a deadline dominates: a push Gauge.Set costs
// nothing at collection time but needs a source that pushes.
func BenchmarkGaugeFuncCollect(b *testing.B) {
	name := "bench_" + b.Name() + "_" + strconv.FormatInt(benchSeq.Add(1), 10)
	rec := metrics.New(metrics.WithName(name))
	rec.GaugeFunc("queue_depth", "b.", func(context.Context) (float64, error) { return 42, nil })
	v := expvar.Get(name)
	b.ReportAllocs()
	for b.Loop() {
		_ = v.String()
	}
}

func BenchmarkGaugeSetForComparison(b *testing.B) {
	g := benchRecorder(b).Gauge("queue_depth", "b.")
	b.ReportAllocs()
	for b.Loop() {
		g.Set(42)
	}
}
