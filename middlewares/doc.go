// Package middlewares provides HTTP middleware for Forge applications.
//
// This package includes three essential middlewares:
//
// # Request ID
//
// RequestID middleware assigns a unique ID to each request for tracing and debugging.
// It checks incoming headers for existing IDs or generates new ones using ULID.
//
//	app := forge.New(
//	    forge.WithMiddleware(
//	        middlewares.RequestID(),
//	    ),
//	)
//
// Use RequestIDExtractor() with WithLogger for automatic request_id in all logs:
//
//	app := forge.New(
//	    forge.WithLogger("api", forge.RequestIDExtractor()),
//	    forge.WithMiddleware(
//	        middlewares.RequestID(),
//	    ),
//	)
//
// # Recover
//
// Recover middleware catches panics and converts them to typed errors.
// The PanicError can be handled by the global ErrorHandler.
//
//	app := forge.New(
//	    forge.WithMiddleware(
//	        middlewares.Recover(),
//	    ),
//	    forge.WithErrorHandler(func(c forge.Context, err error) error {
//	        if forge.IsPanicError(err) {
//	            pe, _ := forge.AsPanicError(err)
//	            c.LogError("panic", "value", pe.Value, "stack", string(pe.Stack))
//	            return c.Error(500, "Internal Server Error")
//	        }
//	        return c.Error(500, err.Error())
//	    }),
//	)
//
// # Timeout
//
// Timeout middleware enforces request timeouts and returns typed TimeoutError.
// Note: The handler goroutine continues after timeout; use context.Done() for early termination.
//
//	app := forge.New(
//	    forge.WithMiddleware(
//	        middlewares.Timeout(5*time.Second),
//	    ),
//	    forge.WithErrorHandler(func(c forge.Context, err error) error {
//	        if forge.IsTimeoutError(err) {
//	            return c.Error(504, "Gateway Timeout")
//	        }
//	        return c.Error(500, err.Error())
//	    }),
//	)
//
// # Recommended Middleware Order
//
// Apply middlewares in this order for best results:
//
//	forge.WithMiddleware(
//	    middlewares.RequestID(),  // First: assign ID for all subsequent logging
//	    middlewares.Recover(),    // Second: catch panics from timeout and handlers
//	    middlewares.Timeout(5*time.Second), // Third: enforce timeout
//	)
//
// # Complete Example
//
//	import (
//	    "github.com/dmitrymomot/forge"
//	    "github.com/dmitrymomot/forge/middlewares"
//	)
//
//	app := forge.New(
//	    forge.WithLogger("api", forge.RequestIDExtractor()),
//	    forge.WithMiddleware(
//	        middlewares.RequestID(),
//	        middlewares.Recover(),
//	        middlewares.Timeout(5*time.Second),
//	    ),
//	    forge.WithErrorHandler(func(c forge.Context, err error) error {
//	        switch {
//	        case forge.IsPanicError(err):
//	            return c.Error(500, "Internal Server Error")
//	        case forge.IsTimeoutError(err):
//	            return c.Error(504, "Gateway Timeout")
//	        default:
//	            return c.Error(500, err.Error())
//	        }
//	    }),
//	)
package middlewares
