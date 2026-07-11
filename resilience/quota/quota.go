package quota

import (
	"context"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/ratelimit"
)

// Meter tracks cumulative usage per subject over a Window, riding the shared
// ratelimit.Store counter seam. See the package doc for the covered shapes.
type Meter struct {
	store  ratelimit.Store
	window Window
	cfg    config
}

// New builds a Meter over store, using window to bucket usage. The store's
// lifecycle is the caller's.
func New(store ratelimit.Store, window Window, opts ...Option) *Meter {
	c := config{clk: clock.System()}
	for _, o := range opts {
		o(&c)
	}
	return &Meter{store: store, window: window, cfg: c}
}

func (m *Meter) key(subject, period string) string {
	if period == "" {
		return m.cfg.prefix + subject
	}
	return m.cfg.prefix + subject + ":" + period
}

// ttlFor returns the counter TTL for a window: time until reset, or -1 (no
// expiry) for gauges (reset is the zero Time).
func ttlFor(now, reset time.Time) time.Duration {
	if reset.IsZero() {
		return -1
	}
	if d := reset.Sub(now); d > 0 {
		return d
	}
	return time.Second
}

// Usage reports current consumption for subject without consuming.
func (m *Meter) Usage(ctx context.Context, subject string, limit Limit) (Result, error) {
	if err := limit.Validate(); err != nil {
		return Result{}, err
	}
	now := m.cfg.clk.Now()
	period, reset := m.window(subject, now)
	used, err := m.store.Get(ctx, m.key(subject, period))
	if err != nil {
		return Result{}, err
	}
	allowed := limit.Max == Unlimited || used <= limit.Max
	return makeResult(limit, used, reset, allowed), nil
}

// Allow consumes cost against subject and reports the decision. It uses
// incr-then-rollback: it increments by cost, and if the new total exceeds a
// finite Max it compensates with -cost and reports Allowed=false, so a rejected
// call does not burn quota.
func (m *Meter) Allow(ctx context.Context, subject string, cost int64, limit Limit) (Result, error) {
	if cost < 0 {
		return Result{}, ErrInvalidCost
	}
	if err := limit.Validate(); err != nil {
		return Result{}, err
	}
	now := m.cfg.clk.Now()
	period, reset := m.window(subject, now)
	key := m.key(subject, period)
	ttl := ttlFor(now, reset)

	used, err := m.store.Incr(ctx, key, cost, ttl)
	if err != nil {
		return Result{}, err
	}
	if limit.Max != Unlimited && used > limit.Max {
		if _, rbErr := m.store.Incr(ctx, key, -cost, ttl); rbErr != nil {
			return Result{}, rbErr
		}
		return makeResult(limit, used-cost, reset, false), nil
	}
	return makeResult(limit, used, reset, true), nil
}
