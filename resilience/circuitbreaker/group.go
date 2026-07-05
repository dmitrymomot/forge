package circuitbreaker

import (
	"context"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

type groupConfig struct {
	breakerOpts   []Option
	idleTTL       time.Duration
	sweepInterval time.Duration
}

// GroupOption configures a Group.
type GroupOption func(*groupConfig)

// WithBreakerOptions sets the options applied to every per-key breaker the
// Group creates (including WithClock, which the Group reuses for eviction).
func WithBreakerOptions(opts ...Option) GroupOption {
	return func(c *groupConfig) { c.breakerOpts = opts }
}

// WithIdleTTL evicts a breaker after it has gone this long with no Do call,
// regardless of state (default 10m). Active traffic refreshes a breaker's
// last-access time, so a breaker in use is never evicted. Non-positive ignored.
func WithIdleTTL(d time.Duration) GroupOption {
	return func(c *groupConfig) {
		if d > 0 {
			c.idleTTL = d
		}
	}
}

// WithSweepInterval sets the minimum clock gap between eviction scans
// (default 1m). Scans run during Do, at most once per interval, so an idle
// Group does no work and holds no goroutine. Non-positive ignored.
func WithSweepInterval(d time.Duration) GroupOption {
	return func(c *groupConfig) {
		if d > 0 {
			c.sweepInterval = d
		}
	}
}

type groupEntry struct {
	breaker    *Breaker
	lastAccess time.Time
}

// Group manages breakers keyed by string, creating each on first use and
// sharing one option set. Breakers with no Do call for longer than the idle TTL
// are evicted opportunistically during Do, so an unbounded key space cannot
// leak memory and no background goroutine is needed. Safe for concurrent use.
type Group struct {
	entries   map[string]*groupEntry
	clk       clock.Clock
	lastSweep time.Time
	cfg       groupConfig
	mu        sync.Mutex
}

// NewGroup builds a Group. Configure per-key breakers with WithBreakerOptions
// and eviction with WithIdleTTL / WithSweepInterval.
func NewGroup(opts ...GroupOption) *Group {
	gc := groupConfig{idleTTL: 10 * time.Minute, sweepInterval: time.Minute}
	for _, o := range opts {
		o(&gc)
	}
	clk := newConfig(gc.breakerOpts...).clk // reuse the breaker clock
	return &Group{
		entries:   make(map[string]*groupEntry),
		clk:       clk,
		lastSweep: clk.Now(),
		cfg:       gc,
	}
}

// Do runs fn under key's breaker, creating it on first use. It returns an error
// matching ErrOpen when that breaker is open.
func (g *Group) Do(ctx context.Context, key string, fn func(context.Context) error) error {
	return g.breaker(key).Do(ctx, fn)
}

// State reports the state of key's breaker, or StateClosed if key has no
// breaker (never called, or evicted). Querying state is not traffic: it neither
// refreshes last-access nor triggers a sweep.
func (g *Group) State(key string) State {
	g.mu.Lock()
	defer g.mu.Unlock()
	if e, ok := g.entries[key]; ok {
		return e.breaker.State()
	}
	return StateClosed
}

// Len reports the number of live breakers (for tests and metrics).
func (g *Group) Len() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.entries)
}

// breaker returns key's breaker, creating it on first use. It refreshes the
// requested key's last-access time BEFORE running the eviction scan, so a key
// touched this call is never evicted this call (an in-use key always survives).
// The scan runs at most once per sweep interval. Shared by Do and the HTTP
// middleware.
func (g *Group) breaker(key string) *Breaker {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.clk.Now()
	e, ok := g.entries[key]
	if ok {
		e.lastAccess = now // refresh before the sweep so an active key survives
	}
	if now.Sub(g.lastSweep) >= g.cfg.sweepInterval {
		g.sweepLocked(now)
		g.lastSweep = now
	}
	if ok {
		return e.breaker
	}
	b := New(g.cfg.breakerOpts...)
	g.entries[key] = &groupEntry{breaker: b, lastAccess: now}
	return b
}

// sweepLocked drops entries idle beyond idleTTL. Caller holds g.mu.
func (g *Group) sweepLocked(now time.Time) {
	cutoff := now.Add(-g.cfg.idleTTL)
	for k, e := range g.entries {
		if e.lastAccess.Before(cutoff) {
			delete(g.entries, k)
		}
	}
}
