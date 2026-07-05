// Package retry runs an operation with a backoff strategy until it succeeds,
// a permanent error is returned, or the context is cancelled.
//
// # Usage
//
//	err := retry.Do(ctx, func(ctx context.Context) error {
//	    return callFlakyAPI(ctx)
//	}, retry.WithMaxAttempts(5))
//
// Wrap an error with retry.Permanent to stop early:
//
//	return retry.Permanent(errBadRequest)
//
// An error implementing RetryAfterError (RetryAfter() time.Duration) raises the
// wait before the next attempt to at least the reported duration — e.g. an HTTP
// 429/503 Retry-After or a circuitbreaker open error is honored automatically.
package retry
