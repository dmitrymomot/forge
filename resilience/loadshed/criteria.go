package loadshed

import (
	"sync"
	"sync/atomic"
	"time"
)

// Criteria reports current load pressure in [0,1]; 0 idle, 1 saturated. It is
// polled once per admission decision. Implement it over any signal (CPU, queue
// depth, pool saturation) to plug a custom criterion into a Shedder.
type Criteria interface {
	Pressure() float64
}

// admitHook / doneHook let built-in criteria observe the request lifecycle. The
// Shedder type-asserts these; custom criteria may ignore them.
type admitHook interface{ onAdmit() }
type doneHook interface{ onDone(latency time.Duration) }

// concurrency reports inflight/max.
type concurrency struct {
	max      int64
	inflight atomic.Int64
}

// Concurrency returns a Criteria whose pressure is the in-flight count over max.
func Concurrency(max int) Criteria { return &concurrency{max: int64(max)} }

func (c *concurrency) Pressure() float64 {
	if c.max <= 0 {
		return 0
	}
	return float64(c.inflight.Load()) / float64(c.max)
}
func (c *concurrency) onAdmit()             { c.inflight.Add(1) }
func (c *concurrency) onDone(time.Duration) { c.inflight.Add(-1) }

type latencyConfig struct{ alpha float64 }

// LatencyOption configures the Latency criterion.
type LatencyOption func(*latencyConfig)

// WithEWMAAlpha sets the EWMA smoothing factor in (0,1]; higher reacts faster.
// Default 0.2.
func WithEWMAAlpha(a float64) LatencyOption {
	return func(c *latencyConfig) {
		if a > 0 && a <= 1 {
			c.alpha = a
		}
	}
}

// latency reports an EWMA of recent request latency over threshold.
type latency struct {
	mu        sync.Mutex
	ewma      float64
	threshold float64
	alpha     float64
}

// Latency returns a Criteria whose pressure is EWMA(latency)/threshold, clamped
// to [0,1]. It observes completion latency via the admit→done lifecycle.
func Latency(threshold time.Duration, opts ...LatencyOption) Criteria {
	c := latencyConfig{alpha: 0.2}
	for _, o := range opts {
		o(&c)
	}
	return &latency{threshold: float64(threshold), alpha: c.alpha}
}

func (l *latency) Pressure() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.threshold <= 0 {
		return 0
	}
	return min(l.ewma/l.threshold, 1)
}
func (l *latency) onAdmit() {}
func (l *latency) onDone(d time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := float64(d)
	if l.ewma == 0 {
		l.ewma = s
	} else {
		l.ewma = l.alpha*s + (1-l.alpha)*l.ewma
	}
}
