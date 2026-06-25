package cache

import (
	"container/list"
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// MemoryConfig configures the in-memory cache.
type MemoryConfig struct {
	DefaultTTL      time.Duration `env:"DEFAULT_TTL"      envDefault:"5m"`
	CleanupInterval time.Duration `env:"CLEANUP_INTERVAL" envDefault:"1m"`
	MaxEntries      int           `env:"MAX_ENTRIES"      envDefault:"0"`
}

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
	sf       singleflight.Group
	items    map[string]*list.Element
	eviction *list.List
	onEvict  func(key string, value V)
	done     chan struct{}
	cfg      MemoryConfig
	mu       sync.Mutex
	closed   bool
}

// NewMemory creates a new in-memory cache.
//
// Example:
//
//	c := cache.NewMemory[string](cache.MemoryConfig{
//	    DefaultTTL:      5 * time.Minute,
//	    CleanupInterval: 30 * time.Second,
//	    MaxEntries:      10000,
//	})
//	defer c.Close()
func NewMemory[V any](cfg MemoryConfig) *Memory[V] {
	if cfg.DefaultTTL == 0 {
		cfg.DefaultTTL = 5 * time.Minute
	}
	if cfg.CleanupInterval == 0 {
		cfg.CleanupInterval = time.Minute
	}

	m := &Memory[V]{
		items:    make(map[string]*list.Element),
		eviction: list.New(),
		cfg:      cfg,
		done:     make(chan struct{}),
	}

	if cfg.CleanupInterval > 0 {
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

	if m.closed {
		m.mu.Unlock()
		var zero V
		return zero, ErrClosed
	}

	elem, ok := m.items[key]
	if !ok {
		m.mu.Unlock()
		var zero V
		return zero, ErrNotFound
	}

	e := elem.Value.(*entry[V])

	if e.isExpired() {
		evicted := m.removeElement(elem)
		fn := m.onEvict
		m.mu.Unlock()
		m.fireEvict(fn, evicted)
		var zero V
		return zero, ErrNotFound
	}

	// Move to front: mark as recently used.
	m.eviction.MoveToFront(elem)
	value := e.value

	m.mu.Unlock()
	return value, nil
}

// Set stores a value with the given TTL.
// TTL semantics: positive = expires after duration, zero = use default TTL,
// negative = never expires.
func (m *Memory[V]) Set(_ context.Context, key string, value V, ttl time.Duration) error {
	m.mu.Lock()

	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}

	// Resolve TTL.
	if ttl == 0 {
		ttl = m.cfg.DefaultTTL
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
		m.mu.Unlock()
		return nil
	}

	// Evict LRU entry if at capacity.
	var evicted *entry[V]
	if m.cfg.MaxEntries > 0 && len(m.items) >= m.cfg.MaxEntries {
		evicted = m.evictOldest()
	}

	// Insert new entry at front.
	e := &entry[V]{key: key, value: value, expiresAt: expiresAt}
	elem := m.eviction.PushFront(e)
	m.items[key] = elem

	fn := m.onEvict
	m.mu.Unlock()
	m.fireEvict(fn, evicted)
	return nil
}

// Delete removes a key from the cache.
func (m *Memory[V]) Delete(_ context.Context, key string) error {
	m.mu.Lock()

	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}

	var evicted *entry[V]
	if elem, ok := m.items[key]; ok {
		evicted = m.removeElement(elem)
	}

	fn := m.onEvict
	m.mu.Unlock()
	m.fireEvict(fn, evicted)
	return nil
}

// Has checks whether a key exists and has not expired.
func (m *Memory[V]) Has(_ context.Context, key string) (bool, error) {
	m.mu.Lock()

	if m.closed {
		m.mu.Unlock()
		return false, ErrClosed
	}

	elem, ok := m.items[key]
	if !ok {
		m.mu.Unlock()
		return false, nil
	}

	e := elem.Value.(*entry[V])
	if e.isExpired() {
		evicted := m.removeElement(elem)
		fn := m.onEvict
		m.mu.Unlock()
		m.fireEvict(fn, evicted)
		return false, nil
	}

	m.mu.Unlock()
	return true, nil
}

// Clear removes all entries from the cache.
func (m *Memory[V]) Clear(_ context.Context) error {
	m.mu.Lock()

	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}

	// Collect evicted entries and snapshot the callback under the lock; fire
	// callbacks after releasing it.
	fn := m.onEvict
	var evicted []*entry[V]
	if fn != nil {
		evicted = make([]*entry[V], 0, len(m.items))
		for _, elem := range m.items {
			evicted = append(evicted, elem.Value.(*entry[V]))
		}
	}

	m.items = make(map[string]*list.Element)
	m.eviction.Init()

	m.mu.Unlock()

	for _, e := range evicted {
		m.fireEvict(fn, e)
	}
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
	ticker := time.NewTicker(m.cfg.CleanupInterval)
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

	var evicted []*entry[V]
	now := time.Now()
	for elem := m.eviction.Back(); elem != nil; {
		e := elem.Value.(*entry[V])
		prev := elem.Prev()
		if !e.expiresAt.IsZero() && now.After(e.expiresAt) {
			if removed := m.removeElement(elem); removed != nil {
				evicted = append(evicted, removed)
			}
		}
		elem = prev
	}

	fn := m.onEvict
	m.mu.Unlock()

	for _, e := range evicted {
		m.fireEvict(fn, e)
	}
}

// evictOldest removes the least recently used entry and returns it (or nil
// if the cache is empty) so the caller can fire the eviction callback after
// releasing the mutex. Caller must hold the mutex.
func (m *Memory[V]) evictOldest() *entry[V] {
	elem := m.eviction.Back()
	if elem != nil {
		return m.removeElement(elem)
	}
	return nil
}

// removeElement removes a specific element from the map and eviction list and
// returns its entry so the caller can fire the eviction callback after
// releasing the mutex. The callback is intentionally NOT invoked here to avoid
// running user code while holding the cache mutex. Caller must hold the mutex.
func (m *Memory[V]) removeElement(elem *list.Element) *entry[V] {
	m.eviction.Remove(elem)
	e := elem.Value.(*entry[V])
	delete(m.items, e.key)
	return e
}

// fireEvict invokes the supplied eviction callback for a removed entry. It must
// be called WITHOUT holding the mutex so user callbacks cannot deadlock or block
// other cache operations. The callback fn is snapshotted by the caller while it
// already holds m.mu (so no second lock cycle is needed here). A nil entry or
// nil callback is a no-op.
func (m *Memory[V]) fireEvict(fn func(key string, value V), e *entry[V]) {
	if e == nil || fn == nil {
		return
	}
	fn(e.key, e.value)
}

func (m *Memory[V]) sfDo(key string, fn func() (any, error)) (any, error) {
	v, err, _ := m.sf.Do(key, fn)
	return v, err
}

var _ Cache[any] = (*Memory[any])(nil)
