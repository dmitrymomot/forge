package smartlink

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

// defaultRefTTL bounds how long a compiled ref stays cached without a reload
// when [WithRefTTL] is not given.
const defaultRefTTL = 5 * time.Minute

// Resolver resolves a Ref-backed Link to a Decider ready for a click decision.
// [Cache.Resolver] returns a ready-made one; [WithResolver] wires it into a
// Manager. Resolver does not special-case ErrNoTarget: when the
// consumer's load func returns it for a paused or deleted offer, Resolver
// propagates the wrapped error and leaves the mapping to dead-link handling
// to the caller. A load func should return an error wrapping [ErrRefNotFound]
// for a ref that names no known Spec, so [Manager.Create]'s ref precheck can
// tell caller error from infrastructure failure.
type Resolver func(ctx context.Context, l Link) (Decider, error)

// Cache is a lazy compile cache for Ref-backed Links. Offers live in the
// consumer's own database as [Spec] values; Cache loads and compiles them on
// demand, keyed by ref string, and the consumer invalidates an entry from its
// offer-save path. Every entry also expires after the [WithRefTTL] duration,
// so a missed invalidation (or another node's save in a multi-node
// deployment) is stale for at most one TTL, and refs that stop being clicked
// do not stay resident forever. Safe for concurrent use.
type Cache struct {
	nextSweep time.Time
	clock     clock.Clock
	load      func(ctx context.Context, ref string) (Spec, error)
	entries   map[string]*refEntry
	opts      []Option
	ttl       time.Duration
	mu        sync.RWMutex
}

// refEntry is one ref's cache slot. The goroutine that inserts it starts one
// detached fill; concurrent Gets for the same ref wait on done — one load per
// ref per miss window, never a thundering herd against the consumer's
// database — with each caller's wait bounded by its own context. Invalidate
// simply removes the entry from the map, so an in-flight load finishes into a
// detached entry: its waiters still get the result (correct when their calls
// began), but the next Get misses and reloads fresh. Entry lifetime doubles
// as the cache itself, so there is no side bookkeeping to prune beyond the
// TTL sweep.
type refEntry struct {
	done      chan struct{} // closed when the fill completes
	compiled  *Compiled
	err       error
	expiresAt time.Time // set before done closes; zero while in flight
}

// expired reports whether e finished filling and its TTL has passed. An
// in-flight entry is never expired — joiners share the pending load.
func (e *refEntry) expired(now time.Time) bool {
	select {
	case <-e.done:
		return now.After(e.expiresAt)
	default:
		return false
	}
}

// cacheConfig holds NewCache's resolved options, collecting validation
// failures in errs the same way managerConfig does.
type cacheConfig struct {
	compileOpts []Option
	errs        []error
	ttl         time.Duration
}

// CacheOption configures [NewCache].
type CacheOption func(*cacheConfig)

// WithRefTTL bounds how long a successfully compiled ref is served before the
// next Get reloads it (default 5m). ttl must be positive: an unbounded entry
// would survive a missed [Cache.Invalidate] forever, and a multi-node
// deployment has no way to invalidate another node's process-local cache. A
// non-positive ttl is a NewCache error.
func WithRefTTL(ttl time.Duration) CacheOption {
	return func(c *cacheConfig) {
		if ttl <= 0 {
			c.errs = append(c.errs, fmt.Errorf("smartlink: ref ttl must be positive, got %s", ttl))
			return
		}
		c.ttl = ttl
	}
}

// WithCompileOptions sets the [Option] values (e.g. [WithClock]) the Cache
// passes to every [Compile] of a loaded Spec.
func WithCompileOptions(opts ...Option) CacheOption {
	return func(c *cacheConfig) { c.compileOpts = append(c.compileOpts, opts...) }
}

// NewCache returns a Cache that loads a Spec via load and compiles it on a
// cache miss; a nil load is a construction error. ref must be globally unique
// and load must be a pure function of ref — the cache is keyed by the bare
// ref string with no tenant dimension, so a multi-tenant consumer whose
// resolution differs per tenant must embed the tenant in ref itself.
//
// A loaded Spec with an empty Salt gets the ref as its Salt, so distinct
// refs bucket their splits and Percent matchers independently by default
// (see [Spec.Salt]).
//
// Errors from load or Compile are never cached, so the next Get retries.
func NewCache(load func(ctx context.Context, ref string) (Spec, error), opts ...CacheOption) (*Cache, error) {
	if load == nil {
		return nil, errors.New("smartlink: nil load func")
	}
	cfg := cacheConfig{ttl: defaultRefTTL}
	for _, o := range opts {
		o(&cfg)
	}
	if len(cfg.errs) > 0 {
		return nil, errors.Join(cfg.errs...)
	}
	return &Cache{
		load: load,
		opts: cfg.compileOpts,
		// TTL expiry shares the compile options' clock, so tests (and
		// TimeWindow matchers) observe one consistent time source.
		clock:   newConfig(cfg.compileOpts...).clock,
		ttl:     cfg.ttl,
		entries: make(map[string]*refEntry),
	}, nil
}

// Get returns the Compiled engine for ref, loading and compiling it on a
// cache miss (or after TTL expiry). Concurrent misses on the same ref share
// one load, which runs detached from any single request's cancellation; each
// caller's wait is still bounded by its own ctx, so a stuck load func cannot
// pin callers past their deadlines. Errors from load or Compile are wrapped,
// not cached.
func (c *Cache) Get(ctx context.Context, ref string) (*Compiled, error) {
	now := c.clock.Now()
	c.mu.RLock()
	e, ok := c.entries[ref]
	c.mu.RUnlock()
	if !ok || e.expired(now) {
		c.mu.Lock()
		e, ok = c.entries[ref]
		if !ok || e.expired(now) {
			e = &refEntry{done: make(chan struct{})}
			c.entries[ref] = e
			c.sweepLocked(now)
			go c.fill(context.WithoutCancel(ctx), ref, e)
		}
		c.mu.Unlock()
	}
	select {
	case <-e.done:
		return e.compiled, e.err
	case <-ctx.Done():
		return nil, fmt.Errorf("smartlink: load ref %q: %w", ref, ctx.Err())
	}
}

// sweepLocked prunes expired entries, at most once per TTL period so a miss
// burst never pays repeated full-map scans. Callers hold c.mu. Without this,
// refs that stop being clicked would stay resident until a Get for that exact
// ref happened to replace them.
func (c *Cache) sweepLocked(now time.Time) {
	if now.Before(c.nextSweep) {
		return
	}
	c.nextSweep = now.Add(c.ttl)
	for ref, e := range c.entries {
		if e.expired(now) {
			delete(c.entries, ref)
		}
	}
}

// fill loads and compiles ref into e and closes e.done exactly once, even if
// load or Compile panics (the panic is converted to an error every waiter
// observes — fill runs in its own goroutine, so there is no caller to
// re-panic into). An errored or panicked entry is removed — if still
// current — so the next Get retries instead of caching the failure; a
// successful one gets its TTL stamped before waiters are released.
func (c *Cache) fill(ctx context.Context, ref string, e *refEntry) {
	defer func() {
		if r := recover(); r != nil {
			e.compiled, e.err = nil, fmt.Errorf("smartlink: load ref %q: panic during load or compile: %v", ref, r)
		}
		if e.err != nil {
			c.removeEntry(ref, e)
		} else {
			e.expiresAt = c.clock.Now().Add(c.ttl)
		}
		close(e.done)
	}()
	e.compiled, e.err = c.loadCompile(ctx, ref)
}

// loadCompile loads ref's Spec and compiles it, defaulting an empty Salt to
// the ref. ctx is already cancellation-detached (see Get).
func (c *Cache) loadCompile(ctx context.Context, ref string) (*Compiled, error) {
	spec, err := c.load(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("smartlink: load ref %q: %w", ref, err)
	}
	if spec.Salt == "" {
		spec.Salt = ref
	}
	compiled, err := Compile(spec, c.opts...)
	if err != nil {
		return nil, fmt.Errorf("smartlink: compile ref %q: %w", ref, err)
	}
	return compiled, nil
}

// removeEntry deletes ref's slot only if it still holds e — an Invalidate
// (or a successor fill) may already have replaced it.
func (c *Cache) removeEntry(ref string, e *refEntry) {
	c.mu.Lock()
	if c.entries[ref] == e {
		delete(c.entries, ref)
	}
	c.mu.Unlock()
}

// Invalidate evicts ref's cached entry, if any, so the next Get reloads and
// recompiles it. Call this from the consumer's offer-save path. An in-flight
// load for ref finishes into the detached entry and is never re-admitted.
func (c *Cache) Invalidate(ref string) {
	c.mu.Lock()
	delete(c.entries, ref)
	c.mu.Unlock()
}

// Resolver returns a ready-made Resolver backed by c: it looks up l.Ref via
// Get and wraps the result with Chain(ds...). A load or compile error
// propagates wrapped, as returned by Get.
func (c *Cache) Resolver(ds ...Decorator) Resolver {
	chain := Chain(ds...)
	return func(ctx context.Context, l Link) (Decider, error) {
		compiled, err := c.Get(ctx, l.Ref)
		if err != nil {
			return nil, err
		}
		return chain(compiled), nil
	}
}
