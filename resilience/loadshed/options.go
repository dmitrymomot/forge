package loadshed

import "github.com/dmitrymomot/forge/core/clock"

type config struct {
	clk       clock.Clock
	rnd       func() float64
	criteria  []Criteria
	threshold float64
	floor     float64
}

// Option configures a Shedder.
type Option func(*config)

// WithCriteria sets the pressure signals; overall pressure is their max.
func WithCriteria(cs ...Criteria) Option {
	return func(c *config) { c.criteria = append(c.criteria, cs...) }
}

// WithThreshold sets the low-water pressure below which all requests are
// admitted. Default 0.8. Clamped to [0,1].
func WithThreshold(t float64) Option {
	return func(c *config) { c.threshold = clamp01(t) }
}

// WithFloor sets the minimum admit fraction at full saturation (the fail-open
// sampler). Default 0.05. Clamped to [0,1].
func WithFloor(f float64) Option {
	return func(c *config) { c.floor = clamp01(f) }
}

// WithClock injects a clock (for tests). Default clock.System().
func WithClock(clk clock.Clock) Option {
	return func(c *config) {
		if clk != nil {
			c.clk = clk
		}
	}
}

// WithRand injects the [0,1) source used by the rejection ramp (for tests).
func WithRand(fn func() float64) Option {
	return func(c *config) {
		if fn != nil {
			c.rnd = fn
		}
	}
}

func clamp01(v float64) float64 {
	return min(max(v, 0), 1)
}
