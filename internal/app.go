package internal

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dmitrymomot/forge/pkg/cookie"
	"github.com/dmitrymomot/forge/pkg/health"
	"github.com/dmitrymomot/forge/pkg/logger"
	"github.com/dmitrymomot/forge/pkg/storage"
)

// Default server timeouts (hardcoded, opinionated).
const (
	defaultReadTimeout       = 15 * time.Second
	defaultWriteTimeout      = 30 * time.Second
	defaultIdleTimeout       = 120 * time.Second
	defaultReadHeaderTimeout = 5 * time.Second
	defaultMaxHeaderBytes    = 1 << 20 // 1MB
	defaultShutdownTimeout   = 30 * time.Second
)

// App orchestrates the application lifecycle.
// It manages HTTP routing, middleware, and graceful shutdown.
// App is immutable after creation - all configuration is done via New().
type App struct {
	router                  chi.Router
	errorHandler            ErrorHandler
	notFoundHandler         HandlerFunc
	methodNotAllowedHandler HandlerFunc
	healthConfig            *healthConfig
	logger                  *slog.Logger
	cookieManager           *cookie.Manager
	sessionManager          *SessionManager
	jobEnqueuer             *JobEnqueuer
	jobWorker               *JobManager
	storage                 storage.Storage
	baseDomain              string
	middlewares             []Middleware
	handlers                []Handler
	staticRoutes            []staticRoute
}

// staticRoute represents a static file handler mount point.
type staticRoute struct {
	handler http.Handler
	pattern string
}

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
func New(opts ...Option) *App {
	a := &App{
		router:        chi.NewRouter(),
		logger:        logger.NewNope(), // Default: noop logger (before options)
		cookieManager: cookie.New(),     // Default: cookie manager (no secret)
	}

	for _, opt := range opts {
		opt(a)
	}

	// Inject app's logger into session manager
	if a.sessionManager != nil {
		a.sessionManager.SetLogger(a.logger)
	}

	a.setupRoutes()
	return a
}

// Router returns the underlying chi.Router for the App.
// This is used internally for composing multi-domain routing.
func (a *App) Router() chi.Router {
	return a.router
}

// JobWorker returns the job worker if configured, nil otherwise.
// This is used internally for multi-domain routing to collect workers.
func (a *App) JobWorker() *JobManager {
	return a.jobWorker
}

// Run starts a single-domain HTTP server and blocks until shutdown.
// This is a convenience method for the common single-app case.
// If job workers are configured, they start automatically before serving
// requests and stop gracefully during shutdown.
//
// Example:
//
//	app := forge.New(
//	    forge.WithHandlers(handlers.NewLandingHandler()),
//	)
//	err := app.Run(":8080", forge.Logger(slog))
func (a *App) Run(addr string, opts ...RunOption) error {
	cfg := buildRunConfig(opts...)

	startupHooks := cfg.startupHooks
	shutdownHooks := cfg.shutdownHooks

	// Auto-register worker hooks if configured
	if a.jobWorker != nil {
		startupHooks = append([]func(context.Context) error{a.jobWorker.Manager().StartFunc()}, startupHooks...)
		shutdownHooks = append(shutdownHooks, a.jobWorker.Shutdown())
	}

	return runServer(runtimeConfig{
		handler:         a.router,
		address:         addr,
		logger:          cfg.logger,
		shutdownTimeout: cfg.shutdownTimeout,
		startupHooks:    startupHooks,
		shutdownHooks:   shutdownHooks,
		baseCtx:         cfg.baseCtx,
	})
}

// setupRoutes configures the router with middleware and handlers.
func (a *App) setupRoutes() {
	// Set custom error handlers on chi router
	if a.notFoundHandler != nil {
		a.router.NotFound(a.wrapHandler(a.notFoundHandler))
	}
	if a.methodNotAllowedHandler != nil {
		a.router.MethodNotAllowed(a.wrapHandler(a.methodNotAllowedHandler))
	}

	// Apply global middleware
	for _, mw := range a.middlewares {
		a.router.Use(a.adaptMiddleware(mw))
	}

	// Mount static file handlers
	for _, sr := range a.staticRoutes {
		a.router.Mount(sr.pattern, sr.handler)
	}

	// Register health check endpoints
	if a.healthConfig != nil {
		a.router.Get(a.healthConfig.livenessPath, health.LivenessHandler())
		a.router.Get(a.healthConfig.readinessPath, health.ReadinessHandler(a.healthConfig.checks))
	}

	// Register handlers
	r := &routerAdapter{router: a.router, app: a}
	for _, h := range a.handlers {
		h.Routes(r)
	}
}

// wrapHandler converts a HandlerFunc to http.HandlerFunc using the app's error handler.
func (a *App) wrapHandler(h HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := newContext(w, r, a.logger, a.cookieManager, a.sessionManager, a.jobEnqueuer, a.storage, a.baseDomain)
		if err := h(c); err != nil {
			a.handleError(c, err)
		}
	}
}

// handleError handles errors from handlers using the configured error handler.
func (a *App) handleError(c Context, err error) {
	// Check if response has already been written
	if c.Written() {
		return
	}
	if a.errorHandler != nil {
		_ = a.errorHandler(c, err)
	} else {
		http.Error(c.Response(), "Internal Server Error", http.StatusInternalServerError)
	}
}

// healthConfig holds health check endpoint configuration.
type healthConfig struct {
	checks        health.Checks
	livenessPath  string
	readinessPath string
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
