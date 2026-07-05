package ratelimit

import (
	"context"
	"math"
	"strconv"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

// Result reports a limiter decision for one key.
type Result struct {
	Reset      time.Time // when the current window rolls
	Limit      int64
	Remaining  int64
	RetryAfter time.Duration // 0 when Allowed
	Allowed    bool
}

type config struct {
	clk    clock.Clock
	limit  int64
	window time.Duration
}

// Option configures a Limiter.
type Option func(*config)

// WithLimit sets n requests allowed per window. Required.
func WithLimit(n int64, per time.Duration) Option {
	return func(c *config) {
		c.limit = n
		c.window = per
	}
}

// WithClock injects a clock (for tests). Default clock.System().
func WithClock(clk clock.Clock) Option {
	return func(c *config) {
		if clk != nil {
			c.clk = clk
		}
	}
}

// Limiter is a keyed sliding-window-counter limiter over a Store.
type Limiter struct {
	store Store
	cfg   config
}

// New builds a Limiter. The Store's lifecycle is the caller's.
func New(store Store, opts ...Option) *Limiter {
	c := config{limit: 100, window: time.Minute, clk: clock.System()}
	for _, o := range opts {
		o(&c)
	}
	if c.window <= 0 {
		c.window = time.Minute
	}
	return &Limiter{store: store, cfg: c}
}

// Allow records one hit against key and reports whether it is within the limit,
// using a weighted current+previous fixed-window estimate.
func (l *Limiter) Allow(ctx context.Context, key string) (Result, error) {
	now := l.cfg.clk.Now()
	win := now.Truncate(l.cfg.window)
	elapsed := now.Sub(win)

	curKey := key + ":" + strconv.FormatInt(win.Unix(), 10)
	prevKey := key + ":" + strconv.FormatInt(win.Add(-l.cfg.window).Unix(), 10)

	cur, err := l.store.Incr(ctx, curKey, 1, 2*l.cfg.window)
	if err != nil {
		return Result{}, err
	}
	prev, err := l.store.Get(ctx, prevKey)
	if err != nil {
		return Result{}, err
	}

	weight := 1 - float64(elapsed)/float64(l.cfg.window)
	est := float64(cur) + float64(prev)*weight

	res := Result{Limit: l.cfg.limit, Reset: win.Add(l.cfg.window)}
	if est > float64(l.cfg.limit) {
		res.Allowed = false
		res.Remaining = 0
		// conservative: wait for the window to roll
		res.RetryAfter = max(res.Reset.Sub(now), 0)
		return res, nil
	}
	res.Allowed = true
	rem := max(l.cfg.limit-int64(math.Ceil(est)), 0)
	res.Remaining = rem
	return res, nil
}
