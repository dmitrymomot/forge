package smartlink

import (
	"context"
	"fmt"
	"sync"
)

// Resolver resolves a Ref-backed Link to a Decider ready for a click decision.
// [Cache.Resolver] returns a ready-made one; a Manager (a later task) is the
// seam's consumer. Resolver does not special-case ErrNoTarget: when the
// consumer's load func returns it for a paused or deleted offer, Resolver
// propagates the wrapped error and leaves the mapping to dead-link handling
// to the caller.
type Resolver func(ctx context.Context, l Link) (Decider, error)

// Cache is a lazy compile cache for Ref-backed Links. Offers live in the
// consumer's own database as [Spec] values; Cache loads and compiles them on
// demand, keyed by ref string, and the consumer invalidates an entry from its
// offer-save path. Safe for concurrent use.
type Cache struct {
	load    func(ctx context.Context, ref string) (Spec, error)
	entries map[string]*Compiled
	// gens counts Invalidate calls per ref. Get stores a loaded result only
	// when the generation it observed on the miss is still current, so a
	// stale in-flight load can never overwrite a post-Invalidate entry.
	gens map[string]uint64
	opts []Option

	mu sync.RWMutex
}

// NewCache returns a Cache that loads a Spec via load and compiles it with
// compileOpts on a cache miss.
//
// Get takes an RLock fast path; on a miss it calls load and Compile outside
// the write lock, then stores the result under a short write lock. Two
// goroutines that miss on the same ref concurrently may both load and
// compile — the redundant work is benign because Compiled values for the
// same Spec are interchangeable, so the second store just overwrites the
// first with an equivalent result. An Invalidate concurrent with an
// in-flight load bumps the ref's generation, so the straggler's stale
// result is discarded instead of stored — it can never overwrite an entry
// cached after the Invalidate. Errors from load or Compile are never
// cached, so the next Get retries.
func NewCache(load func(ctx context.Context, ref string) (Spec, error), compileOpts ...Option) *Cache {
	return &Cache{
		load:    load,
		opts:    compileOpts,
		entries: make(map[string]*Compiled),
		gens:    make(map[string]uint64),
	}
}

// Get returns the Compiled engine for ref, loading and compiling it on a
// cache miss. Errors from load or Compile are wrapped, not cached.
func (c *Cache) Get(ctx context.Context, ref string) (*Compiled, error) {
	c.mu.RLock()
	compiled, ok := c.entries[ref]
	gen := c.gens[ref]
	c.mu.RUnlock()
	if ok {
		return compiled, nil
	}

	spec, err := c.load(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("smartlink: load ref %q: %w", ref, err)
	}
	compiled, err = Compile(spec, c.opts...)
	if err != nil {
		return nil, fmt.Errorf("smartlink: compile ref %q: %w", ref, err)
	}

	c.mu.Lock()
	if c.gens[ref] == gen {
		c.entries[ref] = compiled
	}
	// Otherwise an Invalidate landed while this load was in flight: discard
	// the stale result from the cache but still return it — it was correct
	// when this call began, and the next Get reloads fresh.
	c.mu.Unlock()
	return compiled, nil
}

// Invalidate evicts ref's cached entry, if any, so the next Get reloads and
// recompiles it. Call this from the consumer's offer-save path.
func (c *Cache) Invalidate(ref string) {
	c.mu.Lock()
	delete(c.entries, ref)
	c.gens[ref]++
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
