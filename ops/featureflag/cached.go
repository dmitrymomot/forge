// ops/featureflag/cached.go
package featureflag

import (
	"context"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/singleflight"
)

// CacheOption configures Cached.
type CacheOption func(*cachedConfig)

type cachedConfig struct {
	scope func(ctx context.Context) string
	clk   clock.Clock
}

// CacheKey sets the scope function partitioning cache entries — for
// multi-tenant providers return the tenant ID from ctx. Default scope is ""
// (single-tenant). Cache cardinality is scopes × flags, never users.
func CacheKey(scope func(ctx context.Context) string) CacheOption {
	return func(c *cachedConfig) { c.scope = scope }
}

// CacheClock injects a clock (tests). Default: clock.System().
func CacheClock(clk clock.Clock) CacheOption {
	return func(c *cachedConfig) { c.clk = clk }
}

// Cached wraps a Provider with scope-aware TTL caching: singleflight
// refresh (one loader per entry regardless of concurrent readers),
// serve-stale-on-error (a failed refresh serves the last-known value — the
// failure mode is "yesterday's flags", not "everything off"), and negative
// caching of misses. Entries live for the process lifetime; memory stays
// bounded because cardinality is scopes × flags.
//
// If p implements Lister, the returned Provider does too (All passes
// through uncached — it is an admin/debug path).
//
// Cached panics if p is nil (programmer error, mirrors ErrNilProvider).
func Cached(p Provider, ttl time.Duration, opts ...CacheOption) Provider {
	if p == nil {
		panic(ErrNilProvider.Error())
	}
	cfg := cachedConfig{
		scope: func(context.Context) string { return "" },
		clk:   clock.System(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	c := &cached{p: p, ttl: ttl, cfg: cfg, data: map[string]map[string]cacheEntry{}}
	if l, ok := p.(Lister); ok {
		return &cachedLister{cached: c, lister: l}
	}
	return c
}

type cacheEntry struct {
	at   time.Time
	flag Flag
	ok   bool
}

type cached struct {
	p    Provider
	cfg  cachedConfig
	data map[string]map[string]cacheEntry // scope → key → entry
	sf   singleflight.Group[cacheEntry]
	mu   sync.RWMutex
	ttl  time.Duration
}

func (c *cached) Flag(ctx context.Context, key string) (Flag, bool, error) {
	scope := c.cfg.scope(ctx)
	c.mu.RLock()
	e, hit := c.data[scope][key]
	c.mu.RUnlock()
	if hit && c.cfg.clk.Now().Sub(e.at) < c.ttl {
		return e.flag, e.ok, nil
	}
	fresh, _, err := c.sf.Do(ctx, scope+"\x00"+key, func(ctx context.Context) (cacheEntry, error) {
		f, ok, err := c.p.Flag(ctx, key)
		if err != nil {
			return cacheEntry{}, err
		}
		ne := cacheEntry{flag: f, ok: ok, at: c.cfg.clk.Now()}
		c.mu.Lock()
		m := c.data[scope]
		if m == nil {
			m = map[string]cacheEntry{}
			c.data[scope] = m
		}
		m[key] = ne
		c.mu.Unlock()
		return ne, nil
	})
	if err != nil {
		if hit {
			return e.flag, e.ok, nil // serve stale
		}
		return Flag{}, false, err
	}
	return fresh.flag, fresh.ok, nil
}

type cachedLister struct {
	*cached
	lister Lister
}

func (c *cachedLister) All(ctx context.Context) (Flags, error) {
	return c.lister.All(ctx)
}
