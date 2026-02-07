package ratelimit

import "errors"

// Sentinel errors for rate limit operations.
var (
	// ErrRateLimited is returned when a request exceeds the rate limit.
	ErrRateLimited = errors.New("ratelimit: rate limit exceeded")

	// ErrInvalidLimit is returned when the rate limit is zero or negative.
	ErrInvalidLimit = errors.New("ratelimit: limit must be positive")

	// ErrInvalidWindow is returned when the window duration is zero or negative.
	ErrInvalidWindow = errors.New("ratelimit: window must be positive")

	// ErrNilCounter is returned when a nil Counter is provided.
	ErrNilCounter = errors.New("ratelimit: counter must not be nil")
)
