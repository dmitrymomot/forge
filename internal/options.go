package internal

import (
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/dmitrymomot/forge/pkg/cookie"
	"github.com/dmitrymomot/forge/pkg/health"
	"github.com/dmitrymomot/forge/pkg/logger"
)

// Option configures the application.
type Option func(*App)

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

// WithLogger creates a logger with a component name and optional extractors.
// The component name is added to every log entry for easy filtering.
// Extractors pull values from context (e.g., request_id, user_id).
//
// Example:
//
//	forge.New(
//	    forge.WithLogger("api", requestIDExtractor, userIDExtractor),
//	)
func WithLogger(component string, extractors ...logger.ContextExtractor) Option {
	return func(a *App) {
		a.logger = logger.New(extractors...).With("component", component)
	}
}

// WithCustomLogger sets a fully custom logger.
// Use this when you need complete control over logging configuration.
//
// Example:
//
//	customLogger := slog.New(slog.NewTextHandler(os.Stderr, nil))
//	forge.New(
//	    forge.WithCustomLogger(customLogger),
//	)
func WithCustomLogger(l *slog.Logger) Option {
	return func(a *App) {
		if l != nil {
			a.logger = l
		}
	}
}

// WithCookieOptions configures the cookie manager.
//
// Example:
//
//	forge.New(
//	    forge.WithCookieOptions(
//	        forge.WithCookieSecret(os.Getenv("COOKIE_SECRET")),
//	        forge.WithCookieSecure(true),
//	    ),
//	)
func WithCookieOptions(opts ...cookie.Option) Option {
	return func(a *App) {
		a.cookieManager = cookie.New(opts...)
	}
}
