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

type noopInstrument struct{}

func (noopInstrument) Add(float64, ...string)     {}
func (noopInstrument) Inc(...string)              {}
func (noopInstrument) Set(float64, ...string)     {}
func (noopInstrument) Observe(float64, ...string) {}
