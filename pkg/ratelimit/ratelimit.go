package ratelimit

import (
	"context"
	"time"
)

// Counter is a pluggable storage backend for window-based request counts.
// Implementations must be safe for concurrent use.
type Counter interface {
	// Increment atomically adds n to the count for the given key and window.
	// The ttl specifies how long the window data should be retained.
	// Returns the new count after incrementing.
	Increment(ctx context.Context, key string, window time.Time, ttl time.Duration, n int64) (int64, error)

	// Get returns the current count for the given key and window.
	// Returns 0 if the window has no data or has expired.
	Get(ctx context.Context, key string, window time.Time) (int64, error)

	// Close releases resources (stops background goroutines, etc.).
	Close() error
}

// Info contains rate limit status for a given key.
type Info struct {
	// ResetAt is when the current window ends and counters reset.
	ResetAt time.Time

	// Limit is the maximum number of requests allowed per window.
	Limit int64

	// Remaining is the number of requests remaining in the current window.
	// Always >= 0.
	Remaining int64

	// RetryAfter is the duration a rate-limited client should wait before retrying.
	// Zero when the request is not rate-limited.
	RetryAfter time.Duration
}

// IsAllowed reports whether the request was allowed (not rate-limited).
func (i Info) IsAllowed() bool {
	return i.RetryAfter == 0
}

// Limiter enforces rate limits using a sliding window algorithm.
//
// The sliding window blends request counts from the previous and current
// fixed windows to produce a smooth rate estimate that avoids boundary
// burst problems.
type Limiter struct {
	counter Counter
	limit   int64
	window  time.Duration
}

// New creates a Limiter with the given counter, limit, and window size.
//
// The limit must be positive. The window must be positive.
// Returns an error if any argument is invalid.
func New(counter Counter, limit int64, window time.Duration) (*Limiter, error) {
	if counter == nil {
		return nil, ErrNilCounter
	}
	if limit <= 0 {
		return nil, ErrInvalidLimit
	}
	if window <= 0 {
		return nil, ErrInvalidWindow
	}
	return &Limiter{
		counter: counter,
		limit:   limit,
		window:  window,
	}, nil
}

// Allow reports whether a single request for the given key is allowed.
// It increments the counter and returns the current rate limit info.
func (l *Limiter) Allow(ctx context.Context, key string) (Info, error) {
	return l.AllowN(ctx, key, 1)
}

// AllowN reports whether n requests for the given key are allowed.
// It atomically increments the counter by n and returns the current rate limit info.
//
// The counter is incremented before checking the limit (increment-first strategy).
// This simplifies atomic operations and prevents clients from gaming window boundaries.
func (l *Limiter) AllowN(ctx context.Context, key string, n int64) (Info, error) {
	now := time.Now()
	currWindow := now.Truncate(l.window)
	prevWindow := currWindow.Add(-l.window)

	currCount, err := l.counter.Increment(ctx, key, currWindow, 2*l.window, n)
	if err != nil {
		return Info{}, err
	}

	prevCount, err := l.counter.Get(ctx, key, prevWindow)
	if err != nil {
		return Info{}, err
	}

	return l.computeInfo(prevCount, currCount, now, currWindow), nil
}

// Peek returns the current rate limit info for the given key without
// incrementing the counter. Useful for checking status in non-request contexts.
func (l *Limiter) Peek(ctx context.Context, key string) (Info, error) {
	now := time.Now()
	currWindow := now.Truncate(l.window)
	prevWindow := currWindow.Add(-l.window)

	currCount, err := l.counter.Get(ctx, key, currWindow)
	if err != nil {
		return Info{}, err
	}

	prevCount, err := l.counter.Get(ctx, key, prevWindow)
	if err != nil {
		return Info{}, err
	}

	return l.computeInfo(prevCount, currCount, now, currWindow), nil
}

// computeInfo calculates the sliding window rate limit info.
func (l *Limiter) computeInfo(prevCount, currCount int64, now time.Time, currWindow time.Time) Info {
	elapsed := now.Sub(currWindow)
	weight := float64(l.window-elapsed) / float64(l.window)
	weighted := int64(float64(prevCount)*weight) + currCount

	resetAt := currWindow.Add(l.window)
	remaining := max(l.limit-weighted, 0)

	info := Info{
		Limit:     l.limit,
		Remaining: remaining,
		ResetAt:   resetAt,
	}

	if weighted > l.limit {
		info.RetryAfter = l.retryAfter(prevCount, currCount, now, currWindow)
	}

	return info
}

// retryAfter estimates how long until the weighted count drops below the limit.
func (l *Limiter) retryAfter(prevCount, currCount int64, now time.Time, currWindow time.Time) time.Duration {
	resetAt := currWindow.Add(l.window)

	if prevCount == 0 || currCount >= l.limit {
		// Only current window counts or current alone exceeds limit;
		// must wait for full window reset.
		return resetAt.Sub(now)
	}

	// Solve: prevCount * ((W - t) / W) + currCount = limit
	// where t is elapsed time from currWindow start.
	// t = W - W * (limit - currCount) / prevCount
	needed := float64(l.limit - currCount)
	t := l.window - time.Duration(float64(l.window)*needed/float64(prevCount))
	retryAt := currWindow.Add(t)

	if retryAt.Before(now) {
		return 0
	}
	return retryAt.Sub(now)
}
