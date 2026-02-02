package forge

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/pkg/health"
)

// Option configures the application.
type Option func(*App)

// WithContext sets a custom base context for signal handling.
// Useful for testing or when integrating with existing context hierarchies.
// Defaults to context.Background() if not set.
func WithContext(ctx context.Context) Option {
	return func(a *App) {
		if ctx != nil {
			a.baseCtx = ctx
		}
	}
}

// WithLogger sets the application logger.
// If nil, logging is disabled.
func WithLogger(l *slog.Logger) Option {
	return func(a *App) {
		if l != nil {
			a.logger = l
		}
	}
}

// WithAddress sets the HTTP server address.
// Defaults to ":8080".
func WithAddress(addr string) Option {
	return func(a *App) {
		if addr != "" {
			a.server.Addr = addr
		}
	}
}

// WithReadTimeout sets the HTTP server read timeout.
// Defaults to 15 seconds.
func WithReadTimeout(d time.Duration) Option {
	return func(a *App) {
		if d > 0 {
			a.server.ReadTimeout = d
		}
	}
}

// WithWriteTimeout sets the HTTP server write timeout.
// Defaults to 30 seconds.
func WithWriteTimeout(d time.Duration) Option {
	return func(a *App) {
		if d > 0 {
			a.server.WriteTimeout = d
		}
	}
}

// WithIdleTimeout sets the HTTP server idle timeout.
// Defaults to 120 seconds.
func WithIdleTimeout(d time.Duration) Option {
	return func(a *App) {
		if d > 0 {
			a.server.IdleTimeout = d
		}
	}
}

// WithReadHeaderTimeout sets the HTTP server read header timeout.
// Defaults to 5 seconds.
func WithReadHeaderTimeout(d time.Duration) Option {
	return func(a *App) {
		if d > 0 {
			a.server.ReadHeaderTimeout = d
		}
	}
}

// WithMiddleware adds global middleware to the application.
// Middleware is applied in the order provided.
func WithMiddleware(mw ...Middleware) Option {
	return func(a *App) {
		a.middlewares = append(a.middlewares, mw...)
	}
}

// WithHandlers registers handlers that declare routes.
// Each handler's Routes method is called during setup.
func WithHandlers(h ...Handler) Option {
	return func(a *App) {
		a.handlers = append(a.handlers, h...)
	}
}

// WithStaticFiles mounts a static file handler at the given pattern.
// Directory listings are disabled. Files are served with default cache headers.
//
// Example:
//
//	//go:embed public
//	var assets embed.FS
//
//	forge.New(
//	    forge.WithStaticFiles("/static/", assets, "public"),
//	)
func WithStaticFiles(pattern string, fsys fs.FS, subDir string) Option {
	return func(a *App) {
		subFS, err := fs.Sub(fsys, subDir)
		if err != nil {
			panic(err)
		}

		fileServer := http.FileServerFS(subFS)

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Block directory listings
			if strings.HasSuffix(r.URL.Path, "/") {
				http.NotFound(w, r)
				return
			}

			w.Header().Set("Cache-Control", "public, max-age=3600")
			w.Header().Set("X-Content-Type-Options", "nosniff")

			fileServer.ServeHTTP(w, r)
		})

		a.staticRoutes = append(a.staticRoutes, staticRoute{pattern, handler})
	}
}

// WithHostRoutes enables host-based routing.
// Requests matching a host pattern go to that handler.
// Unmatched requests go to the default router (handlers registered via WithHandlers).
//
// Example:
//
//	apiRouter := chi.NewRouter()
//	// ... setup api routes
//
//	tenantRouter := chi.NewRouter()
//	// ... setup tenant routes
//
//	app := forge.New(
//	    forge.WithHostRoutes(forge.HostRoutes{
//	        "api.example.com": apiRouter,      // Exact match
//	        "*.example.com":   tenantRouter,   // Wildcard for subdomains
//	    }),
//	    forge.WithHandlers(defaultHandler),    // Fallback for unmatched hosts
//	)
func WithHostRoutes(routes HostRoutes) Option {
	return func(a *App) {
		a.hostRoutes = routes
	}
}

// WithErrorHandler sets a custom error handler for handler errors.
// Called when a handler returns a non-nil error.
//
// Example:
//
//	forge.WithErrorHandler(func(c forge.Context, err error) error {
//	    // Log error, render error page, etc.
//	    return c.JSON(http.StatusInternalServerError, map[string]string{
//	        "error": err.Error(),
//	    })
//	})
func WithErrorHandler(h ErrorHandler) Option {
	return func(a *App) {
		a.errorHandler = h
	}
}

// WithNotFoundHandler sets a custom 404 handler.
//
// Example:
//
//	forge.WithNotFoundHandler(func(c forge.Context) error {
//	    return c.String(http.StatusNotFound, "Page not found")
//	})
func WithNotFoundHandler(h HandlerFunc) Option {
	return func(a *App) {
		a.notFoundHandler = h
	}
}

// WithMethodNotAllowedHandler sets a custom 405 handler.
//
// Example:
//
//	forge.WithMethodNotAllowedHandler(func(c forge.Context) error {
//	    return c.String(http.StatusMethodNotAllowed, "Method not allowed")
//	})
func WithMethodNotAllowedHandler(h HandlerFunc) Option {
	return func(a *App) {
		a.methodNotAllowedHandler = h
	}
}

// WithShutdownTimeout sets the timeout for graceful shutdown.
// This applies to both the HTTP server and shutdown hooks.
// Defaults to 30 seconds.
func WithShutdownTimeout(d time.Duration) Option {
	return func(a *App) {
		if d > 0 {
			a.shutdownTimeout = d
		}
	}
}

// WithShutdownHook registers a cleanup function to run during shutdown.
// Hooks are called in the order they were registered.
// Each hook receives a context with the shutdown timeout.
//
// Example:
//
//	forge.WithShutdownHook(db.Shutdown(pool))
func WithShutdownHook(fn func(context.Context) error) Option {
	return func(a *App) {
		if fn != nil {
			a.shutdownHooks = append(a.shutdownHooks, fn)
		}
	}
}

// healthConfig holds health check endpoint configuration.
type healthConfig struct {
	livenessPath  string
	readinessPath string
	checks        health.Checks
}

// Default health check paths.
const (
	defaultLivenessPath  = "/health/live"
	defaultReadinessPath = "/health/ready"
)

// HealthOption configures health check endpoints.
type HealthOption func(*healthConfig)

// WithLivenessPath sets a custom liveness endpoint path.
// Defaults to "/health/live".
func WithLivenessPath(path string) HealthOption {
	return func(c *healthConfig) {
		if path != "" {
			c.livenessPath = path
		}
	}
}

// WithReadinessPath sets a custom readiness endpoint path.
// Defaults to "/health/ready".
func WithReadinessPath(path string) HealthOption {
	return func(c *healthConfig) {
		if path != "" {
			c.readinessPath = path
		}
	}
}

// WithReadinessCheck adds a named readiness check.
// Checks run in parallel during readiness probe.
//
// Example:
//
//	forge.WithReadinessCheck("db", db.Healthcheck(pool))
func WithReadinessCheck(name string, fn health.CheckFunc) HealthOption {
	return func(c *healthConfig) {
		if c.checks == nil {
			c.checks = make(health.Checks)
		}
		c.checks[name] = fn
	}
}

// WithHealthChecks enables health check endpoints with optional configuration.
// Liveness (/health/live): Always returns OK if process is running.
// Readiness (/health/ready): Runs all configured checks.
//
// Example:
//
//	forge.WithHealthChecks(
//	    forge.WithReadinessCheck("db", db.Healthcheck(pool)),
//	    forge.WithReadinessCheck("redis", redis.Healthcheck(client)),
//	)
func WithHealthChecks(opts ...HealthOption) Option {
	return func(a *App) {
		cfg := &healthConfig{
			livenessPath:  defaultLivenessPath,
			readinessPath: defaultReadinessPath,
			checks:        make(health.Checks),
		}
		for _, opt := range opts {
			opt(cfg)
		}
		a.healthConfig = cfg
	}
}
