package ratelimit

import "errors"

// Sentinel errors for rate limit operations.
var (
	// ErrRateLimited is returned by AllowN when n exceeds the configured limit.
	// Such a batch can never fit within a single window, so it is rejected up
	// front without incrementing the counter (see [Limiter.AllowN]).
	//
	// Note: an ordinary request that is merely throttled does NOT return this
	// error — it returns an [Info] with IsAllowed()==false and a non-zero
	// RetryAfter. ErrRateLimited signals a structurally impossible request.
	ErrRateLimited = errors.New("ratelimit: rate limit exceeded")

	// ErrInvalidLimit is returned when the rate limit is zero or negative.
	ErrInvalidLimit = errors.New("ratelimit: limit must be positive")

	// ErrInvalidWindow is returned when the window duration is zero or negative.
	ErrInvalidWindow = errors.New("ratelimit: window must be positive")

	// ErrInvalidN is returned by AllowN when n is zero or negative.
	ErrInvalidN = errors.New("ratelimit: n must be positive")

	// ErrNilCounter is returned when a nil Counter is provided.
	ErrNilCounter = errors.New("ratelimit: counter must not be nil")
)
