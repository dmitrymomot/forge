package cache

import "errors"

// Sentinel errors for cache operations.
var (
	// ErrNotFound is returned when a key is absent or expired.
	ErrNotFound = errors.New("cache: entry not found")
	// ErrClosed is returned by a store after Close.
	ErrClosed = errors.New("cache: closed")
	// ErrMarshal is returned when value serialization fails.
	ErrMarshal = errors.New("cache: failed to marshal value")
	// ErrUnmarshal is returned when value deserialization fails.
	ErrUnmarshal = errors.New("cache: failed to unmarshal value")
	// ErrStore wraps an unclassified error returned by the underlying Store.
	ErrStore = errors.New("cache: store operation failed")
	// ErrExists is returned by Set with WithSetNonExist when the key is present.
	ErrExists = errors.New("cache: entry already exists")
)
