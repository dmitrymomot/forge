package middlewares

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/internal"
)

// CORSConfig configures the CORS middleware.
type CORSConfig struct {
	AllowOrigins     []string      `env:"ALLOW_ORIGINS"     envSeparator:"," envDefault:"*"`
	AllowMethods     []string      `env:"ALLOW_METHODS"     envSeparator:"," envDefault:"GET,POST,PUT,PATCH,DELETE,OPTIONS"`
	AllowHeaders     []string      `env:"ALLOW_HEADERS"     envSeparator:"," envDefault:"Origin,Content-Type,Accept,Authorization"`
	ExposeHeaders    []string      `env:"EXPOSE_HEADERS"    envSeparator:","`
	AllowCredentials bool          `env:"ALLOW_CREDENTIALS" envDefault:"false"`
	MaxAge           time.Duration `env:"MAX_AGE"           envDefault:"12h"`
}

type corsOptions struct {
	allowOriginFunc func(origin string) bool
}

// CORSOption configures runtime dependencies for the CORS middleware.
type CORSOption func(*corsOptions)

// WithAllowOriginFunc sets a dynamic origin validator.
// When set, it completely overrides AllowOrigins.
func WithAllowOriginFunc(fn func(origin string) bool) CORSOption {
	return func(o *corsOptions) {
		o.allowOriginFunc = fn
	}
}

// CORS returns middleware that handles Cross-Origin Resource Sharing.
// It processes preflight (OPTIONS) requests and adds CORS headers to all responses.
func CORS(cfg CORSConfig, opts ...CORSOption) internal.Middleware {
	o := &corsOptions{}
	for _, opt := range opts {
		opt(o)
	}

	// Apply runtime defaults for zero/nil fields
	if len(cfg.AllowOrigins) == 0 {
		cfg.AllowOrigins = []string{"*"}
	}
	if len(cfg.AllowMethods) == 0 {
		cfg.AllowMethods = []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions}
	}
	if len(cfg.AllowHeaders) == 0 {
		cfg.AllowHeaders = []string{"Origin", "Content-Type", "Accept", "Authorization"}
	}

	// Pre-compute joined strings to avoid repeated string allocations across multiple requests
	allowMethodsStr := strings.Join(cfg.AllowMethods, ", ")
	allowHeadersStr := strings.Join(cfg.AllowHeaders, ", ")
	exposeHeadersStr := strings.Join(cfg.ExposeHeaders, ", ")
	maxAgeStr := strconv.Itoa(int(cfg.MaxAge.Seconds()))

	// Wildcard check is performed once during middleware setup to optimize lookups on each request
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
			allowed := isOriginAllowed(origin, &cfg, o, hasWildcard)
			if !allowed {
				// Continue without CORS headers; browser's same-origin policy prevents credential access from rejected origins
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
func isOriginAllowed(origin string, cfg *CORSConfig, o *corsOptions, hasWildcard bool) bool {
	// AllowOriginFunc completely overrides AllowOrigins when set
	if o.allowOriginFunc != nil {
		return o.allowOriginFunc(origin)
	}

	// Wildcard allows all
	if hasWildcard {
		return true
	}

	// Check static list
	return slices.Contains(cfg.AllowOrigins, origin)
}
