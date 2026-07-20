package fxrate

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/decimal"
	"github.com/dmitrymomot/forge/resilience/singleflight"
)

// Converter serves conversions from a cached Snapshot, refreshed through a
// RateSource once the cached copy is older than the configured TTL.
// Concurrent refreshes are coalesced into one source call. It fails closed:
// when the snapshot is stale and the source errors, callers get the error —
// never silently-stale rates.
type Converter struct {
	snap    Snapshot  // guarded by mu
	fetched time.Time // guarded by mu
	source  RateSource
	clk     clock.Clock

	base string

	group singleflight.Group[Snapshot]

	quotes []string
	ttl    time.Duration

	mu sync.RWMutex
}

// New builds a Converter fetching rates denominated in base from source.
func New(source RateSource, base string, opts ...Option) (*Converter, error) {
	if source == nil {
		return nil, errors.New("fxrate: nil RateSource")
	}
	base = normalizeCode(base)
	if base == "" {
		return nil, errors.New("fxrate: empty base currency")
	}

	cfg := config{ttl: time.Hour, clk: clock.System()}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.ttl <= 0 {
		return nil, errors.New("fxrate: TTL must be positive")
	}
	if cfg.clk == nil {
		return nil, errors.New("fxrate: nil clock")
	}
	for _, q := range cfg.quotes {
		if q == "" {
			return nil, errors.New("fxrate: empty quote currency")
		}
		if q == base {
			return nil, fmt.Errorf("fxrate: quote %s equals base", q)
		}
	}

	return &Converter{source: source, base: base, quotes: cfg.quotes, ttl: cfg.ttl, clk: cfg.clk}, nil
}

// Convert converts amount between two currencies using the current snapshot:
// rate lookup, exact multiply, one rounding to scale digits with mode. The
// returned Conversion records the applied rate for audit.
func (c *Converter) Convert(ctx context.Context, amount decimal.Decimal, from, to string, scale int32, mode decimal.RoundingMode) (Conversion, error) {
	s, err := c.Snapshot(ctx)
	if err != nil {
		return Conversion{}, err
	}
	return s.Convert(amount, from, to, scale, mode)
}

// Rate returns the current rate converting from into to.
func (c *Converter) Rate(ctx context.Context, from, to string) (Rate, error) {
	s, err := c.Snapshot(ctx)
	if err != nil {
		return Rate{}, err
	}
	return s.Rate(from, to)
}

// Snapshot returns the cached snapshot, fetching from the source when none is
// cached yet or the cache is older than the TTL. Persist the returned value
// alongside the transaction to make conversions recomputable forever.
func (c *Converter) Snapshot(ctx context.Context) (Snapshot, error) {
	if snap, ok := c.cached(); ok {
		return snap, nil
	}
	// DoDetached, not Do: a caller joining an in-flight refresh waits only
	// until its own ctx ends; the shared fetch keeps running for the rest.
	snap, _, err := c.group.DoDetached(ctx, "refresh", func(ctx context.Context) (Snapshot, error) {
		// Re-check after winning the flight: a refresh that completed while
		// this caller was deciding to fetch already did the work.
		if snap, ok := c.cached(); ok {
			return snap, nil
		}
		return c.fetch(ctx)
	})
	return snap, err
}

// cached returns the snapshot when present and fresher than the TTL.
func (c *Converter) cached() (Snapshot, bool) {
	c.mu.RLock()
	snap, fetched := c.snap, c.fetched
	c.mu.RUnlock()
	if snap.IsZero() || c.clk.Now().Sub(fetched) >= c.ttl {
		return Snapshot{}, false
	}
	return snap, true
}

// fetch pulls a fresh snapshot from the source, validates it against the
// converter's configuration, and caches it.
func (c *Converter) fetch(ctx context.Context) (Snapshot, error) {
	snap, err := c.source.Fetch(ctx, c.base, slices.Clone(c.quotes))
	if err != nil {
		return Snapshot{}, fmt.Errorf("%w: %w", ErrFetchFailed, err)
	}
	if snap.IsZero() {
		return Snapshot{}, fmt.Errorf("%w: source returned zero snapshot", ErrFetchFailed)
	}
	if snap.Base() != c.base {
		return Snapshot{}, fmt.Errorf("%w: requested %s, source returned %s", ErrBaseMismatch, c.base, snap.Base())
	}
	for _, q := range c.quotes {
		if !snap.Has(q) {
			return Snapshot{}, fmt.Errorf("%w: source omitted requested quote %s", ErrFetchFailed, q)
		}
	}

	c.mu.Lock()
	c.snap = snap
	c.fetched = c.clk.Now()
	c.mu.Unlock()
	return snap, nil
}
