package smartlink

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

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
// offer-save path. Safe for concurrent use.
type Cache struct {
	load    func(ctx context.Context, ref string) (Spec, error)
	entries map[string]*refEntry
	opts    []Option
	mu      sync.RWMutex
}

// refEntry is one ref's cache slot. The goroutine that inserts it (the
// leader) loads and compiles while concurrent Gets for the same ref wait on
// wg — one load per ref per miss window, never a thundering herd against the
// consumer's database. Invalidate simply removes the entry from the map, so
// an in-flight leader finishes into a detached entry: its waiters still get
// the result (correct when their calls began), but the next Get misses and
// reloads fresh. Entry lifetime doubles as the cache itself, so there is no
// side bookkeeping to prune.
type refEntry struct {
	compiled *Compiled
	err      error
	wg       sync.WaitGroup
}

// NewCache returns a Cache that loads a Spec via load and compiles it with
// compileOpts on a cache miss; a nil load is a construction error. ref must
// be globally unique and load must be a pure function of ref — the cache is
// keyed by the bare ref string with no tenant dimension, so a multi-tenant
// consumer whose resolution differs per tenant must embed the tenant in ref
// itself.
//
// A loaded Spec with an empty Salt gets the ref as its Salt, so distinct
// refs bucket their splits and Percent matchers independently by default
// (see [Spec.Salt]).
//
// Errors from load or Compile are never cached, so the next Get retries.
func NewCache(load func(ctx context.Context, ref string) (Spec, error), compileOpts ...Option) (*Cache, error) {
	if load == nil {
		return nil, errors.New("smartlink: nil load func")
	}
	return &Cache{
		load:    load,
		opts:    compileOpts,
		entries: make(map[string]*refEntry),
	}, nil
}

// Get returns the Compiled engine for ref, loading and compiling it on a
// cache miss. Concurrent misses on the same ref share one load. Errors from
// load or Compile are wrapped, not cached.
func (c *Cache) Get(ctx context.Context, ref string) (*Compiled, error) {
	c.mu.RLock()
	e, ok := c.entries[ref]
	c.mu.RUnlock()
	if !ok {
		var leader bool
		c.mu.Lock()
		if e, ok = c.entries[ref]; !ok {
			e = &refEntry{}
			e.wg.Add(1)
			c.entries[ref] = e
			leader = true
		}
		c.mu.Unlock()
		if leader {
			return c.fill(ctx, ref, e)
		}
	}
	e.wg.Wait()
	return e.compiled, e.err
}

// fill loads and compiles ref into e, releasing waiters exactly once even if
// load or Compile panics (the panic then propagates to the leader's caller;
// waiters observe an error). load runs with cancellation detached so one
// canceled request cannot fail the waiters sharing the flight
// (resilience/singleflight precedent). An errored or panicked entry is
// removed — if still current — so the next Get retries instead of caching
// the failure.
func (c *Cache) fill(ctx context.Context, ref string, e *refEntry) (*Compiled, error) {
	finished := false
	defer func() {
		if !finished {
			e.err = fmt.Errorf("smartlink: load ref %q: panic during load or compile", ref)
			e.wg.Done()
			c.removeEntry(ref, e)
		}
	}()

	compiled, err := c.loadCompile(ctx, ref)
	e.compiled, e.err = compiled, err
	finished = true
	e.wg.Done()
	if err != nil {
		c.removeEntry(ref, e)
		return nil, err
	}
	return compiled, nil
}

// loadCompile loads ref's Spec (cancellation-detached — see fill) and
// compiles it, defaulting an empty Salt to the ref.
func (c *Cache) loadCompile(ctx context.Context, ref string) (*Compiled, error) {
	spec, err := c.load(context.WithoutCancel(ctx), ref)
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
