// Package ratelimit provides a sliding window rate limiter with pluggable storage backends.
//
// The sliding window algorithm blends request counts from the previous and current
// fixed windows, producing a smooth rate estimate that avoids the boundary burst
// problem of simple fixed-window counters.
//
// # Basic Usage
//
// Create a counter, build a limiter, and call Allow on each request:
//
//	counter := ratelimit.NewMemoryCounter(ratelimit.MemoryConfig{})
//	defer counter.Close()
//
//	lim, err := ratelimit.New(counter, 100, time.Minute)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	info, err := lim.Allow(ctx, "user:123")
//	if err != nil {
//	    // handle error
//	}
//	if !info.IsAllowed() {
//	    // reject request, retry after info.RetryAfter
//	}
//
// # Counter Backends
//
// Two counter implementations are provided:
//
// [NewMemoryCounter] creates an in-memory counter suitable for single-process
// deployments. It runs a background goroutine to clean up expired window data:
//
//	counter := ratelimit.NewMemoryCounter(ratelimit.MemoryConfig{
//	    CleanupInterval: 30 * time.Second,
//	})
//	defer counter.Close()
//
// [NewRedisCounter] creates a Redis-backed counter for distributed deployments.
// On Redis failure, it automatically falls back to an in-memory counter:
//
//	client := redis.MustOpen(ctx, redisConfig)
//	counter := ratelimit.NewRedisCounter(client, ratelimit.RedisConfig{
//	    Prefix: "api",
//	})
//
// # Key Extractors
//
// [KeyFunc] extracts a rate-limit key from an HTTP request. Several built-in
// extractors are provided:
//
//   - [KeyByIP] — client IP address (supports CDN headers)
//   - [KeyByFingerprint] — device fingerprint
//   - [KeyByPath] — request URL path
//   - [KeyByHeader] — arbitrary request header value
//
// Use [KeyComposite] to combine multiple extractors into a single key:
//
//	keyFn := ratelimit.KeyComposite(ratelimit.KeyByIP, ratelimit.KeyByPath)
//	key := keyFn(r) // "192.168.1.1:/api/users"
//
// # Peeking
//
// Use [Limiter.Peek] to check the current rate limit status without incrementing
// the counter:
//
//	info, err := lim.Peek(ctx, "user:123")
//
// # Error Handling
//
// The package defines sentinel errors:
//
//   - [ErrRateLimited] — request exceeds the rate limit
//   - [ErrInvalidLimit] — limit is zero or negative
//   - [ErrInvalidWindow] — window duration is zero or negative
//   - [ErrNilCounter] — nil counter provided to New
//
// Use [errors.Is] to check:
//
//	_, err := ratelimit.New(nil, 100, time.Minute)
//	if errors.Is(err, ratelimit.ErrNilCounter) {
//	    // handle missing counter
//	}
package ratelimit
