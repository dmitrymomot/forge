package backoff

import (
	"math"
	"math/rand/v2"
	"time"
)

// Backoff returns the delay to wait before a given 1-based attempt.
// Implementations are stateless and safe for concurrent use.
type Backoff interface {
	Next(attempt int) time.Duration
}

type constant struct{ d time.Duration }

func (c constant) Next(int) time.Duration { return c.d }

// Constant always returns d. A negative d is clamped to 0.
func Constant(d time.Duration) Backoff {
	if d < 0 {
		d = 0
	}
	return constant{d: d}
}

type exponential struct {
	base       time.Duration
	max        time.Duration
	multiplier float64
	jitter     float64
}

// Option configures Exponential.
type Option func(*exponential)

// WithMultiplier sets the growth factor (default 2.0). Values ≤ 0 are ignored.
func WithMultiplier(f float64) Option {
	return func(e *exponential) {
		if f > 0 {
			e.multiplier = f
		}
	}
}

// WithJitter randomizes each delay by ±fraction (clamped to 0..1).
func WithJitter(fraction float64) Option {
	return func(e *exponential) {
		e.jitter = math.Min(1, math.Max(0, fraction))
	}
}

func (e exponential) Next(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := float64(e.base) * math.Pow(e.multiplier, float64(attempt-1))
	if d > float64(e.max) {
		d = float64(e.max)
	}
	if e.jitter > 0 {
		delta := d * e.jitter
		d = d - delta + rand.Float64()*(2*delta)
	}
	if d > float64(e.max) { // re-clamp: jitter must not exceed the ceiling
		d = float64(e.max)
	}
	if d < 0 {
		d = 0
	}
	return time.Duration(d)
}

// Exponential grows the delay as base*multiplier^(attempt-1), capped at max.
// base is clamped to ≥ 1ns and max to ≥ base.
func Exponential(base, max time.Duration, opts ...Option) Backoff {
	if base < 1 {
		base = 1
	}
	if max < base {
		max = base
	}
	e := exponential{base: base, max: max, multiplier: 2.0}
	for _, o := range opts {
		o(&e)
	}
	return e
}
