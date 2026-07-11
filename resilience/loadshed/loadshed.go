package loadshed

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

// Shedder is an adaptive admission controller: it rejects a fraction of work
// when its Criteria report overload, protecting the service from itself.
type Shedder struct {
	clk        clock.Clock
	rnd        func() float64
	criteria   []Criteria
	admitHooks []admitHook
	doneHooks  []doneHook
	threshold  float64
	floor      float64
}

// New builds a Shedder. Defaults: threshold 0.8, floor 0.05, clock.System().
func New(opts ...Option) *Shedder {
	c := config{clk: clock.System(), rnd: rand.Float64, threshold: 0.8, floor: 0.05}
	for _, o := range opts {
		o(&c)
	}
	s := &Shedder{
		clk: c.clk, rnd: c.rnd, criteria: c.criteria,
		threshold: c.threshold, floor: c.floor,
	}
	for _, cr := range c.criteria {
		if h, ok := cr.(admitHook); ok {
			s.admitHooks = append(s.admitHooks, h)
		}
		if h, ok := cr.(doneHook); ok {
			s.doneHooks = append(s.doneHooks, h)
		}
	}
	return s
}

// Ticket is returned by an admitted Acquire; Release MUST be called on
// completion (records latency, decrements inflight).
type Ticket interface{ Release() }

type ticket struct {
	s     *Shedder
	start time.Time
	done  bool
}

func (t *ticket) Release() {
	if t.done {
		return
	}
	t.done = true
	d := t.s.clk.Now().Sub(t.start)
	for _, h := range t.s.doneHooks {
		h.onDone(d)
	}
}

// Acquire reports whether the request is admitted. On admit it returns a Ticket
// whose Release must be called; on shed it returns (nil, false).
func (s *Shedder) Acquire(_ context.Context) (Ticket, bool) {
	if !s.admit() {
		return nil, false
	}
	for _, h := range s.admitHooks {
		h.onAdmit()
	}
	return &ticket{s: s, start: s.clk.Now()}, true
}

// admit applies the probabilistic rejection ramp: admit all below threshold,
// then reject with probability rising to (1 - floor) at saturation.
func (s *Shedder) admit() bool {
	p := s.pressure()
	if p <= s.threshold {
		return true
	}
	denom := 1 - s.threshold
	frac := 1.0
	if denom > 0 {
		frac = (p - s.threshold) / denom
	}
	frac = min(frac, 1)
	rejectProb := frac * (1 - s.floor)
	return s.rnd() >= rejectProb
}

// pressure is the max over all criteria; a panicking criterion contributes 0
// (fail open — a monitoring glitch must not become an outage).
func (s *Shedder) pressure() float64 {
	maxP := 0.0
	for _, c := range s.criteria {
		maxP = max(maxP, safePressure(c))
	}
	return maxP
}

func safePressure(c Criteria) (p float64) {
	defer func() {
		if recover() != nil {
			p = 0
		}
	}()
	return c.Pressure()
}
