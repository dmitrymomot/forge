package ratelimit

import (
	"context"
	"strconv"
	"sync"
	"time"
)

// MemoryConfig configures the in-memory counter.
type MemoryConfig struct {
	// CleanupInterval controls how often expired windows are removed.
	// Zero uses the default (1 minute). Negative disables cleanup.
	CleanupInterval time.Duration `env:"CLEANUP_INTERVAL" envDefault:"1m"`
}

// memWindow holds a count and its expiration time.
type memWindow struct {
	expiresAt time.Time
	count     int64
}

// MemoryCounter is an in-memory Counter implementation with background
// cleanup of expired windows. Suitable for single-process deployments.
//
// Example:
//
//	counter := ratelimit.NewMemoryCounter(ratelimit.MemoryConfig{
//	    CleanupInterval: 30 * time.Second,
//	})
//	defer counter.Close()
type MemoryCounter struct {
	windows map[string]*memWindow
	done    chan struct{}
	mu      sync.Mutex
	closed  bool
}

// NewMemoryCounter creates a new in-memory counter.
//
// The cleanup goroutine runs at the configured interval to remove expired
// window data. Set CleanupInterval to a negative value to disable cleanup.
func NewMemoryCounter(cfg MemoryConfig) *MemoryCounter {
	if cfg.CleanupInterval == 0 {
		cfg.CleanupInterval = time.Minute
	}

	m := &MemoryCounter{
		windows: make(map[string]*memWindow),
		done:    make(chan struct{}),
	}

	if cfg.CleanupInterval > 0 {
		go m.janitor(cfg.CleanupInterval)
	}

	return m
}

// Increment atomically adds n to the count for the given key and window.
func (m *MemoryCounter) Increment(_ context.Context, key string, window time.Time, ttl time.Duration, n int64) (int64, error) {
	skey := storageKey(key, window)

	m.mu.Lock()
	defer m.mu.Unlock()

	w, ok := m.windows[skey]
	if !ok || time.Now().After(w.expiresAt) {
		w = &memWindow{expiresAt: time.Now().Add(ttl)}
		m.windows[skey] = w
	}
	w.count += n
	return w.count, nil
}

// Get returns the current count for the given key and window.
// Returns 0 if the window has no data or has expired.
func (m *MemoryCounter) Get(_ context.Context, key string, window time.Time) (int64, error) {
	skey := storageKey(key, window)

	m.mu.Lock()
	defer m.mu.Unlock()

	w, ok := m.windows[skey]
	if !ok || time.Now().After(w.expiresAt) {
		return 0, nil
	}
	return w.count, nil
}

// Close stops the background janitor goroutine and marks the counter as closed.
// Close is idempotent.
func (m *MemoryCounter) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}
	m.closed = true
	close(m.done)
	return nil
}

// storageKey formats a composite key from the user key and window timestamp.
func storageKey(key string, window time.Time) string {
	return key + ":" + strconv.FormatInt(window.Unix(), 10)
}

// janitor periodically removes expired windows.
func (m *MemoryCounter) janitor(interval time.Duration) {
	ticker := time.NewTicker(interval)
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

// deleteExpired removes all expired window entries.
func (m *MemoryCounter) deleteExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for k, w := range m.windows {
		if now.After(w.expiresAt) {
			delete(m.windows, k)
		}
	}
}

var _ Counter = (*MemoryCounter)(nil)
