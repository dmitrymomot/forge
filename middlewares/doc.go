// Package middlewares provides HTTP middleware for Forge applications.
//
// This package includes four essential middlewares:
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
// # CORS
//
// CORS middleware handles Cross-Origin Resource Sharing headers.
// It processes preflight (OPTIONS) requests and adds CORS headers to all responses.
//
//	app := forge.New(
//	    forge.WithMiddleware(
//	        middlewares.CORS(),  // Allow all origins (default)
//	    ),
//	)
//
// Configure specific origins and credentials:
//
//	app := forge.New(
//	    forge.WithMiddleware(
//	        middlewares.CORS(
//	            middlewares.WithAllowOrigins("https://app.example.com"),
//	            middlewares.WithAllowCredentials(),
//	        ),
//	    ),
//	)
//
// Use dynamic origin validation:
//
//	app := forge.New(
//	    forge.WithMiddleware(
//	        middlewares.CORS(
//	            middlewares.WithAllowOriginFunc(func(origin string) bool {
//	                // Custom logic to validate origin
//	                return strings.HasSuffix(origin, ".example.com")
//	            }),
//	        ),
//	    ),
//	)
//
// # JWT
//
// JWT middleware extracts a JWT from the request, validates it, and stores
// the parsed claims in the context. It uses generics so handlers can work
// with custom claims types.
//
// Basic usage with standard claims:
//
//	jwtSvc, _ := jwt.NewFromString(os.Getenv("JWT_SECRET"))
//	app := forge.New(
//	    forge.WithMiddleware(
//	        middlewares.JWT[jwt.StandardClaims](jwtSvc),
//	    ),
//	)
//
// Access claims in a handler:
//
//	func (h *Handler) Routes(r forge.Router) {
//	    r.GET("/me", h.me)
//	}
//
//	func (h *Handler) me(c forge.Context) error {
//	    claims := forge.GetJWTClaims[jwt.StandardClaims](c)
//	    return c.JSON(200, map[string]string{"user": claims.Subject})
//	}
//
// Custom claims with additional fields:
//
//	type MyClaims struct {
//	    jwt.StandardClaims
//	    Role   string `json:"role"`
//	    TeamID string `json:"team_id"`
//	}
//
//	func (c MyClaims) Valid() error { return c.StandardClaims.Valid() }
//
//	app := forge.New(
//	    forge.WithMiddleware(
//	        middlewares.JWT[MyClaims](jwtSvc),
//	    ),
//	)
//
//	// In handler:
//	claims := forge.GetJWTClaims[MyClaims](c)
//	if claims.Role == "admin" { ... }
//
// Custom token extractor (e.g., from query parameter):
//
//	app := forge.New(
//	    forge.WithMiddleware(
//	        middlewares.JWT[jwt.StandardClaims](jwtSvc,
//	            middlewares.WithJWTExtractor(
//	                forge.NewExtractor(forge.FromQuery("token")),
//	            ),
//	        ),
//	    ),
//	)
//
// # Recommended Middleware Order
//
// Apply middlewares in this order for best results:
//
//	forge.WithMiddleware(
//	    middlewares.CORS(),       // First: handle preflight before other processing
//	    middlewares.RequestID(),  // Second: assign ID for all subsequent logging
//	    middlewares.Recover(),    // Third: catch panics from timeout and handlers
//	    middlewares.Timeout(5*time.Second), // Fourth: enforce timeout
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
//	        middlewares.CORS(),
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
