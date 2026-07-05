package ratelimit

import (
	"context"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

type memoryConfig struct {
	clk clock.Clock
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

type counter struct {
	expiresAt time.Time
	val       int64
}

type memoryStore struct {
	clk clock.Clock
	m   map[string]counter
	mu  sync.Mutex
}

// NewMemoryStore returns an in-process counter Store. Lifecycle is the caller's
// (Close). Suitable for single-instance use and tests; multi-instance limiting
// needs ratelimit/redisstore.
func NewMemoryStore(opts ...MemoryOption) Store {
	c := memoryConfig{clk: clock.System()}
	for _, o := range opts {
		o(&c)
	}
	return &memoryStore{m: make(map[string]counter), clk: c.clk}
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

func (s *memoryStore) Close() error { return nil }
