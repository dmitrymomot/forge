package cache

import "errors"

// Sentinel errors for cache operations.
var (
	// ErrNotFound is returned when a key does not exist in the cache or has expired.
	ErrNotFound = errors.New("cache: entry not found")

	// ErrClosed is returned when an operation is attempted on a closed cache.
	ErrClosed = errors.New("cache: closed")

	// ErrMarshal is returned when value serialization fails.
	ErrMarshal = errors.New("cache: failed to marshal value")

	// ErrUnmarshal is returned when value deserialization fails.
	ErrUnmarshal = errors.New("cache: failed to unmarshal value")
)
