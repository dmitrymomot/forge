package cache

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"golang.org/x/sync/singleflight"
)

// Cache is a generic key-value cache with TTL support.
//
// TTL semantics for Set:
//   - Positive duration: item expires after this duration
//   - Zero: use the cache's configured default TTL
//   - Negative: item never expires
type Cache[V any] interface {
	// Get retrieves a value by key.
	// Returns ErrNotFound if the key does not exist or has expired.
	Get(ctx context.Context, key string) (V, error)

	// Set stores a value with the given TTL.
	Set(ctx context.Context, key string, value V, ttl time.Duration) error

	// Delete removes a key from the cache.
	Delete(ctx context.Context, key string) error

	// Has checks whether a key exists and has not expired.
	Has(ctx context.Context, key string) (bool, error)

	// Clear removes all entries from the cache.
	Clear(ctx context.Context) error

	// Close releases resources (stops background goroutines, etc.).
	Close() error
}

// Marshaler serializes and deserializes cache values for storage backends
// that require byte representation (e.g., Redis).
type Marshaler[V any] interface {
	Marshal(v V) ([]byte, error)
	Unmarshal(data []byte) (V, error)
}

type jsonMarshaler[V any] struct{}

func (jsonMarshaler[V]) Marshal(v V) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, errors.Join(ErrMarshal, err)
	}
	return data, nil
}

func (jsonMarshaler[V]) Unmarshal(data []byte) (V, error) {
	var v V
	if err := json.Unmarshal(data, &v); err != nil {
		return v, errors.Join(ErrUnmarshal, err)
	}
	return v, nil
}

var sfGroup singleflight.Group

type getOrSetResult[V any] struct {
	val V
	ttl time.Duration
}

// GetOrSet retrieves a value from the cache, or calls fn to compute it on a miss.
// Uses singleflight to prevent cache stampedes: if multiple goroutines call
// GetOrSet with the same key concurrently, fn is called only once.
//
// The callback returns the value, a TTL for caching, and an error.
// If fn returns an error, the value is not cached and the error is returned.
func GetOrSet[V any](ctx context.Context, c Cache[V], key string, fn func(ctx context.Context) (V, time.Duration, error)) (V, error) {
	// Fast path: try cache first.
	if v, err := c.Get(ctx, key); err == nil {
		return v, nil
	}

	// Slow path: use singleflight to deduplicate concurrent misses.
	v, err, _ := sfGroup.Do(key, func() (any, error) {
		val, ttl, err := fn(ctx)
		if err != nil {
			return nil, err
		}
		return getOrSetResult[V]{val: val, ttl: ttl}, nil
	})
	if err != nil {
		var zero V
		return zero, err
	}

	r := v.(getOrSetResult[V])

	// Best-effort cache the result.
	_ = c.Set(ctx, key, r.val, r.ttl)

	return r.val, nil
}
