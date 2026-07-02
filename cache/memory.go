package cache

import (
	"container/list"
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/clock"
)

type memoryConfig struct {
	clk        clock.Clock
	maxEntries int
	cleanup    time.Duration
}

// MemoryOption configures NewMemoryStore.
type MemoryOption func(*memoryConfig)

// WithMaxEntries caps entries; exceeding it evicts the least-recently-used.
// 0 (default) is unbounded.
func WithMaxEntries(n int) MemoryOption { return func(c *memoryConfig) { c.maxEntries = n } }

// WithCleanupInterval starts a janitor goroutine that sweeps expired entries
// every d; Close stops it. 0 (default) means lazy expiry only.
func WithCleanupInterval(d time.Duration) MemoryOption {
	return func(c *memoryConfig) { c.cleanup = d }
}

// WithClock injects the time source (default clock.System()).
func WithClock(clk clock.Clock) MemoryOption {
	return func(c *memoryConfig) {
		if clk != nil {
			c.clk = clk
		}
	}
}

type memEntry struct {
	expires time.Time // zero = never
	elem    *list.Element
	key     string
	val     []byte
}

type memoryStore struct {
	items  map[string]*memEntry
	lru    *list.List
	stop   chan struct{}
	cfg    memoryConfig
	mu     sync.Mutex
	closed bool
}

// NewMemoryStore returns an in-process Store backed by a map with LRU eviction
// and TTL expiry. It is a standalone instance: call Close to release it.
func NewMemoryStore(opts ...MemoryOption) Store {
	c := memoryConfig{clk: clock.System()}
	for _, o := range opts {
		o(&c)
	}
	m := &memoryStore{cfg: c, items: make(map[string]*memEntry), lru: list.New()}
	if c.cleanup > 0 {
		m.stop = make(chan struct{})
		go m.janitor()
	}
	return m
}

func (m *memoryStore) janitor() {
	t := time.NewTicker(m.cfg.cleanup)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-t.C:
			m.sweep()
		}
	}
}

func (m *memoryStore) sweep() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.cfg.clk.Now()
	for k, e := range m.items {
		if !e.expires.IsZero() && now.After(e.expires) {
			m.removeLocked(k, e)
		}
	}
}

func (m *memoryStore) removeLocked(k string, e *memEntry) {
	delete(m.items, k)
	m.lru.Remove(e.elem)
}

func (m *memoryStore) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrClosed
	}
	e, ok := m.items[key]
	if !ok {
		return nil, ErrNotFound
	}
	if !e.expires.IsZero() && m.cfg.clk.Now().After(e.expires) {
		m.removeLocked(key, e)
		return nil, ErrNotFound
	}
	m.lru.MoveToFront(e.elem)
	out := make([]byte, len(e.val))
	copy(out, e.val)
	return out, nil
}

func (m *memoryStore) Set(_ context.Context, key string, val []byte, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	var expires time.Time
	if ttl > 0 {
		expires = m.cfg.clk.Now().Add(ttl)
	}
	stored := make([]byte, len(val))
	copy(stored, val)
	if e, ok := m.items[key]; ok {
		e.val = stored
		e.expires = expires
		m.lru.MoveToFront(e.elem)
		return nil
	}
	e := &memEntry{key: key, val: stored, expires: expires}
	e.elem = m.lru.PushFront(e)
	m.items[key] = e
	if m.cfg.maxEntries > 0 && m.lru.Len() > m.cfg.maxEntries {
		if back := m.lru.Back(); back != nil {
			old := back.Value.(*memEntry)
			m.removeLocked(old.key, old)
		}
	}
	return nil
}

func (m *memoryStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	if e, ok := m.items[key]; ok {
		m.removeLocked(key, e)
	}
	return nil
}

func (m *memoryStore) Has(ctx context.Context, key string) (bool, error) {
	_, err := m.Get(ctx, key)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, ErrNotFound):
		return false, nil
	default:
		return false, err
	}
}

func (m *memoryStore) DeletePrefix(_ context.Context, prefix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	for k, e := range m.items {
		if strings.HasPrefix(k, prefix) {
			m.removeLocked(k, e)
		}
	}
	return nil
}

func (m *memoryStore) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	if m.stop != nil {
		close(m.stop)
	}
	m.items = nil
	m.lru.Init()
	return nil
}
