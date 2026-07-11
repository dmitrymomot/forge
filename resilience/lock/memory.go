package lock

import (
	"context"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

type memLease struct {
	expiresAt time.Time
	owner     string
	fence     uint64
}

// MemoryStore is an in-process lease Store for single-node use and tests.
type MemoryStore struct {
	clk   clock.Clock
	m     map[string]memLease
	mu    sync.Mutex
	fence uint64
}

type memoryConfig struct{ clk clock.Clock }

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

// NewMemoryStore returns an in-process lease Store.
func NewMemoryStore(opts ...MemoryOption) *MemoryStore {
	c := memoryConfig{clk: clock.System()}
	for _, o := range opts {
		o(&c)
	}
	return &MemoryStore{clk: c.clk, m: make(map[string]memLease)}
}

// Acquire implements Store.
func (s *MemoryStore) Acquire(_ context.Context, key, owner string, ttl time.Duration) (uint64, bool, error) {
	now := s.clk.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if l, ok := s.m[key]; ok && l.owner != owner && now.Before(l.expiresAt) {
		return 0, false, nil // held by another live owner
	}
	s.fence++
	s.m[key] = memLease{owner: owner, expiresAt: now.Add(ttl), fence: s.fence}
	return s.fence, true, nil
}

// Refresh implements Store.
func (s *MemoryStore) Refresh(_ context.Context, key, owner string, ttl time.Duration) (bool, error) {
	now := s.clk.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.m[key]
	if !ok || l.owner != owner || !now.Before(l.expiresAt) {
		return false, nil
	}
	l.expiresAt = now.Add(ttl)
	s.m[key] = l
	return true, nil
}

// Release implements Store.
func (s *MemoryStore) Release(_ context.Context, key, owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l, ok := s.m[key]; ok && l.owner == owner {
		delete(s.m, key)
	}
	return nil
}
