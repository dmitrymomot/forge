package internal

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/pkg/cookie"
	"github.com/dmitrymomot/forge/pkg/job"
	"github.com/dmitrymomot/forge/pkg/logger"
	"github.com/dmitrymomot/forge/pkg/storage"
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

		a.staticRoutes = append(a.staticRoutes, staticRoute{handler, pattern})
	}
}

// WithErrorHandler sets a custom error handler for handler errors.
// Called when a handler returns a non-nil error.
func WithErrorHandler(h ErrorHandler) Option {
	return func(a *App) {
		a.errorHandler = h
	}
}

// WithNotFoundHandler sets a custom 404 handler.
func WithNotFoundHandler(h HandlerFunc) Option {
	return func(a *App) {
		a.notFoundHandler = h
	}
}

// WithMethodNotAllowedHandler sets a custom 405 handler.
func WithMethodNotAllowedHandler(h HandlerFunc) Option {
	return func(a *App) {
		a.methodNotAllowedHandler = h
	}
}

// WithHealthChecks enables health check endpoints with hardcoded paths.
// Liveness: /_live — always returns OK if process is running.
// Readiness: /_ready — runs all configured checks.
func WithHealthChecks(checks ...HealthCheckOption) Option {
	return func(a *App) {
		hc := &healthConfig{
			livenessPath:  "/_live",
			readinessPath: "/_ready",
			checks:        make(healthChecks),
		}
		for _, c := range checks {
			c(hc.checks)
		}
		a.healthConfig = hc
	}
}

// WithLogger creates a logger with a component name and optional extractors.
// The component name is added to every log entry for easy filtering.
// Extractors pull values from context (e.g., request_id, user_id).
func WithLogger(component string, extractors ...logger.ContextExtractor) Option {
	return func(a *App) {
		a.logger = logger.New(extractors...).With("component", component)
	}
}

// WithCustomLogger sets a fully custom logger.
// Use this when you need complete control over logging configuration.
func WithCustomLogger(l *slog.Logger) Option {
	return func(a *App) {
		if l != nil {
			a.logger = l
		}
	}
}

// WithCookieConfig configures the cookie manager.
func WithCookieConfig(cfg cookie.Config) Option {
	return func(a *App) {
		cm, err := cookie.New(cfg)
		if err != nil {
			panic(fmt.Sprintf("cookie: %v", err))
		}
		a.cookieManager = cm
	}
}

// WithSession enables server-side session management.
// A Store implementation must be provided (e.g., PostgresStore).
// Sessions are loaded lazily and saved automatically before the response is written.
// Additional configuration can be provided via SessionOption functions.
func WithSession(store Store, opts ...SessionOption) Option {
	return func(a *App) {
		// Build config with defaults
		cfg := &sessionConfig{
			ttl:                   defaultSessionTTL,
			maxSessionsPerUser:    defaultMaxSessionsPerUser,
			touchThreshold:        defaultTouchThreshold,
			cookieName:            defaultCookieName,
			fingerprintMode:       FingerprintCookie,
			fingerprintStrictness: FingerprintWarn,
		}

		// Apply options
		for _, opt := range opts {
			opt(cfg)
		}

		if cfg.maxSessionsPerUser < 1 {
			cfg.maxSessionsPerUser = 1
		}
		if cfg.maxSessionsPerUser > maxAllowedSessionsPerUser {
			cfg.maxSessionsPerUser = maxAllowedSessionsPerUser
		}

		a.sessionStore = store
		a.sessionConfig = cfg
	}
}

// WithJobs enables both job enqueueing and worker processing.
// A job.Driver is required for the job queue backend. Workers are started
// automatically when the app runs and stopped gracefully during shutdown.
func WithJobs(driver job.Driver, cfg job.Config, opts ...job.Option) Option {
	return func(a *App) {
		if err := driver.Migrate(context.Background()); err != nil {
			panic(fmt.Sprintf("job migrate: %v", err))
		}
		jm, err := NewJobManager(driver, cfg, opts...)
		if err != nil {
			panic(fmt.Sprintf("job manager: %v", err))
		}
		a.jobEnqueuer = &JobEnqueuer{enqueuer: jm.Manager().Enqueuer}
		a.jobWorker = jm
	}
}

// WithJobEnqueuer enables job enqueueing without worker processing.
// Use this for web servers that dispatch work to separate worker processes.
// Workers must be running elsewhere to process the enqueued jobs.
func WithJobEnqueuer(driver job.Driver, opts ...job.EnqueuerOption) Option {
	return func(a *App) {
		if err := driver.Migrate(context.Background()); err != nil {
			panic(fmt.Sprintf("job migrate: %v", err))
		}
		je, err := NewJobEnqueuer(driver, opts...)
		if err != nil {
			panic(fmt.Sprintf("job enqueuer: %v", err))
		}
		a.jobEnqueuer = je
	}
}

// WithJobWorker enables job processing without enqueueing capability.
// Use this for dedicated background worker processes that don't need
// to dispatch additional jobs. Workers are started automatically when
// the app runs and stopped gracefully during shutdown.
func WithJobWorker(driver job.Driver, cfg job.Config, opts ...job.Option) Option {
	return func(a *App) {
		if err := driver.Migrate(context.Background()); err != nil {
			panic(fmt.Sprintf("job migrate: %v", err))
		}
		jm, err := NewJobManager(driver, cfg, opts...)
		if err != nil {
			panic(fmt.Sprintf("job worker: %v", err))
		}
		a.jobWorker = jm
	}
}

// WithRoles configures role-based access control for the application.
// The permissions map defines which permissions each role grants.
// The extractor function determines the current user's role from the request context.
// Roles are extracted lazily (once per request) and cached.
func WithRoles(permissions RolePermissions, extractor RoleExtractorFunc) Option {
	return func(a *App) {
		a.rolePermissions = permissions
		a.roleExtractor = extractor
	}
}

// WithSSEKeepAlive sets the interval for SSE keepalive comments.
// Defaults to 30 seconds if not set or if d <= 0.
func WithSSEKeepAlive(d time.Duration) Option {
	return func(a *App) {
		a.sseKeepAlive = d
	}
}

// WithStorage configures file storage for the application.
// A storage.Storage implementation must be provided (e.g., S3Client).
// Enables c.Upload(), c.UploadFromURL(), c.Download(), c.DeleteFile(), and c.FileURL().
func WithStorage(s storage.Storage) Option {
	return func(a *App) {
		a.storage = s
	}
}
