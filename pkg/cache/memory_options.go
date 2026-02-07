package cache

import "time"

// MemoryOption configures the in-memory cache.
type MemoryOption func(*memoryOptions)

type memoryOptions struct {
	defaultTTL      time.Duration
	cleanupInterval time.Duration
	maxEntries      int
}

func defaultMemoryOptions() *memoryOptions {
	return &memoryOptions{
		defaultTTL:      time.Hour,
		cleanupInterval: time.Minute,
		maxEntries:      0, // 0 = unlimited
	}
}

// WithDefaultTTL sets the default expiration for cache entries when
// Set is called with a zero TTL.
// Default: 1 hour.
func WithDefaultTTL(d time.Duration) MemoryOption {
	return func(o *memoryOptions) {
		o.defaultTTL = d
	}
}

// WithCleanupInterval sets how often expired entries are removed
// by the background janitor goroutine.
// Default: 1 minute.
func WithCleanupInterval(d time.Duration) MemoryOption {
	return func(o *memoryOptions) {
		o.cleanupInterval = d
	}
}

// WithMaxEntries sets the maximum number of entries in the cache.
// When the limit is reached, the least recently used entry is evicted.
// Zero means unlimited.
// Default: 0 (unlimited).
func WithMaxEntries(n int) MemoryOption {
	return func(o *memoryOptions) {
		o.maxEntries = n
	}
}
