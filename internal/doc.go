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
//   - Context: Provides request/response access, identity, RBAC, and helper methods
//   - Router: Interface handlers use to declare routes with HTTP methods and grouping
//   - Handler: Interface implemented by types that declare routes on a router
//   - HandlerFunc: Signature for individual route handlers that return errors
//   - Middleware: Wraps handlers to add cross-cutting concerns like auth or logging
//   - ErrorHandler: Custom error handling function for handler errors
//   - Permission: Named permission string for RBAC checks
//   - RolePermissions: Maps role names to their granted permissions
//   - RoleExtractorFunc: Extracts the current user's role from the request context
//
// # Context as context.Context
//
// Context embeds context.Context, so it can be passed directly to any function
// that expects a standard library context. The Deadline, Done, Err, and Value
// methods delegate to the underlying request context:
//
//	func (h *Handler) getUser(c forge.Context) error {
//	    // Pass c directly to database calls, HTTP clients, etc.
//	    user, err := h.repo.GetUser(c, userID)
//	    if err != nil {
//	        return err
//	    }
//	    return c.JSON(200, user)
//	}
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
// # Identity Methods
//
// Context provides convenience methods for checking user identity. These are
// shortcuts over the session system — they load the session lazily on first
// access and return safe defaults when no session is configured:
//
//   - UserID() string: Returns the authenticated user's ID, or empty string
//   - IsAuthenticated() bool: Returns true if a user is associated with the session
//   - IsCurrentUser(id string) bool: Returns true if the given ID matches the current user
//
// Example:
//
//	func (h *Handler) showProfile(c internal.Context) error {
//	    if !c.IsAuthenticated() {
//	        return c.Redirect(302, "/login")
//	    }
//	    user, err := h.repo.GetUser(c, c.UserID())
//	    if err != nil {
//	        return err
//	    }
//	    return c.JSON(200, user)
//	}
//
// # Role-Based Access Control (RBAC)
//
// Configure RBAC with WithRoles, which accepts a permission map and a role
// extractor function. The role extractor is called lazily on the first Can()
// call and the result is cached for the lifetime of the request:
//
//	app := internal.New(
//	    internal.WithRoles(
//	        internal.RolePermissions{
//	            "admin":  {"users.read", "users.write", "billing.manage"},
//	            "member": {"users.read"},
//	        },
//	        func(c internal.Context) string {
//	            role, _ := c.SessionValue("role")
//	            if s, ok := role.(string); ok {
//	                return s
//	            }
//	            return ""
//	        },
//	    ),
//	)
//
// Check permissions in handlers with Can():
//
//	func (h *Handler) deleteUser(c internal.Context) error {
//	    if !c.Can("users.write") {
//	        return c.Error(403, "forbidden")
//	    }
//	    // proceed with deletion...
//	}
//
// Can() returns false if RBAC is not configured, the user has no role, or
// the role does not grant the requested permission. It never panics.
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
//   - Identity shortcuts (UserID, IsAuthenticated, IsCurrentUser)
//   - Role-based access control (Can with lazy role extraction)
//   - Standard library context.Context compatibility
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
