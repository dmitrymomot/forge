package smartlink

import "github.com/dmitrymomot/forge/core/clock"

type config struct {
	clock clock.Clock
}

// Option configures Compile.
type Option func(*config)

func newConfig(opts ...Option) config {
	c := config{clock: clock.System()}
	for _, o := range opts {
		o(&c)
	}
	return c
}

// WithClock sets the clock TimeWindow matchers evaluate against when Visit.At
// is zero. Defaults to clock.System(). A nil clock is ignored.
func WithClock(c clock.Clock) Option {
	return func(cfg *config) {
		if c != nil {
			cfg.clock = c
		}
	}
}
