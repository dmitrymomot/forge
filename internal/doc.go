// Package internal provides the core types and implementation for the Forge framework.
//
// This package is internal and should not be used directly. Import "github.com/dmitrymomot/forge"
// instead, which re-exports the public API.
//
// # Core Types
//
// The package defines the fundamental types that users interact with:
//
//   - App: Orchestrates the application lifecycle, HTTP routing, and graceful shutdown
//   - Context: Provides request/response access and helper methods during handler execution
//   - Router: Interface handlers use to declare routes with HTTP methods and grouping
//   - Handler: Interface implemented by types that declare routes on a router
//   - HandlerFunc: Signature for individual route handlers that return errors
//   - Middleware: Wraps handlers to add cross-cutting concerns like auth or logging
//   - ErrorHandler: Custom error handling function for handler errors
//
// # Application Structure
//
// Create an application with New() and configure it using options:
//
//	app := internal.New(
//	    internal.WithHandlers(authHandler, pageHandler),
//	    internal.WithMiddleware(loggingMiddleware, panicMiddleware),
//	    internal.WithHealthChecks(internal.WithReadinessCheck("db", dbCheck)),
//	)
//
// # Handler Pattern
//
// Handlers implement the Handler interface and declare routes:
//
//	type AuthHandler struct {
//	    repo *repository.Queries
//	}
//
//	func (h *AuthHandler) Routes(r internal.Router) {
//	    r.GET("/login", h.showLogin)
//	    r.POST("/login", h.handleLogin)
//	}
//
// Handlers receive dependencies via constructor injection, not context helpers.
// This keeps handler logic explicit and testable.
//
// # Request Handling
//
// Each request receives a Context with comprehensive helper methods:
//
//	func (h *AuthHandler) handleLogin(c internal.Context) error {
//	    var form LoginForm
//	    validationErrs, err := c.Bind(&form)
//	    if err != nil {
//	        return c.Error(http.StatusBadRequest, "invalid form")
//	    }
//	    if len(validationErrs) > 0 {
//	        return c.Render(http.StatusOK, loginTemplate, validationErrs)
//	    }
//
//	    // Process login...
//	    return c.JSON(http.StatusOK, result)
//	}
//
// # Middleware
//
// Middleware wraps handlers to add cross-cutting concerns:
//
//	func LoggingMiddleware(next internal.HandlerFunc) internal.HandlerFunc {
//	    return func(c internal.Context) error {
//	        start := time.Now()
//	        err := next(c)
//	        duration := time.Since(start)
//	        c.LogInfo("request processed", "duration", duration)
//	        return err
//	    }
//	}
//
// Middleware can inspect/modify the request, short-circuit processing, or wrap the response.
// They have full access to the Context and can be route-specific or global.
//
// # Error Handling
//
// Errors returned from handlers trigger the ErrorHandler:
//
//	func customErrorHandler(c internal.Context, err error) error {
//	    if statusCode := getStatusCode(err); statusCode > 0 {
//	        return c.Error(statusCode, err.Error())
//	    }
//	    c.LogError("unhandled error", "error", err)
//	    return c.Error(http.StatusInternalServerError, "internal server error")
//	}
//
// # Server Runtime
//
// Start the server with Run() or use Run() for multi-domain deployments:
//
//	// Single app
//	err := app.Run(":8080", internal.Logger(log))
//
//	// Multi-domain
//	err := internal.Run(
//	    internal.Domain("api.example.com", apiApp),
//	    internal.Domain("*.example.com", tenantApp),
//	    internal.Address(":8080"),
//	)
//
// # Features
//
// The Context provides helpers for common request patterns:
//   - JSON encoding/decoding
//   - Form binding with validation and sanitization
//   - Cookie management (plain, signed, encrypted, flash)
//   - Session management (load, create, authenticate, destroy)
//   - HTMX-aware response rendering
//   - File upload/download with configurable storage
//   - Background job enqueueing
//   - Structured logging with request-scoped values
//   - Domain and subdomain extraction
//   - Custom context values
//
// # Design Principles
//
//   - No magic: Explicit code, no reflection, no service containers
//   - Flat handlers: Business logic in handlers, extract to services only when shared
//   - Constructor injection: All dependencies visible in main.go
//   - No context helpers: Packages receive values via parameters
//   - Framework, not boilerplate: Provides utilities, not business logic
//
// See the forge package documentation for the public API and usage examples.
package internal
