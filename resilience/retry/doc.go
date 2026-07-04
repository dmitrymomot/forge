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
package retry
