package forge

import (
	"context"
	"io/fs"
	"log/slog"
	"time"

	"github.com/dmitrymomot/forge/internal"
	"github.com/dmitrymomot/forge/pkg/health"
	"github.com/dmitrymomot/forge/pkg/logger"
)

// Type aliases - public API
type (
	// App orchestrates the application lifecycle.
	// It manages HTTP routing, middleware, and graceful shutdown.
	App = internal.App

	// Router is the interface handlers use to declare routes.
	Router = internal.Router

	// Context provides request/response access and helper methods.
	Context = internal.Context

	// Handler declares routes on a router.
	Handler = internal.Handler

	// HandlerFunc is the signature for route handlers.
	HandlerFunc = internal.HandlerFunc

	// Middleware wraps a HandlerFunc to add cross-cutting concerns.
	Middleware = internal.Middleware

	// ErrorHandler handles errors returned from handlers.
	ErrorHandler = internal.ErrorHandler

	// Option configures the application.
	Option = internal.Option

	// RunOption configures the server runtime.
	RunOption = internal.RunOption

	// Component is the interface for renderable templates.
	Component = internal.Component

	// ValidationErrors is a collection of validation errors.
	ValidationErrors = internal.ValidationErrors

	// HealthOption configures health check endpoints.
	HealthOption = internal.HealthOption

	// ContextExtractor extracts a slog attribute from context.
	// Used with WithLogger to add request-scoped values to logs.
	ContextExtractor = logger.ContextExtractor
)

// Constructors

// New creates a new application with the given options.
// The App is immutable after creation.
//
// Example:
//
//	app := forge.New(
//	    forge.WithMiddleware(middlewares.Logger(log)),
//	    forge.WithHandlers(
//	        handlers.NewAuth(repo),
//	        handlers.NewPages(repo),
//	    ),
//	)
//
//	err := app.Run(":8080", forge.Logger(slog))
func New(opts ...Option) *App {
	return internal.New(opts...)
}

// Run starts a multi-domain HTTP server and blocks until shutdown.
// Use this for composing multiple Apps under different domain patterns.
//
// Example:
//
//	api := forge.New(
//	    forge.WithHandlers(handlers.NewAPIHandler()),
//	)
//
//	website := forge.New(
//	    forge.WithHandlers(handlers.NewLandingHandler()),
//	)
//
//	err := forge.Run(
//	    forge.Domain("api.acme.com", api),
//	    forge.Domain("*.acme.com", website),
//	    forge.Address(":8080"),
//	    forge.Logger(slog),
//	)
func Run(opts ...RunOption) error {
	return internal.Run(opts...)
}

// App options

// WithMiddleware adds global middleware to the application.
// Middleware is applied in the order provided.
func WithMiddleware(mw ...Middleware) Option {
	return internal.WithMiddleware(mw...)
}

// WithHandlers registers handlers that declare routes.
// Each handler's Routes method is called during setup.
func WithHandlers(h ...Handler) Option {
	return internal.WithHandlers(h...)
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
	return internal.WithStaticFiles(pattern, fsys, subDir)
}

// WithErrorHandler sets a custom error handler for handler errors.
// Called when a handler returns a non-nil error.
func WithErrorHandler(h ErrorHandler) Option {
	return internal.WithErrorHandler(h)
}

// WithNotFoundHandler sets a custom 404 handler.
func WithNotFoundHandler(h HandlerFunc) Option {
	return internal.WithNotFoundHandler(h)
}

// WithMethodNotAllowedHandler sets a custom 405 handler.
func WithMethodNotAllowedHandler(h HandlerFunc) Option {
	return internal.WithMethodNotAllowedHandler(h)
}

// WithHealthChecks enables health check endpoints with optional configuration.
// Liveness (/health/live): Always returns OK if process is running.
// Readiness (/health/ready): Runs all configured checks.
//
// Example:
//
//	forge.WithHealthChecks(
//	    forge.WithReadinessCheck("db", db.Healthcheck(pool)),
//	)
func WithHealthChecks(opts ...HealthOption) Option {
	return internal.WithHealthChecks(opts...)
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
func WithLogger(component string, extractors ...ContextExtractor) Option {
	return internal.WithLogger(component, extractors...)
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
	return internal.WithCustomLogger(l)
}

// Health check options

// WithLivenessPath sets a custom liveness endpoint path.
// Defaults to "/health/live".
func WithLivenessPath(path string) HealthOption {
	return internal.WithLivenessPath(path)
}

// WithReadinessPath sets a custom readiness endpoint path.
// Defaults to "/health/ready".
func WithReadinessPath(path string) HealthOption {
	return internal.WithReadinessPath(path)
}

// WithReadinessCheck adds a named readiness check.
// Checks run in parallel during readiness probe.
func WithReadinessCheck(name string, fn health.CheckFunc) HealthOption {
	return internal.WithReadinessCheck(name, fn)
}

// Run options

// Address sets the HTTP server address.
// Defaults to ":8080".
func Address(addr string) RunOption {
	return internal.Address(addr)
}

// Logger sets the application logger.
// If nil, logging is disabled.
func Logger(l *slog.Logger) RunOption {
	return internal.Logger(l)
}

// ShutdownTimeout sets the timeout for graceful shutdown.
// This applies to both the HTTP server and shutdown hooks.
// Defaults to 30 seconds.
func ShutdownTimeout(d time.Duration) RunOption {
	return internal.ShutdownTimeout(d)
}

// ShutdownHook registers a cleanup function to run during shutdown.
// Hooks are called in the order they were registered.
// Each hook receives a context with the shutdown timeout.
//
// Example:
//
//	forge.ShutdownHook(db.Shutdown(pool))
func ShutdownHook(fn func(context.Context) error) RunOption {
	return internal.ShutdownHook(fn)
}

// Domain maps a host pattern to an App.
// Patterns: "api.example.com" (exact) or "*.example.com" (wildcard)
//
// Example:
//
//	forge.Run(
//	    forge.Domain("api.acme.com", apiApp),
//	    forge.Domain("*.acme.com", tenantApp),
//	)
func Domain(pattern string, app *App) RunOption {
	return internal.Domain(pattern, app)
}

// Fallback sets the default App for requests that don't match any domain.
// If no domains are configured, the fallback becomes the main handler.
//
// Example:
//
//	forge.Run(
//	    forge.Domain("api.acme.com", apiApp),
//	    forge.Fallback(landingApp),
//	)
func Fallback(app *App) RunOption {
	return internal.Fallback(app)
}

// WithContext sets a custom base context for signal handling.
// Useful for testing or when integrating with existing context hierarchies.
// Defaults to context.Background() if not set.
func WithContext(ctx context.Context) RunOption {
	return internal.WithContext(ctx)
}

// Context helpers

// ContextValue retrieves a typed value from the context.
// Returns the zero value of T if the key is not found or type assertion fails.
//
// Example:
//
//	type tenantKey struct{}
//
//	tenant := forge.ContextValue[string](c, tenantKey{})
//	user := forge.ContextValue[*User](c, userKey{})
func ContextValue[T any](c Context, key any) T {
	if v, ok := c.Get(key).(T); ok {
		return v
	}
	var zero T
	return zero
}
