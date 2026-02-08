package internal

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dmitrymomot/forge/pkg/cookie"
	"github.com/dmitrymomot/forge/pkg/htmx"
	"github.com/dmitrymomot/forge/pkg/logger"
	"github.com/dmitrymomot/forge/pkg/storage"
)

const (
	defaultReadTimeout       = 15 * time.Second
	defaultWriteTimeout      = 30 * time.Second
	defaultIdleTimeout       = 120 * time.Second
	defaultReadHeaderTimeout = 5 * time.Second
	defaultMaxHeaderBytes    = 1 << 20 // 1MB
	defaultShutdownTimeout   = 30 * time.Second
)

// AppConfig holds externally configurable application settings.
type AppConfig struct {
	BaseDomain     string        `env:"BASE_DOMAIN"`
	RequestTimeout time.Duration `env:"REQUEST_TIMEOUT"`
}

// HealthCheckOption adds a readiness check to the health configuration.
type HealthCheckOption func(healthChecks)

// HealthCheck creates a named readiness check option.
func HealthCheck(name string, fn CheckFunc) HealthCheckOption {
	return func(checks healthChecks) {
		checks[name] = fn
	}
}

// App orchestrates the application lifecycle.
// It manages HTTP routing, middleware, and graceful shutdown.
// App is immutable after creation - all configuration is done via New().
type App struct {
	router                  chi.Router
	storage                 storage.Storage
	errorHandler            ErrorHandler
	notFoundHandler         HandlerFunc
	methodNotAllowedHandler HandlerFunc
	roleExtractor           RoleExtractorFunc
	rolePermissions         RolePermissions
	healthConfig            *healthConfig
	logger                  *slog.Logger
	cookieManager           *cookie.Manager
	sessionStore            Store          // Direct store reference
	sessionConfig           *sessionConfig // Session configuration
	jobEnqueuer             *JobEnqueuer
	jobWorker               *JobManager
	baseDomain              string
	middlewares             []Middleware
	handlers                []Handler
	staticRoutes            []staticRoute
	requestTimeout          time.Duration
}

// staticRoute represents a static file handler mount point.
type staticRoute struct {
	handler http.Handler
	pattern string
}

// New creates a new application with the given config and options.
// The App is immutable after creation.
func New(cfg AppConfig, opts ...Option) *App {
	cm, _ := cookie.New(cookie.Config{})
	a := &App{
		router:         chi.NewRouter(),
		logger:         logger.NewNope(),
		cookieManager:  cm,
		baseDomain:     cfg.BaseDomain,
		requestTimeout: cfg.RequestTimeout,
	}

	for _, opt := range opts {
		opt(a)
	}

	// Note: Session manager logger should be configured via WithSessionLogger option

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
func (a *App) Run(cfg RunConfig, opts ...RunOption) error {
	rc := buildRunConfig(cfg, opts...)

	startupHooks := rc.startupHooks
	shutdownHooks := rc.shutdownHooks

	// Auto-register worker hooks if configured
	if a.jobWorker != nil {
		startupHooks = append([]func(context.Context) error{a.jobWorker.Manager().StartFunc()}, startupHooks...)
		shutdownHooks = append(shutdownHooks, a.jobWorker.Shutdown())
	}

	return runServer(runtimeConfig{
		handler:         a.router,
		address:         rc.address,
		logger:          rc.logger,
		shutdownTimeout: rc.shutdownTimeout,
		startupHooks:    startupHooks,
		shutdownHooks:   shutdownHooks,
		baseCtx:         rc.baseCtx,
	})
}

func (a *App) setupRoutes() {
	// Set custom error handlers on chi router
	if a.notFoundHandler != nil {
		a.router.NotFound(a.wrapHandler(a.notFoundHandler))
	}
	if a.methodNotAllowedHandler != nil {
		a.router.MethodNotAllowed(a.wrapHandler(a.methodNotAllowedHandler))
	}

	// Apply request deadline before all other middleware.
	// Operations using the context (DB queries, HTTP clients) unblock
	// on deadline and return context.DeadlineExceeded naturally.
	if a.requestTimeout > 0 {
		a.router.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx, cancel := context.WithTimeout(r.Context(), a.requestTimeout)
				defer cancel()
				next.ServeHTTP(w, r.WithContext(ctx))
			})
		})
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
		a.router.Get(a.healthConfig.livenessPath, livenessHandler())
		a.router.Get(a.healthConfig.readinessPath, readinessHandler(a.healthConfig.checks))
	}

	// Register handlers
	r := &routerAdapter{router: a.router, app: a}
	for _, h := range a.handlers {
		h.Routes(r)
	}
}

func (a *App) wrapHandler(h HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rw := NewResponseWriter(w, htmx.IsHTMX(r))
		c := &requestContext{
			request:         r,
			response:        rw,
			responseWriter:  rw,
			app:             a, // Pass app reference for lazy sessionManager init
			logger:          a.logger,
			cookieManager:   a.cookieManager,
			storage:         a.storage,
			jobEnqueuer:     a.jobEnqueuer,
			baseDomain:      a.baseDomain,
			rolePermissions: a.rolePermissions,
			roleExtractor:   a.roleExtractor,
		}
		if err := h(c); err != nil {
			a.handleError(c, err)
		}
	}
}

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
	// Seal the writer to prevent any concurrent or deferred writes
	// from corrupting the response after the error has been written.
	if rw := c.ResponseWriter(); rw != nil {
		rw.Seal()
	}
}

// healthConfig holds health check endpoint configuration (internal).
type healthConfig struct {
	checks        healthChecks
	livenessPath  string
	readinessPath string
}
