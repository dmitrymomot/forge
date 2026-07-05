package ratelimit

import (
	"context"
	"time"
)

// Store is the windowed atomic-counter seam shared by ratelimit (and, later,
// quota and lockout). It is distinct from cache.Store (byte-KV): counters need
// atomic increment-with-TTL, which Get/Set cannot express race-free.
type Store interface {
	// Incr atomically adds delta to key's counter and returns the new value. If
	// this call creates the key, its TTL is set to ttl; Incr never extends the
	// TTL of an existing (live) key.
	Incr(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error)
	// Get returns the current counter, or 0 if the key is absent or expired.
	Get(ctx context.Context, key string) (int64, error)
	// Reset deletes the counter for key.
	Reset(ctx context.Context, key string) error
	// Close releases resources (e.g. a janitor goroutine).
	Close() error
}
