package ratelimit

import (
	"context"
	"hash/maphash"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

type memoryConfig struct {
	clk             clock.Clock
	janitorInterval time.Duration
	shards          int
}

// MemoryOption configures NewMemoryStore.
type MemoryOption func(*memoryConfig)

// WithMemoryClock injects a clock (for tests). Default clock.System().
func WithMemoryClock(clk clock.Clock) MemoryOption {
	return func(c *memoryConfig) {
		if clk != nil {
			c.clk = clk
		}
	}
}

// WithMemoryJanitor starts a background goroutine that periodically deletes
// expired entries, so the map does not grow unbounded under high-cardinality
// (e.g. per-IP) keys. It ticks every interval; expiry is still decided by the
// store's clock. Default (option not passed, or interval <= 0) is no janitor:
// Close remains a no-op and the map is only pruned lazily by Get/Incr.
func WithMemoryJanitor(interval time.Duration) MemoryOption {
	return func(c *memoryConfig) {
		c.janitorInterval = interval
	}
}

// WithShards splits the store into n independently-locked shards, keyed by a
// hash of the key. This lifts the single-mutex throughput ceiling under high
// concurrency and bounds each janitor sweep to one shard at a time, so a large
// map no longer blocks every request for the duration of a full sweep. Default
// 1 — a single lock, identical to the unsharded behavior; values < 1 are
// ignored. The single-lock default already sustains well over a million
// ops/sec, so only very high throughput or high-cardinality-with-janitor
// workloads benefit; a small multiple of GOMAXPROCS (e.g. 16–64) is ample.
func WithShards(n int) MemoryOption {
	return func(c *memoryConfig) {
		if n >= 1 {
			c.shards = n
		}
	}
}

type counter struct {
	expiresAt time.Time
	val       int64
}

// memShard is one independently-locked partition of the store's keyspace.
type memShard struct {
	m  map[string]counter
	mu sync.Mutex
}

type memoryStore struct {
	clk    clock.Clock
	stop   chan struct{}
	shards []memShard
	wg     sync.WaitGroup

	seed maphash.Seed

	closeOnce sync.Once
}

// NewMemoryStore returns an in-process counter Store. Lifecycle is the caller's
// (Close). Suitable for single-instance use and tests; multi-instance limiting
// needs a Store backed by shared state such as Redis.
func NewMemoryStore(opts ...MemoryOption) Store {
	c := memoryConfig{clk: clock.System(), shards: 1}
	for _, o := range opts {
		o(&c)
	}
	s := &memoryStore{
		clk:    c.clk,
		shards: make([]memShard, c.shards),
		seed:   maphash.MakeSeed(),
	}
	for i := range s.shards {
		s.shards[i].m = make(map[string]counter)
	}
	if c.janitorInterval > 0 {
		s.stop = make(chan struct{})
		s.startJanitor(c.janitorInterval)
	}
	return s
}

// shardFor returns the shard owning key. With a single shard the hash is
// skipped so the common (unsharded) path stays hash-free.
func (s *memoryStore) shardFor(key string) *memShard {
	if len(s.shards) == 1 {
		return &s.shards[0]
	}
	h := maphash.String(s.seed, key)
	return &s.shards[h%uint64(len(s.shards))]
}

// startJanitor runs the eviction sweep every interval until stop is closed.
func (s *memoryStore) startJanitor(interval time.Duration) {
	ticker := time.NewTicker(interval)
	s.wg.Go(func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.sweep()
			case <-s.stop:
				return
			}
		}
	})
}

// sweep deletes entries expired as of the store's clock, locking one shard at a
// time so a large map never blocks all requests for a full O(n) scan.
func (s *memoryStore) sweep() {
	now := s.clk.Now()
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.Lock()
		for key, e := range sh.m {
			if expired(e.expiresAt, now) {
				delete(sh.m, key)
			}
		}
		sh.mu.Unlock()
	}
}

// Len returns the number of entries currently held across all shards, including
// any not yet evicted by lazy expiry or the janitor. Useful for metrics and for
// tests asserting physical eviction.
func (s *memoryStore) Len() int {
	n := 0
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.Lock()
		n += len(sh.m)
		sh.mu.Unlock()
	}
	return n
}

func (s *memoryStore) Incr(_ context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	now := s.clk.Now()
	sh := s.shardFor(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e, ok := sh.m[key]
	if !ok || expired(e.expiresAt, now) {
		e = counter{val: delta, expiresAt: expiryAt(now, ttl)}
	} else {
		e.val += delta
	}
	sh.m[key] = e
	return e.val, nil
}

func (s *memoryStore) Get(_ context.Context, key string) (int64, error) {
	now := s.clk.Now()
	sh := s.shardFor(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e, ok := sh.m[key]
	if !ok || expired(e.expiresAt, now) {
		return 0, nil
	}
	return e.val, nil
}

func (s *memoryStore) Reset(_ context.Context, key string) error {
	sh := s.shardFor(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	delete(sh.m, key)
	return nil
}

// Close stops the janitor goroutine, if one was started, and waits for it to
// exit. Idempotent and safe to call even when no janitor was started.
func (s *memoryStore) Close() error {
	s.closeOnce.Do(func() {
		if s.stop != nil {
			close(s.stop)
		}
	})
	s.wg.Wait()
	return nil
}

// expiryAt returns the absolute expiry for a new counter. A ttl <= 0 yields the
// zero Time, the sentinel for "no expiry".
func expiryAt(now time.Time, ttl time.Duration) time.Time {
	if ttl <= 0 {
		return time.Time{}
	}
	return now.Add(ttl)
}

// expired reports whether a counter with expiry exp is expired as of now. The
// zero Time means no expiry (never expired).
func expired(exp, now time.Time) bool {
	return !exp.IsZero() && now.After(exp)
}
