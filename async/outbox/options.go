package outbox

import (
	"log/slog"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/backoff"
)

// Option configures NewRelay.
type Option func(*Relay)

// WithConfig replaces the default Config (pair with ops/config env loading).
func WithConfig(cfg Config) Option {
	return func(r *Relay) { r.cfg = cfg }
}

// WithName overrides the supervisor service name (default "outbox") —
// needed when one app runs several relays.
func WithName(name string) Option {
	return func(r *Relay) {
		if name != "" {
			r.name = name
		}
	}
}

// WithLogger injects a logger; the default discards everything.
func WithLogger(log *slog.Logger) Option {
	return func(r *Relay) {
		if log != nil {
			r.log = log
		}
	}
}

// WithClock injects a clock (tests).
func WithClock(c clock.Clock) Option {
	return func(r *Relay) {
		if c != nil {
			r.clk = c
		}
	}
}

// WithBackoff replaces the per-row push-retry backoff (default exponential,
// 5s base, 5m cap, 20% jitter).
func WithBackoff(bo backoff.Backoff) Option {
	return func(r *Relay) {
		if bo != nil {
			r.bo = bo
		}
	}
}
