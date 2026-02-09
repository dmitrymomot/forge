// Package middlewares provides HTTP middleware for Forge applications.
//
// This package includes the following middlewares:
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
//	jwtSvc, _ := jwt.New(jwt.Config{SigningKey: os.Getenv("JWT_SECRET")})
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
// # RBAC
//
// RBAC middlewares gate route groups by authentication status and permissions.
// They use the existing Context.IsAuthenticated() and Context.Can() methods,
// so RBAC must be configured with forge.WithRoles() for permission checks.
//
// RequireAuthenticated rejects unauthenticated requests with 401:
//
//	r.Group(func(r forge.Router) {
//	    r.Use(middlewares.RequireAuthenticated())
//	    r.GET("/dashboard", h.dashboard)
//	})
//
// RequirePermission requires ALL listed permissions (AND logic).
// Returns 401 if unauthenticated, 403 if missing any permission:
//
//	r.Group(func(r forge.Router) {
//	    r.Use(middlewares.RequirePermission("billing.read", "billing.write"))
//	    r.GET("/billing", h.billing)
//	})
//
// RequireAnyPermission requires at least ONE permission (OR logic).
// Returns 401 if unauthenticated, 403 if none match:
//
//	r.Group(func(r forge.Router) {
//	    r.Use(middlewares.RequireAnyPermission("users.read", "users.admin"))
//	    r.GET("/users", h.listUsers)
//	})
//
// # CSRF
//
// CSRF middleware protects against Cross-Site Request Forgery using the
// double-submit cookie pattern with signed cookies. Unsafe methods
// (POST/PUT/PATCH/DELETE) must submit a token matching the signed cookie.
//
// Basic usage:
//
//	app := forge.New(
//	    forge.WithCookieConfig(forge.CookieConfig{Secret: os.Getenv("COOKIE_SECRET")}),
//	    forge.WithMiddleware(
//	        middlewares.CSRF(middlewares.CSRFConfig{}),
//	    ),
//	)
//
// In templates, include the token as a hidden field:
//
//	<form method="POST" action="/submit">
//	    <input type="hidden" name="_csrf" value="{{ forge.GetCSRFToken(c) }}">
//	    <!-- other fields -->
//	</form>
//
// For HTMX, set the token as a request header on the body element:
//
//	<body hx-headers='{"X-CSRF-Token": "{{ forge.GetCSRFToken(c) }}"}'>
//
// Skip CSRF for webhook endpoints:
//
//	middlewares.CSRF(middlewares.CSRFConfig{},
//	    middlewares.WithCSRFSkipFunc(func(c forge.Context) bool {
//	        return strings.HasPrefix(c.Request().URL.Path, "/webhooks/")
//	    }),
//	)
//
// # Rate Limit
//
// RateLimit middleware enforces request rate limits using the pkg/ratelimit
// sliding window algorithm. It accepts a *ratelimit.Limiter as a dependency
// and sets standard rate limit headers on every response.
//
// Basic usage with in-memory counter:
//
//	counter := ratelimit.NewMemoryCounter(ratelimit.MemoryConfig{})
//	defer counter.Close()
//
//	limiter, _ := ratelimit.New(counter, 100, time.Minute) // 100 req/min
//
//	app := forge.New(
//	    forge.WithMiddleware(
//	        middlewares.RateLimit(limiter),
//	    ),
//	)
//
// With Redis counter for distributed deployments:
//
//	redisCounter := ratelimit.NewRedisCounter(redisClient, ratelimit.RedisConfig{
//	    KeyPrefix: "rl:",
//	})
//
//	limiter, _ := ratelimit.New(redisCounter, 100, time.Minute)
//
//	app := forge.New(
//	    forge.WithMiddleware(
//	        middlewares.RateLimit(limiter),
//	    ),
//	)
//
// Custom key function (e.g., by API key header):
//
//	middlewares.RateLimit(limiter,
//	    middlewares.WithRateLimitKeyFunc(ratelimit.KeyByHeader("X-API-Key")),
//	)
//
// Skip rate limiting for webhook endpoints:
//
//	middlewares.RateLimit(limiter,
//	    middlewares.WithRateLimitSkipFunc(func(c forge.Context) bool {
//	        return strings.HasPrefix(c.Request().URL.Path, "/webhooks/")
//	    }),
//	)
//
// Per-route rate limiting using route groups:
//
//	r.Group(func(r forge.Router) {
//	    r.Use(middlewares.RateLimit(strictLimiter))
//	    r.POST("/login", h.login)
//	})
//
// Access rate limit info in handlers:
//
//	func (h *Handler) status(c forge.Context) error {
//	    info := middlewares.GetRateLimitInfo(c)
//	    if info != nil {
//	        // info.Limit, info.Remaining, info.ResetAt
//	    }
//	    return c.JSON(200, data)
//	}
//
// # Recommended Middleware Order
//
// Apply middlewares in this order for best results:
//
//	forge.WithMiddleware(
//	    middlewares.CORS(),                         // First: handle preflight before other processing
//	    middlewares.RateLimit(limiter),              // Second: reject early before spending cycles
//	    middlewares.RequestID(),                     // Third: assign ID for all subsequent logging
//	    middlewares.Recover(),                       // Fourth: catch panics from handlers
//	    middlewares.CSRF(middlewares.CSRFConfig{}),   // Fifth: validate CSRF tokens
//	    // middlewares.RequireAuthenticated(),        // Sixth: auth gate (on protected groups)
//	    // middlewares.RequirePermission(...),        // Seventh: permission gate (on protected groups)
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
//	    ),
//	    forge.WithErrorHandler(func(c forge.Context, err error) error {
//	        switch {
//	        case forge.IsPanicError(err):
//	            return c.Error(500, "Internal Server Error")
//	        default:
//	            return c.Error(500, err.Error())
//	        }
//	    }),
//	)
package middlewares
