package metrics

import (
	"encoding/json"
	"expvar"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

type config struct {
	name string
}

// Option configures the expvar recorder returned by New.
type Option func(*config)

// WithName sets the expvar variable name the recorder publishes under
// (default "metrics"). An empty name is ignored.
func WithName(name string) Option {
	return func(c *config) {
		if name != "" {
			c.name = name
		}
	}
}

// New returns the default Recorder: instruments are aggregated in-process and
// published as a single expvar map (default name "metrics"), so they appear on
// /debug/vars with zero external dependencies. Labeled instruments render as
// objects keyed by the label set (`method="GET",status="200"`); histograms
// render as {"count","sum","buckets"} with cumulative upper-inclusive buckets.
//
// expvar names are process-global: publishing a second recorder under the same
// name panics (expvar semantics). Use WithName to run multiple recorders.
func New(opts ...Option) Recorder {
	c := config{name: "metrics"}
	for _, o := range opts {
		o(&c)
	}
	return &expvarRecorder{
		root:        expvar.NewMap(c.name),
		instruments: make(map[string]*entry),
	}
}

type kind uint8

const (
	kindCounter kind = iota
	kindGauge
	kindHistogram
)

func (k kind) String() string {
	switch k {
	case kindCounter:
		return "counter"
	case kindGauge:
		return "gauge"
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

type expvarRecorder struct {
	root        *expvar.Map
	instruments map[string]*entry
	mu          sync.Mutex
}

func (r *expvarRecorder) Counter(name, _ string, labelNames ...string) Counter {
	return r.instrument(kindCounter, name, labelNames, nil).(Counter)
}

func (r *expvarRecorder) Gauge(name, _ string, labelNames ...string) Gauge {
	return r.instrument(kindGauge, name, labelNames, nil).(Gauge)
}

func (r *expvarRecorder) Histogram(name, _ string, buckets []float64, labelNames ...string) Histogram {
	return r.instrument(kindHistogram, name, labelNames, NormalizeBuckets(buckets)).(Histogram)
}

// instrument returns the existing instrument under name or creates it. The
// help text is accepted for adapter parity but not rendered: expvar output is
// a bare JSON snapshot with no metadata section to carry it.
func (r *expvarRecorder) instrument(k kind, name string, labelNames []string, buckets []float64) any {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.instruments[name]; ok {
		if e.kind != k || !slices.Equal(e.labelNames, labelNames) ||
			(k == kindHistogram && !slices.Equal(e.buckets, buckets)) {
			panic("metrics: instrument " + strconv.Quote(name) + " already registered as a different " + e.kind.String())
		}
		return e.inst
	}

	MustValidNames(name, labelNames...)
	labelNames = slices.Clone(labelNames)

	var inst any
	var v expvar.Var
	switch k {
	case kindCounter:
		cv := &counterVec{labelNames: labelNames}
		if len(labelNames) == 0 {
			cv.single = new(floatVar)
			v = cv.single
		} else {
			v = cv
		}
		inst = cv
	case kindGauge:
		gv := &gaugeVec{labelNames: labelNames}
		if len(labelNames) == 0 {
			gv.single = new(floatVar)
			v = gv.single
		} else {
			v = gv
		}
		inst = gv
	default:
		hv := &histogramVec{labelNames: labelNames, bounds: buckets}
		if len(labelNames) == 0 {
			hv.single = newHistogram(buckets)
			v = hv.single
		} else {
			v = hv
		}
		inst = hv
	}
	r.root.Set(name, v)
	r.instruments[name] = &entry{kind: k, labelNames: labelNames, buckets: buckets, inst: inst}
	return inst
}

// labelKey renders a label set as `name="value",...` — both the aggregation
// identity and the JSON object key shown on /debug/vars.
func labelKey(names, values []string) string {
	checkLabelCount(names, values)
	n := 0
	for i := range names {
		n += len(names[i]) + len(values[i]) + 4 //nolint:nilaway // checkLabelCount proved len(values)==len(names), so values is never nil here
	}
	// One buffer + AppendQuote instead of strings.Builder + strconv.Quote:
	// Quote allocates per value, this path allocates once (plus the string).
	buf := make([]byte, 0, n)
	for i, name := range names {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, name...)
		buf = append(buf, '=')
		buf = strconv.AppendQuote(buf, values[i])
	}
	return string(buf)
}

func checkLabelCount(names, values []string) {
	if len(names) != len(values) {
		panic("metrics: expected " + strconv.Itoa(len(names)) + " label values, got " + strconv.Itoa(len(values)))
	}
}

// floatVar is the expvar.Var behind counters and gauges. Unlike expvar.Float,
// it renders non-finite values as null — expvar.Float emits NaN/+Inf verbatim,
// which would corrupt the entire /debug/vars JSON document.
type floatVar struct {
	atomicFloat
}

func (f *floatVar) String() string {
	v := f.load()
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "null"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// loadFloat returns the floatVar under key, creating it on first use.
func loadFloat(m *sync.Map, key string) *floatVar {
	if v, ok := m.Load(key); ok {
		return v.(*floatVar)
	}
	v, _ := m.LoadOrStore(key, new(floatVar))
	return v.(*floatVar)
}

// vecString renders a labeled vec as one JSON object; Marshal escapes the
// label-set keys and sorts them for stable output.
func vecString(m *sync.Map) string {
	out := make(map[string]json.RawMessage)
	m.Range(func(k, v any) bool {
		out[k.(string)] = json.RawMessage(v.(expvar.Var).String())
		return true
	})
	b, err := json.Marshal(out)
	if err != nil {
		return "{}"
	}
	return string(b)
}

type counterVec struct {
	single     *floatVar
	m          sync.Map // labelKey string -> *floatVar
	labelNames []string
}

func (c *counterVec) Add(delta float64, labelValues ...string) {
	if delta < 0 {
		panic("metrics: counter cannot decrease")
	}
	if c.single != nil {
		checkLabelCount(c.labelNames, labelValues)
		c.single.Add(delta)
		return
	}
	loadFloat(&c.m, labelKey(c.labelNames, labelValues)).Add(delta)
}

func (c *counterVec) Inc(labelValues ...string) { c.Add(1, labelValues...) }

func (c *counterVec) String() string { return vecString(&c.m) }

type gaugeVec struct {
	single     *floatVar
	m          sync.Map // labelKey string -> *floatVar
	labelNames []string
}

func (g *gaugeVec) Set(value float64, labelValues ...string) {
	if g.single != nil {
		checkLabelCount(g.labelNames, labelValues)
		g.single.store(value)
		return
	}
	loadFloat(&g.m, labelKey(g.labelNames, labelValues)).store(value)
}

func (g *gaugeVec) Add(delta float64, labelValues ...string) {
	if g.single != nil {
		checkLabelCount(g.labelNames, labelValues)
		g.single.Add(delta)
		return
	}
	loadFloat(&g.m, labelKey(g.labelNames, labelValues)).Add(delta)
}

func (g *gaugeVec) String() string { return vecString(&g.m) }

type histogramVec struct {
	single     *histogram
	m          sync.Map // labelKey string -> *histogram
	bounds     []float64
	labelNames []string
}

func (h *histogramVec) Observe(value float64, labelValues ...string) {
	if h.single != nil {
		checkLabelCount(h.labelNames, labelValues)
		h.single.observe(value)
		return
	}
	key := labelKey(h.labelNames, labelValues)
	hi, ok := h.m.Load(key)
	if !ok {
		hi, _ = h.m.LoadOrStore(key, newHistogram(h.bounds))
	}
	hi.(*histogram).observe(value)
}

func (h *histogramVec) String() string { return vecString(&h.m) }

// histogram aggregates observations lock-free: per-bucket non-cumulative
// atomic counts plus an atomic sum. A scrape may observe a count/sum/bucket
// snapshot mid-update (standard for lock-free metrics; prometheus behaves the
// same way under its lock-free hot path).
type histogram struct {
	bounds []float64
	counts []atomic.Uint64 // len(bounds)+1; last is the +Inf overflow bucket
	count  atomic.Uint64
	sum    atomicFloat
}

func newHistogram(bounds []float64) *histogram {
	return &histogram{bounds: bounds, counts: make([]atomic.Uint64, len(bounds)+1)}
}

func (h *histogram) observe(v float64) {
	// SearchFloat64s returns the first i with bounds[i] >= v — exactly the
	// upper-inclusive (le) bucket; NaN and out-of-range land in +Inf.
	h.counts[sort.SearchFloat64s(h.bounds, v)].Add(1)
	h.count.Add(1)
	h.sum.Add(v)
}

// String implements expvar.Var: {"count":N,"sum":S,"buckets":{"0.005":c,...,"+Inf":c}}
// with cumulative bucket counts, matching prometheus semantics.
func (h *histogram) String() string {
	var b strings.Builder
	b.Grow(64 + len(h.bounds)*16)
	b.WriteString(`{"count":`)
	b.WriteString(strconv.FormatUint(h.count.Load(), 10))
	b.WriteString(`,"sum":`)
	if sum := h.sum.load(); math.IsNaN(sum) || math.IsInf(sum, 0) {
		b.WriteString("null")
	} else {
		b.WriteString(strconv.FormatFloat(sum, 'g', -1, 64))
	}
	b.WriteString(`,"buckets":{`)
	var cumulative uint64
	for i, bound := range h.bounds {
		if i > 0 {
			b.WriteByte(',')
		}
		cumulative += h.counts[i].Load()
		b.WriteByte('"')
		b.WriteString(strconv.FormatFloat(bound, 'g', -1, 64))
		b.WriteString(`":`)
		b.WriteString(strconv.FormatUint(cumulative, 10))
	}
	if len(h.bounds) > 0 {
		b.WriteByte(',')
	}
	cumulative += h.counts[len(h.bounds)].Load()
	b.WriteString(`"+Inf":`)
	b.WriteString(strconv.FormatUint(cumulative, 10))
	b.WriteString("}}")
	return b.String()
}

type atomicFloat struct {
	bits atomic.Uint64
}

func (f *atomicFloat) Add(delta float64) {
	for {
		old := f.bits.Load()
		if f.bits.CompareAndSwap(old, math.Float64bits(math.Float64frombits(old)+delta)) {
			return
		}
	}
}

func (f *atomicFloat) store(v float64) { f.bits.Store(math.Float64bits(v)) }

func (f *atomicFloat) load() float64 { return math.Float64frombits(f.bits.Load()) }
