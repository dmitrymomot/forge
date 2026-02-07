package middlewares

import (
	"context"
	"errors"
	"time"

	"github.com/dmitrymomot/forge/internal"
)

// DefaultTimeout is the default request timeout.
const DefaultTimeout = 30 * time.Second

// Timeout returns middleware that enforces a request timeout.
// If the handler does not complete within the timeout, a TimeoutError is returned
// to be handled by the global ErrorHandler.
//
// Note: The handler goroutine continues running after timeout. Use context.Done()
// in long-running operations to detect cancellation and terminate early.
// Request ID is automatically included via RequestIDExtractor() if configured.
func Timeout(timeout time.Duration) internal.Middleware {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	return func(next internal.HandlerFunc) internal.HandlerFunc {
		return func(c internal.Context) error {
			ctx, cancel := context.WithTimeout(c.Context(), timeout)
			defer cancel()

			c.Set(timeoutContextKey{}, ctx)

			// Capture logger before spawning goroutine (not safe to access c.Logger() from timeout goroutine)
			logger := c.Logger()

			done := make(chan error, 1)
			go func() {
				done <- next(c)
			}()

			select {
			case err := <-done:
				return err
			case <-ctx.Done():
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					logger.WarnContext(ctx, "request timeout", "timeout", timeout.String())
					return &TimeoutError{Duration: timeout}
				}
				return ctx.Err()
			}
		}
	}
}

// timeoutContextKey is used to store the timeout context.
type timeoutContextKey struct{}

// GetTimeoutContext retrieves the timeout context if available.
// This allows handlers to check for cancellation via ctx.Done().
func GetTimeoutContext(c internal.Context) context.Context {
	if v, ok := c.Get(timeoutContextKey{}).(context.Context); ok {
		return v
	}
	return c.Context()
}
