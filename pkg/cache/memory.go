package cache

import (
	"container/list"
	"context"
	"sync"
	"time"
)

// entry holds a cached value with its expiration time and key.
type entry[V any] struct {
	expiresAt time.Time // zero value = never expires
	value     V
	key       string
}

// isExpired reports whether the entry has passed its expiration time.
func (e *entry[V]) isExpired() bool {
	if e.expiresAt.IsZero() {
		return false
	}
	return time.Now().After(e.expiresAt)
}

// Memory is an in-memory cache with TTL-based expiration and optional
// LRU eviction when a maximum entry count is configured.
//
// It uses a hash map for O(1) lookups and a doubly-linked list for O(1)
// LRU eviction ordering. The most recently accessed items are at the
// front of the list; the least recently used are at the back.
type Memory[V any] struct {
	items    map[string]*list.Element
	eviction *list.List
	opts     *memoryOptions
	onEvict  func(key string, value V)
	done     chan struct{}
	mu       sync.Mutex
	closed   bool
}

// NewMemory creates a new in-memory cache.
//
// Example:
//
//	c := cache.NewMemory[string](
//	    cache.WithDefaultTTL(5 * time.Minute),
//	    cache.WithCleanupInterval(30 * time.Second),
//	    cache.WithMaxEntries(10000),
//	)
//	defer c.Close()
func NewMemory[V any](opts ...MemoryOption) *Memory[V] {
	o := defaultMemoryOptions()
	for _, opt := range opts {
		opt(o)
	}

	m := &Memory[V]{
		items:    make(map[string]*list.Element),
		eviction: list.New(),
		opts:     o,
		done:     make(chan struct{}),
	}

	if o.cleanupInterval > 0 {
		go m.janitor()
	}

	return m
}

// SetEvictCallback sets a callback function that is called when items
// are evicted from the cache. This includes LRU eviction, TTL expiration
// cleanup, manual deletion, and clearing.
func (m *Memory[V]) SetEvictCallback(fn func(key string, value V)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onEvict = fn
}

// Get retrieves a value by key.
// Returns ErrNotFound if the key does not exist or has expired.
// Accessing a key marks it as recently used for LRU purposes.
func (m *Memory[V]) Get(_ context.Context, key string) (V, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	elem, ok := m.items[key]
	if !ok {
		var zero V
		return zero, ErrNotFound
	}

	e := elem.Value.(*entry[V])

	if e.isExpired() {
		m.removeElement(elem)
		var zero V
		return zero, ErrNotFound
	}

	// Move to front: mark as recently used.
	m.eviction.MoveToFront(elem)

	return e.value, nil
}

// Set stores a value with the given TTL.
// TTL semantics: positive = expires after duration, zero = use default TTL,
// negative = never expires.
func (m *Memory[V]) Set(_ context.Context, key string, value V, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return ErrClosed
	}

	// Resolve TTL.
	if ttl == 0 {
		ttl = m.opts.defaultTTL
	}

	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}
	// ttl < 0: expiresAt stays zero (never expires)

	// Update existing entry.
	if elem, ok := m.items[key]; ok {
		e := elem.Value.(*entry[V])
		e.value = value
		e.expiresAt = expiresAt
		m.eviction.MoveToFront(elem)
		return nil
	}

	// Evict LRU entry if at capacity.
	if m.opts.maxEntries > 0 && len(m.items) >= m.opts.maxEntries {
		m.evictOldest()
	}

	// Insert new entry at front.
	e := &entry[V]{key: key, value: value, expiresAt: expiresAt}
	elem := m.eviction.PushFront(e)
	m.items[key] = elem

	return nil
}

// Delete removes a key from the cache.
func (m *Memory[V]) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return ErrClosed
	}

	if elem, ok := m.items[key]; ok {
		m.removeElement(elem)
	}

	return nil
}

// Has checks whether a key exists and has not expired.
func (m *Memory[V]) Has(_ context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	elem, ok := m.items[key]
	if !ok {
		return false, nil
	}

	e := elem.Value.(*entry[V])
	if e.isExpired() {
		m.removeElement(elem)
		return false, nil
	}

	return true, nil
}

// Clear removes all entries from the cache.
func (m *Memory[V]) Clear(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return ErrClosed
	}

	if m.onEvict != nil {
		for _, elem := range m.items {
			e := elem.Value.(*entry[V])
			m.onEvict(e.key, e.value)
		}
	}

	m.items = make(map[string]*list.Element)
	m.eviction.Init()

	return nil
}

// Close stops the background janitor goroutine and marks the cache as closed.
// Close is idempotent.
func (m *Memory[V]) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}

	m.closed = true
	close(m.done)

	return nil
}

// janitor periodically removes expired entries.
func (m *Memory[V]) janitor() {
	ticker := time.NewTicker(m.opts.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.done:
			return
		case <-ticker.C:
			m.deleteExpired()
		}
	}
}

// deleteExpired removes all expired entries from back to front.
func (m *Memory[V]) deleteExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for elem := m.eviction.Back(); elem != nil; {
		e := elem.Value.(*entry[V])
		prev := elem.Prev()
		if !e.expiresAt.IsZero() && now.After(e.expiresAt) {
			m.removeElement(elem)
		}
		elem = prev
	}
}

// evictOldest removes the least recently used entry.
// Caller must hold the mutex.
func (m *Memory[V]) evictOldest() {
	elem := m.eviction.Back()
	if elem != nil {
		m.removeElement(elem)
	}
}

// removeElement removes a specific element and triggers the eviction callback.
// Caller must hold the mutex.
func (m *Memory[V]) removeElement(elem *list.Element) {
	m.eviction.Remove(elem)
	e := elem.Value.(*entry[V])
	delete(m.items, e.key)

	if m.onEvict != nil {
		m.onEvict(e.key, e.value)
	}
}

var _ Cache[any] = (*Memory[any])(nil)
