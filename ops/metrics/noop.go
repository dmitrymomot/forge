package metrics

// NewNoop returns a Recorder that discards everything. Use as a safe default
// when metrics are an optional dependency.
func NewNoop() Recorder { return noopRecorder{} }

type noopRecorder struct{}

func (noopRecorder) Counter(string, string, ...string) Counter { return noopInstrument{} }

func (noopRecorder) Gauge(string, string, ...string) Gauge { return noopInstrument{} }

func (noopRecorder) Histogram(string, string, []float64, ...string) Histogram {
	return noopInstrument{}
}

// GaugeFunc drops the func without ever calling it, so a read behind it costs
// nothing when metrics are switched off.
func (noopRecorder) GaugeFunc(string, string, GaugeFunc) {}

type noopInstrument struct{}

func (noopInstrument) Add(float64, ...string)     {}
func (noopInstrument) Inc(...string)              {}
func (noopInstrument) Set(float64, ...string)     {}
func (noopInstrument) Observe(float64, ...string) {}
