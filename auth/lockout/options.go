package lockout

import (
	"context"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

type config struct {
	scope     func(context.Context) (string, error)
	clk       clock.Clock
	threshold int64
	baseLock  time.Duration
	maxLock   time.Duration
	window    time.Duration
	factor    float64
}

// Option configures a Locker.
type Option func(*config)

// WithThreshold sets the number of free failures before the first lock.
// Default 5.
func WithThreshold(n int) Option { return func(c *config) { c.threshold = int64(n) } }

// WithBaseLock sets the first lock duration. Default 1 minute.
func WithBaseLock(d time.Duration) Option { return func(c *config) { c.baseLock = d } }

// WithFactor sets the escalation multiplier applied to each lock after the
// first; 1.0 means fixed-duration locks. Default 2.0.
func WithFactor(f float64) Option { return func(c *config) { c.factor = f } }

// WithMaxLock caps the escalated lock duration. Default 15 minutes.
func WithMaxLock(d time.Duration) Option { return func(c *config) { c.maxLock = d } }

// WithWindow sets the failure-memory window. The counter's TTL is fixed when
// the first failure of a burst creates it (Incr never extends a live TTL), so
// New enforces window >= max lock and rejects configs that violate it —
// otherwise escalation memory would expire before the last lock does.
// Default 30 minutes.
func WithWindow(d time.Duration) Option { return func(c *config) { c.window = d } }

// WithClock injects a clock (for tests). Default clock.System().
func WithClock(clk clock.Clock) Option {
	return func(c *config) {
		if clk != nil {
			c.clk = clk
		}
	}
}

// WithScope derives a tenant scope from the request context on every call and
// isolates all keys per scope. An error or empty scope fails closed with
// ErrScope. Without this option keys are unscoped (single-tenant).
func WithScope(fn func(context.Context) (string, error)) Option {
	return func(c *config) { c.scope = fn }
}
