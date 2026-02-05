package middlewares

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/internal"
)

// DefaultCORSMaxAge is the default preflight cache duration.
const DefaultCORSMaxAge = 12 * time.Hour

// DefaultCORSConfig provides sensible defaults for CORS.
var DefaultCORSConfig = CORSConfig{
	AllowOrigins: []string{"*"},
	AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
	AllowHeaders: []string{"Origin", "Content-Type", "Accept", "Authorization"},
	MaxAge:       DefaultCORSMaxAge,
}

// CORSConfig configures the CORS middleware.
type CORSConfig struct {
	// AllowOrigins is a static list of allowed origins.
	// Use "*" to allow all origins (not recommended with credentials).
	AllowOrigins []string

	// AllowOriginFunc is a dynamic origin validator.
	// When set, it completely overrides AllowOrigins for that request.
	// Return true if the origin should be allowed.
	AllowOriginFunc func(origin string) bool

	// AllowMethods specifies the allowed HTTP methods.
	AllowMethods []string

	// AllowHeaders specifies the allowed request headers.
	AllowHeaders []string

	// ExposeHeaders specifies headers exposed to the client.
	ExposeHeaders []string

	// AllowCredentials indicates whether credentials (cookies, authorization headers) are allowed.
	// When true, Access-Control-Allow-Origin cannot be "*" — the actual origin is echoed.
	AllowCredentials bool

	// MaxAge specifies how long preflight responses can be cached.
	MaxAge time.Duration
}

// CORSOption configures CORSConfig.
type CORSOption func(*CORSConfig)

// WithAllowOrigins sets the allowed origins.
func WithAllowOrigins(origins ...string) CORSOption {
	return func(cfg *CORSConfig) {
		cfg.AllowOrigins = origins
	}
}

// WithAllowOriginFunc sets a dynamic origin validator.
// When set, it completely overrides AllowOrigins.
func WithAllowOriginFunc(fn func(origin string) bool) CORSOption {
	return func(cfg *CORSConfig) {
		cfg.AllowOriginFunc = fn
	}
}

// WithAllowMethods sets the allowed HTTP methods.
func WithAllowMethods(methods ...string) CORSOption {
	return func(cfg *CORSConfig) {
		cfg.AllowMethods = methods
	}
}

// WithAllowHeaders sets the allowed request headers.
func WithAllowHeaders(headers ...string) CORSOption {
	return func(cfg *CORSConfig) {
		cfg.AllowHeaders = headers
	}
}

// WithExposeHeaders sets the headers exposed to the client.
func WithExposeHeaders(headers ...string) CORSOption {
	return func(cfg *CORSConfig) {
		cfg.ExposeHeaders = headers
	}
}

// WithAllowCredentials enables credentials support.
// When enabled, Access-Control-Allow-Origin echoes the actual origin instead of "*".
func WithAllowCredentials() CORSOption {
	return func(cfg *CORSConfig) {
		cfg.AllowCredentials = true
	}
}

// WithMaxAge sets the preflight cache duration.
func WithMaxAge(duration time.Duration) CORSOption {
	return func(cfg *CORSConfig) {
		cfg.MaxAge = duration
	}
}

// CORS returns middleware that handles Cross-Origin Resource Sharing.
// It processes preflight (OPTIONS) requests and adds CORS headers to all responses.
func CORS(opts ...CORSOption) internal.Middleware {
	cfg := &CORSConfig{
		AllowOrigins: DefaultCORSConfig.AllowOrigins,
		AllowMethods: DefaultCORSConfig.AllowMethods,
		AllowHeaders: DefaultCORSConfig.AllowHeaders,
		MaxAge:       DefaultCORSConfig.MaxAge,
	}

	for _, opt := range opts {
		opt(cfg)
	}

	// Pre-compute joined strings for headers
	allowMethodsStr := strings.Join(cfg.AllowMethods, ", ")
	allowHeadersStr := strings.Join(cfg.AllowHeaders, ", ")
	exposeHeadersStr := strings.Join(cfg.ExposeHeaders, ", ")
	maxAgeStr := strconv.Itoa(int(cfg.MaxAge.Seconds()))

	// Check if wildcard is in allow origins
	hasWildcard := slices.Contains(cfg.AllowOrigins, "*")

	return func(next internal.HandlerFunc) internal.HandlerFunc {
		return func(c internal.Context) error {
			if c == nil {
				return next(c)
			}

			origin := c.Header("Origin")

			// Not a CORS request — continue without adding headers
			if origin == "" {
				return next(c)
			}

			// Check if origin is allowed
			allowed := isOriginAllowed(origin, cfg, hasWildcard)
			if !allowed {
				// Origin not allowed — continue without CORS headers (browser will block)
				return next(c)
			}

			// Set CORS headers
			headers := c.Response().Header()

			// Vary header for proper caching
			headers.Add("Vary", "Origin")

			// Set Access-Control-Allow-Origin
			// When credentials are enabled or specific origins are configured, echo the actual origin
			if cfg.AllowCredentials || !hasWildcard {
				headers.Set("Access-Control-Allow-Origin", origin)
			} else {
				headers.Set("Access-Control-Allow-Origin", "*")
			}

			// Set credentials header if enabled
			if cfg.AllowCredentials {
				headers.Set("Access-Control-Allow-Credentials", "true")
			}

			// Set expose headers if configured
			if exposeHeadersStr != "" {
				headers.Set("Access-Control-Expose-Headers", exposeHeadersStr)
			}

			// Handle preflight request
			if c.Request().Method == http.MethodOptions {
				headers.Add("Vary", "Access-Control-Request-Method")
				headers.Add("Vary", "Access-Control-Request-Headers")

				headers.Set("Access-Control-Allow-Methods", allowMethodsStr)
				headers.Set("Access-Control-Allow-Headers", allowHeadersStr)

				if cfg.MaxAge > 0 {
					headers.Set("Access-Control-Max-Age", maxAgeStr)
				}

				return c.NoContent(http.StatusNoContent)
			}

			return next(c)
		}
	}
}

// isOriginAllowed checks if the given origin is allowed based on configuration.
func isOriginAllowed(origin string, cfg *CORSConfig, hasWildcard bool) bool {
	// AllowOriginFunc completely overrides AllowOrigins when set
	if cfg.AllowOriginFunc != nil {
		return cfg.AllowOriginFunc(origin)
	}

	// Wildcard allows all
	if hasWildcard {
		return true
	}

	// Check static list
	return slices.Contains(cfg.AllowOrigins, origin)
}
