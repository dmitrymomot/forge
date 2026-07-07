// ops/featureflag/cached.go
package featureflag

import (
	"context"
	"maps"
	"sync"
	"sync/atomic"
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
	c := &cached{p: p, ttl: ttl, cfg: cfg}
	c.data.Store(&cacheData{})
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

// cacheData is the scope → key → entry snapshot. It is treated as immutable
// once published: writers build a new snapshot and swap the pointer so
// readers on the hit path never take a lock (see cached.Flag).
type cacheData map[string]map[string]cacheEntry

type cached struct {
	p       Provider
	cfg     cachedConfig
	data    atomic.Pointer[cacheData] // copy-on-write snapshot, lock-free reads
	sf      singleflight.Group[cacheEntry]
	writeMu sync.Mutex // serializes snapshot construction; readers never block on this
	ttl     time.Duration
}

func (c *cached) Flag(ctx context.Context, key string) (Flag, bool, error) {
	scope := c.cfg.scope(ctx)
	e, hit := (*c.data.Load())[scope][key]
	if hit && c.cfg.clk.Now().Sub(e.at) < c.ttl {
		return e.flag, e.ok, nil
	}
	fresh, _, err := c.sf.Do(ctx, scope+"\x00"+key, func(ctx context.Context) (cacheEntry, error) {
		f, ok, err := c.p.Flag(ctx, key)
		if err != nil {
			return cacheEntry{}, err
		}
		ne := cacheEntry{flag: f, ok: ok, at: c.cfg.clk.Now()}
		c.store(scope, key, ne)
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

// store publishes ne under scope/key via copy-on-write: it copies the outer
// snapshot and the touched scope's inner map, then atomically swaps the
// pointer. Writers serialize on writeMu (refreshes are already singleflighted
// per key, so this is never hot); readers are entirely lock-free.
func (c *cached) store(scope, key string, ne cacheEntry) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	old := *c.data.Load()
	next := make(cacheData, len(old)+1)
	maps.Copy(next, old)
	inner := next[scope]
	newInner := make(map[string]cacheEntry, len(inner)+1)
	maps.Copy(newInner, inner)
	newInner[key] = ne
	next[scope] = newInner
	c.data.Store(&next)
}

type cachedLister struct {
	*cached
	lister Lister
}

func (c *cachedLister) All(ctx context.Context) (Flags, error) {
	return c.lister.All(ctx)
}
