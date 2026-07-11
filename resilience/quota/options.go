package quota

import "github.com/dmitrymomot/forge/core/clock"

type config struct {
	clk    clock.Clock
	prefix string
}

// Option configures a Meter.
type Option func(*config)

// WithClock injects a clock (for tests). Default clock.System(). Pass the SAME
// clock to the underlying store so window rolls and TTL expiry stay in sync.
func WithClock(clk clock.Clock) Option {
	return func(c *config) {
		if clk != nil {
			c.clk = clk
		}
	}
}

// WithKeyPrefix namespaces every store key (e.g. "quota:").
func WithKeyPrefix(p string) Option {
	return func(c *config) { c.prefix = p }
}
