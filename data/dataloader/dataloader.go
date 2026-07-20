package dataloader

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

// BatchFunc loads values for a set of deduplicated keys in one round trip.
// A key absent from the returned map resolves to ErrNotFound for its caller;
// a non-nil error fails every key in the batch with that error.
type BatchFunc[K comparable, V any] func(ctx context.Context, keys []K) (map[K]V, error)

// thunk is one key's pending or completed result. done is the owning batch's
// channel (shared by every thunk of that batch, since they complete together);
// it is closed exactly once after all the batch's val/err fields are set, so
// waiters read them without a lock.
type thunk[V any] struct {
	done chan struct{}
	val  V
	err  error
}

func (t *thunk[V]) wait(ctx context.Context) (V, error) {
	// Prefer a ready result over a simultaneously-done context.
	select {
	case <-t.done:
		return t.val, t.err
	default:
	}
	select {
	case <-t.done:
		return t.val, t.err
	case <-ctx.Done():
		var zero V
		return zero, ctx.Err()
	}
}

// closedDone is shared by every pre-resolved thunk (Prime) so priming never
// allocates a channel.
var closedDone = func() chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}()

// batch accumulates keys until the wait window elapses or maxBatch is hit.
// keys and thunks are parallel slices; both are mutated only under the
// loader's mutex and only while the batch is current (fired == false).
type batch[K comparable, V any] struct {
	ctx    context.Context
	timer  *time.Timer
	done   chan struct{}
	keys   []K
	thunks []*thunk[V]
	fired  bool
	// cleared records that Clear/ClearAll dropped cache entries while this
	// batch was open, so a re-Load must rejoin the pending thunk instead of
	// appending its key a second time.
	cleared bool
}

// Loader collapses N+1 lookups: concurrent and batched Load calls within the
// wait window are deduplicated and fetched in one BatchFunc call, and every
// resolved key (including its error) is memoized for the loader's lifetime.
// A Loader is safe for concurrent use, but it is a per-request object: create
// one per request (or per tenant scope) so the memoized results never leak
// across requests or tenants.
type Loader[K comparable, V any] struct {
	fetch    BatchFunc[K, V]
	cache    map[K]*thunk[V]
	current  *batch[K, V]
	wait     time.Duration
	maxBatch int
	mu       sync.Mutex
}

// New returns a Loader over fetch. It panics on a nil fetch — a Loader
// without a BatchFunc is a programming error, not a runtime condition.
func New[K comparable, V any](fetch BatchFunc[K, V], opts ...Option) *Loader[K, V] {
	if fetch == nil {
		panic("dataloader: nil BatchFunc")
	}
	c := newConfig(opts...)
	return &Loader[K, V]{
		fetch:    fetch,
		cache:    make(map[K]*thunk[V]),
		wait:     c.wait,
		maxBatch: c.maxBatch,
	}
}

// Load returns the value for key, joining the current batch (or opening one)
// on a cache miss. It blocks until the batch resolves or ctx ends; a caller
// abandoning the wait does not abort the batch, and the result is still
// cached for later calls.
func (l *Loader[K, V]) Load(ctx context.Context, key K) (V, error) {
	if l == nil || l.fetch == nil {
		var zero V
		return zero, errNotConstructed
	}
	l.mu.Lock()
	t := l.scheduleLocked(ctx, key)
	l.mu.Unlock()
	return t.wait(ctx)
}

// LoadMany resolves all keys (duplicates collapse) and returns the values
// found. Keys the BatchFunc did not return are simply absent from the result
// map — absence is the not-found signal. Any other per-batch errors are
// deduplicated and joined into the returned error alongside the partial
// results. All keys are scheduled before waiting, so a sequential loop's
// lookups still land in shared batches.
func (l *Loader[K, V]) LoadMany(ctx context.Context, keys []K) (map[K]V, error) {
	if l == nil || l.fetch == nil {
		return nil, errNotConstructed
	}
	thunks := make(map[K]*thunk[V], len(keys))
	l.mu.Lock()
	for _, key := range keys {
		if _, ok := thunks[key]; ok {
			continue
		}
		thunks[key] = l.scheduleLocked(ctx, key)
	}
	l.mu.Unlock()

	out := make(map[K]V, len(thunks))
	var errs []error
	for key, t := range thunks {
		v, err := t.wait(ctx)
		if err == nil {
			out[key] = v
			continue
		}
		// Loader-generated absence (exact type, set only by run): the missing
		// map entry is the signal. A caller batch error that merely wraps
		// ErrNotFound does not match and still joins the returned error.
		if _, absent := err.(*notFoundError); absent {
			continue
		}
		// A whole-batch failure repeats the same error instance across its
		// keys; errors.Is-based dedup keeps the join readable and is safe
		// even for uncomparable error types.
		if !slices.ContainsFunc(errs, func(e error) bool { return errors.Is(e, err) }) {
			errs = append(errs, err)
		}
	}
	return out, errors.Join(errs...)
}

// Prime seeds key with value so later Loads skip the BatchFunc. It reports
// whether the value was stored: an already cached or in-flight key is left
// untouched (Clear first to overwrite).
func (l *Loader[K, V]) Prime(key K, value V) bool {
	if l == nil || l.fetch == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.cache[key]; ok {
		return false
	}
	l.cache[key] = &thunk[V]{done: closedDone, val: value}
	return true
}

// Clear drops key from the cache so the next Load refetches it. A key whose
// fetch has not started yet (still in the open batch) rejoins that pending
// fetch instead of being fetched twice; a fetch already in flight completes
// and delivers to its current waiters.
func (l *Loader[K, V]) Clear(key K) {
	if l == nil {
		return
	}
	l.mu.Lock()
	delete(l.cache, key)
	if l.current != nil {
		l.current.cleared = true
	}
	l.mu.Unlock()
}

// ClearAll drops every cached and pending entry; in-flight fetches still
// deliver to their current waiters.
func (l *Loader[K, V]) ClearAll() {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.cache = make(map[K]*thunk[V])
	if l.current != nil {
		l.current.cleared = true
	}
	l.mu.Unlock()
}

// scheduleLocked returns the thunk for key, creating it and appending the key
// to the current batch on a miss. The caller holds l.mu.
func (l *Loader[K, V]) scheduleLocked(ctx context.Context, key K) *thunk[V] {
	if t, ok := l.cache[key]; ok {
		return t
	}
	b := l.current
	if b != nil && b.cleared {
		// Clear dropped the cache entry but the key may still sit in the open
		// batch; rejoin its pending thunk so the BatchFunc never sees the same
		// key twice.
		if i := slices.Index(b.keys, key); i >= 0 {
			t := b.thunks[i]
			l.cache[key] = t
			return t
		}
	}
	created := b == nil
	if created {
		// The batch runs under the opening caller's context values but
		// detached from its cancellation (singleflight precedent): one caller
		// abandoning must not fail the shared fetch. The BatchFunc owns its
		// own timeout.
		b = &batch[K, V]{ctx: context.WithoutCancel(ctx), done: make(chan struct{})}
		l.current = b
	}
	t := &thunk[V]{done: b.done}
	l.cache[key] = t
	b.keys = append(b.keys, key)
	b.thunks = append(b.thunks, t)
	switch {
	case l.maxBatch > 0 && len(b.keys) >= l.maxBatch:
		b.fired = true
		if b.timer != nil {
			b.timer.Stop()
		}
		l.current = nil
		go l.run(b)
	case created:
		// Armed after the size check so a batch that fills instantly
		// (maxBatch 1) never allocates a timer.
		b.timer = time.AfterFunc(l.wait, func() { l.fire(b) })
	}
	return t
}

// fire is the timer path: detach the batch under the lock, then fetch. The
// fired flag makes timer expiry and the full-batch path race-safe.
func (l *Loader[K, V]) fire(b *batch[K, V]) {
	l.mu.Lock()
	if b.fired {
		l.mu.Unlock()
		return
	}
	b.fired = true
	if l.current == b {
		l.current = nil
	}
	l.mu.Unlock()
	l.run(b)
}

// run executes the fetch for a detached batch and resolves its thunks. b is
// no longer reachable from the loader, so its slices are read without a lock.
func (l *Loader[K, V]) run(b *batch[K, V]) {
	results, err := l.safeFetch(b.ctx, b.keys)
	for i, key := range b.keys {
		t := b.thunks[i]
		if err != nil {
			t.err = err
		} else if v, ok := results[key]; ok {
			t.val = v
		} else {
			t.err = &notFoundError{err: fmt.Errorf("%w: key %v", ErrNotFound, key)}
		}
	}
	close(b.done) // after every thunk is resolved — waiters read without a lock
}

// safeFetch converts a BatchFunc panic into an error so waiters are released
// instead of deadlocking.
func (l *Loader[K, V]) safeFetch(ctx context.Context, keys []K) (results map[K]V, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %v", ErrFetchPanic, r)
		}
	}()
	return l.fetch(ctx, keys)
}
