package internal

import (
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/pkg/cookie"
	"github.com/dmitrymomot/forge/pkg/job"
	"github.com/dmitrymomot/forge/pkg/logger"
	"github.com/dmitrymomot/forge/pkg/session"
	"github.com/dmitrymomot/forge/pkg/storage"
)

// Option configures the application.
type Option func(*App)

// WithBaseDomain configures the base domain for subdomain extraction.
// This enables c.Subdomain() to work without parameters.
//
// Example:
//
//	forge.New(
//	    forge.WithBaseDomain("example.com"),
//	)
func WithBaseDomain(domain string) Option {
	return func(a *App) {
		a.baseDomain = domain
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

		a.staticRoutes = append(a.staticRoutes, staticRoute{handler, pattern})
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
			checks:        make(healthChecks),
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

// WithSession enables server-side session management.
// A session.Store implementation must be provided (e.g., PostgresStore).
// Sessions are loaded lazily and saved automatically before the response is written.
//
// Example:
//
//	pgStore := postgres.NewSessionStore(pool)
//	forge.New(
//	    forge.WithSession(pgStore,
//	        forge.WithSessionCookieName("__sid"),
//	        forge.WithSessionMaxAge(86400 * 30),
//	        forge.WithSessionSecure(true),
//	    ),
//	)
func WithSession(store session.Store, opts ...SessionOption) Option {
	return func(a *App) {
		a.sessionManager = NewSessionManager(store, opts...)
	}
}

// WithJobs enables both job enqueueing and worker processing using River.
// A pgxpool.Pool is required for the job queue. Workers are started automatically
// when the app runs and stopped gracefully during shutdown.
// Use this for monolith deployments or workers that need to enqueue follow-up tasks.
//
// Example:
//
//	forge.New(
//	    forge.WithJobs(pool,
//	        job.WithTask(tasks.NewSendWelcome(mailer, repo)),
//	        job.WithScheduledTask(tasks.NewCleanupSessions(repo)),
//	        job.WithQueue("email", 10),
//	        job.WithLogger(slog.Default()),
//	    ),
//	)
func WithJobs(pool *pgxpool.Pool, opts ...job.Option) Option {
	return func(a *App) {
		jm, err := NewJobManager(pool, opts...)
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
//
// Example:
//
//	// Web server - only enqueues jobs
//	forge.New(
//	    forge.WithJobEnqueuer(pool),
//	)
//	// c.Enqueue("send_email", payload) works
func WithJobEnqueuer(pool *pgxpool.Pool, opts ...job.EnqueuerOption) Option {
	return func(a *App) {
		je, err := NewJobEnqueuer(pool, opts...)
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
//
// If workers need to enqueue follow-up tasks, use WithJobs instead.
//
// Example:
//
//	// Dedicated worker process
//	forge.New(
//	    forge.WithJobWorker(pool,
//	        job.WithTask(tasks.NewSendEmail(mailer)),
//	        job.WithScheduledTask(tasks.NewCleanup(repo)),
//	    ),
//	)
//	// c.Enqueue() returns job.ErrNotConfigured
func WithJobWorker(pool *pgxpool.Pool, opts ...job.Option) Option {
	return func(a *App) {
		jm, err := NewJobManager(pool, opts...)
		if err != nil {
			panic(fmt.Sprintf("job worker: %v", err))
		}
		a.jobWorker = jm
		// Note: jobEnqueuer stays nil - c.Enqueue() returns ErrNotConfigured
	}
}

// WithRoles configures role-based access control for the application.
// The permissions map defines which permissions each role grants.
// The extractor function determines the current user's role from the request context.
// Roles are extracted lazily (once per request) and cached.
//
// Example:
//
//	forge.New(
//	    forge.WithRoles(
//	        forge.RolePermissions{
//	            "admin":  {"users.read", "users.write", "billing.manage"},
//	            "member": {"users.read"},
//	        },
//	        func(c forge.Context) string {
//	            return c.Get(roleKey{}).(string)
//	        },
//	    ),
//	)
func WithRoles(permissions RolePermissions, extractor RoleExtractorFunc) Option {
	return func(a *App) {
		a.rolePermissions = permissions
		a.roleExtractor = extractor
	}
}

// WithStorage configures file storage for the application.
// A storage.Storage implementation must be provided (e.g., S3Client).
// Enables c.Upload(), c.Download(), c.DeleteFile(), and c.FileURL().
//
// Example:
//
//	s3 := storage.NewS3Client(storage.Config{
//	    Bucket:    "my-bucket",
//	    AccessKey: os.Getenv("AWS_ACCESS_KEY"),
//	    SecretKey: os.Getenv("AWS_SECRET_KEY"),
//	})
//	forge.New(
//	    forge.WithStorage(s3),
//	)
func WithStorage(s storage.Storage) Option {
	return func(a *App) {
		a.storage = s
	}
}
