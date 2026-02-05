package middlewares

import (
	"context"
	"time"

	"github.com/dmitrymomot/forge/internal"
)

// DefaultTimeout is the default request timeout.
const DefaultTimeout = 30 * time.Second

// TimeoutConfig configures the timeout middleware.
type TimeoutConfig struct {
	Timeout time.Duration
}

// TimeoutOption configures TimeoutConfig.
type TimeoutOption func(*TimeoutConfig)

// Timeout returns middleware that enforces a request timeout.
// If the handler does not complete within the timeout, a TimeoutError is returned
// to be handled by the global ErrorHandler.
//
// Note: The handler goroutine continues running after timeout. Use context.Done()
// in long-running operations to detect cancellation and terminate early.
// Request ID is automatically included via RequestIDExtractor() if configured.
func Timeout(timeout time.Duration, opts ...TimeoutOption) internal.Middleware {
	cfg := &TimeoutConfig{
		Timeout: timeout,
	}

	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}

	return func(next internal.HandlerFunc) internal.HandlerFunc {
		return func(c internal.Context) error {
			ctx, cancel := context.WithTimeout(c.Context(), cfg.Timeout)
			defer cancel()

			c.Set(timeoutContextKey{}, ctx)

			// Use goroutine + select to allow handler to complete normally if context is cancelled
			// for reasons other than timeout, rather than forcing early termination.
			done := make(chan error, 1)
			go func() {
				done <- next(c)
			}()

			select {
			case err := <-done:
				return err
			case <-ctx.Done():
				if ctx.Err() == context.DeadlineExceeded {
					c.LogWarn("request timeout", "timeout", cfg.Timeout.String())
					return &TimeoutError{Duration: cfg.Timeout}
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
