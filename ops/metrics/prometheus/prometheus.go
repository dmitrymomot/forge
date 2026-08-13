package prometheus

import (
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/dmitrymomot/forge/ops/metrics"
)

type config struct {
	registry       *prom.Registry
	namespace      string
	collectTimeout time.Duration
}

// Option configures New.
type Option func(*config)

// WithRegistry uses the given registry instead of a fresh one. No standard
// collectors are added — the registry's contents are the caller's choice.
func WithRegistry(reg *prom.Registry) Option {
	return func(c *config) {
		if reg != nil {
			c.registry = reg
		}
	}
}

// WithNamespace prefixes every instrument name (rendered as "namespace_name").
func WithNamespace(namespace string) Option {
	return func(c *config) { c.namespace = namespace }
}

// WithCollectTimeout bounds every metrics.GaugeFunc call during a scrape
// (default metrics.DefaultCollectTimeout), so one stalled read cannot hold the
// scrape open. A non-positive duration is ignored.
func WithCollectTimeout(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.collectTimeout = d
		}
	}
}

// New returns a prometheus-backed metrics.Recorder. Without WithRegistry it
// creates a private registry preloaded with the standard Go runtime and
// process collectors. Scrape it via Handler.
func New(opts ...Option) *Recorder {
	var c config
	for _, o := range opts {
		o(&c)
	}
	if c.namespace != "" {
		metrics.MustValidNames(c.namespace)
	}
	if c.registry == nil {
		c.registry = prom.NewRegistry()
		c.registry.MustRegister(
			collectors.NewGoCollector(),
			collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		)
	}
	return &Recorder{
		registry:       c.registry,
		namespace:      c.namespace,
		instruments:    make(map[string]*entry),
		collectTimeout: metrics.ResolveCollectTimeout(c.collectTimeout),
	}
}

type kind uint8

const (
	kindCounter kind = iota
	kindGauge
	kindGaugeFunc
	kindHistogram
)

func (k kind) String() string {
	switch k {
	case kindCounter:
		return "counter"
	case kindGauge:
		return "gauge"
	case kindGaugeFunc:
		return "gauge func"
	default:
		return "histogram"
	}
}

type entry struct {
	inst       any
	labelNames []string
	buckets    []float64
	kind       kind
}

// Recorder implements metrics.Recorder over a prometheus registry. Instrument
// creation is idempotent per name; schema mismatches and invalid names panic
// (the latter via the prometheus client itself), matching the expvar recorder.
type Recorder struct {
	registry       *prom.Registry
	instruments    map[string]*entry
	namespace      string
	collectTimeout time.Duration
	mu             sync.Mutex
}

var _ metrics.Recorder = (*Recorder)(nil)

// Handler returns the scrape endpoint for the recorder's registry, typically
// mounted at GET /metrics.
func (r *Recorder) Handler() http.Handler {
	return promhttp.HandlerFor(r.registry, promhttp.HandlerOpts{})
}

func (r *Recorder) Counter(name, help string, labelNames ...string) metrics.Counter {
	return r.instrument(kindCounter, name, help, labelNames, nil).(metrics.Counter)
}

func (r *Recorder) Gauge(name, help string, labelNames ...string) metrics.Gauge {
	return r.instrument(kindGauge, name, help, labelNames, nil).(metrics.Gauge)
}

func (r *Recorder) Histogram(name, help string, buckets []float64, labelNames ...string) metrics.Histogram {
	// NormalizeBuckets (nil→DefaultBuckets, NaN/order panic, trailing +Inf
	// stripped) before storing and comparing, so bucket semantics and the
	// schema-mismatch check match the expvar recorder exactly.
	return r.instrument(kindHistogram, name, help, labelNames, metrics.NormalizeBuckets(buckets)).(metrics.Histogram)
}

// GaugeFunc registers a collector that reads fn during every scrape, so the value
// never has to be pushed. The failure counter is created first, outside the
// instrument lock, because instrument holds r.mu and Counter takes it.
func (r *Recorder) GaugeFunc(name, help string, fn metrics.GaugeFunc) {
	if fn == nil {
		panic("metrics: GaugeFunc " + strconv.Quote(name) + " received a nil func")
	}
	failures := metrics.CollectFailuresCounter(r)
	r.instrumentCollector(name, func() prom.Collector {
		return &gaugeFuncCollector{
			desc:     prom.NewDesc(prom.BuildFQName(r.namespace, "", name), help, nil, nil),
			fn:       fn,
			name:     name,
			timeout:  r.collectTimeout,
			failures: failures,
		}
	})
}

// instrumentCollector registers a pull-model collector under name, applying the same
// idempotency and schema-mismatch rules the vec instruments get.
func (r *Recorder) instrumentCollector(name string, build func() prom.Collector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.instruments[name]; ok {
		if e.kind != kindGaugeFunc {
			panic("metrics: instrument " + strconv.Quote(name) + " already registered as a different " + e.kind.String())
		}
		return
	}

	metrics.MustValidNames(name)
	coll := build()
	r.registry.MustRegister(coll)
	r.instruments[name] = &entry{kind: kindGaugeFunc, inst: coll}
}

// gaugeFuncCollector produces one sample per scrape. A failed read emits nothing —
// prometheus treats a missing series as stale, which is the honest answer when the
// source could not be read — and counts one failure.
type gaugeFuncCollector struct {
	desc     *prom.Desc
	fn       metrics.GaugeFunc
	failures metrics.Counter
	name     string
	timeout  time.Duration
}

func (c *gaugeFuncCollector) Describe(ch chan<- *prom.Desc) { ch <- c.desc }

func (c *gaugeFuncCollector) Collect(ch chan<- prom.Metric) {
	v, ok := metrics.CollectGauge(c.fn, c.name, c.timeout, c.failures)
	if !ok {
		return
	}
	ch <- prom.MustNewConstMetric(c.desc, prom.GaugeValue, v)
}

func (r *Recorder) instrument(k kind, name, help string, labelNames []string, buckets []float64) any {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.instruments[name]; ok {
		if e.kind != k || !slices.Equal(e.labelNames, labelNames) ||
			(k == kindHistogram && !slices.Equal(e.buckets, buckets)) {
			panic("metrics: instrument " + strconv.Quote(name) + " already registered as a different " + e.kind.String())
		}
		return e.inst
	}

	metrics.MustValidNames(name, labelNames...) // forge naming rules, not the client's (UTF-8-lenient) ones
	labelNames = slices.Clone(labelNames)
	var inst any
	var coll prom.Collector
	switch k {
	case kindCounter:
		vec := prom.NewCounterVec(prom.CounterOpts{Namespace: r.namespace, Name: name, Help: help}, labelNames)
		inst, coll = counter{vec: vec}, vec
	case kindGauge:
		vec := prom.NewGaugeVec(prom.GaugeOpts{Namespace: r.namespace, Name: name, Help: help}, labelNames)
		inst, coll = gauge{vec: vec}, vec
	default:
		vec := prom.NewHistogramVec(prom.HistogramOpts{Namespace: r.namespace, Name: name, Help: help, Buckets: buckets}, labelNames)
		inst, coll = histogram{vec: vec}, vec
	}
	r.registry.MustRegister(coll) // panics on invalid names or a registry-level collision
	r.instruments[name] = &entry{kind: k, labelNames: labelNames, buckets: buckets, inst: inst}
	return inst
}

// The wrappers delegate label handling to the client: WithLabelValues panics
// on a label-count mismatch, CounterVec panics on negative Add — the same
// contract the expvar recorder enforces.

type counter struct {
	vec *prom.CounterVec
}

func (c counter) Add(delta float64, labelValues ...string) {
	c.vec.WithLabelValues(labelValues...).Add(delta)
}

func (c counter) Inc(labelValues ...string) { c.vec.WithLabelValues(labelValues...).Inc() }

type gauge struct {
	vec *prom.GaugeVec
}

func (g gauge) Set(value float64, labelValues ...string) {
	g.vec.WithLabelValues(labelValues...).Set(value)
}

func (g gauge) Add(delta float64, labelValues ...string) {
	g.vec.WithLabelValues(labelValues...).Add(delta)
}

type histogram struct {
	vec *prom.HistogramVec
}

func (h histogram) Observe(value float64, labelValues ...string) {
	h.vec.WithLabelValues(labelValues...).Observe(value)
}
