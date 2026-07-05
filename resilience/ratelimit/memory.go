package ratelimit

import (
	"context"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

type memoryConfig struct {
	clk             clock.Clock
	janitorInterval time.Duration
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

type counter struct {
	expiresAt time.Time
	val       int64
}

type memoryStore struct {
	clk  clock.Clock
	m    map[string]counter
	stop chan struct{}
	wg   sync.WaitGroup

	closeOnce sync.Once
	mu        sync.Mutex
}

// NewMemoryStore returns an in-process counter Store. Lifecycle is the caller's
// (Close). Suitable for single-instance use and tests; multi-instance limiting
// needs ratelimit/redisstore.
func NewMemoryStore(opts ...MemoryOption) Store {
	c := memoryConfig{clk: clock.System()}
	for _, o := range opts {
		o(&c)
	}
	s := &memoryStore{m: make(map[string]counter), clk: c.clk}
	if c.janitorInterval > 0 {
		s.stop = make(chan struct{})
		s.startJanitor(c.janitorInterval)
	}
	return s
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

// sweep deletes entries expired as of the store's clock.
func (s *memoryStore) sweep() {
	now := s.clk.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, e := range s.m {
		if now.After(e.expiresAt) {
			delete(s.m, key)
		}
	}
}

// Len returns the number of entries currently held, including any not yet
// evicted by lazy expiry or the janitor. Useful for metrics/debugging and for
// tests asserting physical eviction.
func (s *memoryStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.m)
}

func (s *memoryStore) Incr(_ context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	now := s.clk.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[key]
	if !ok || now.After(e.expiresAt) {
		e = counter{val: delta, expiresAt: now.Add(ttl)}
	} else {
		e.val += delta
	}
	s.m[key] = e
	return e.val, nil
}

func (s *memoryStore) Get(_ context.Context, key string) (int64, error) {
	now := s.clk.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[key]
	if !ok || now.After(e.expiresAt) {
		return 0, nil
	}
	return e.val, nil
}

func (s *memoryStore) Reset(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
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
