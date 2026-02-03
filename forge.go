package forge

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/internal"
	"github.com/dmitrymomot/forge/pkg/cookie"
	"github.com/dmitrymomot/forge/pkg/health"
	"github.com/dmitrymomot/forge/pkg/job"
	"github.com/dmitrymomot/forge/pkg/logger"
	"github.com/dmitrymomot/forge/pkg/session"
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

	// CookieOption configures the cookie manager.
	CookieOption = cookie.Option

	// SessionOption configures the session manager.
	SessionOption = internal.SessionOption

	// Session represents a user session.
	Session = session.Session

	// SessionStore defines the interface for session persistence.
	SessionStore = session.Store

	// ResponseWriter wraps http.ResponseWriter with hooks and HTMX support.
	ResponseWriter = internal.ResponseWriter

	// JobOption configures the job manager.
	JobOption = job.Option

	// EnqueueOption configures job enqueueing.
	EnqueueOption = job.EnqueueOption

	// EnqueuerOption configures the job enqueuer.
	EnqueuerOption = job.EnqueuerOption

	// JobManager handles background job processing.
	JobManager = job.Manager

	// JobEnqueuer provides job enqueueing without worker processing.
	JobEnqueuer = job.Enqueuer
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

// WithBaseDomain configures the base domain for subdomain extraction.
// This enables c.Subdomain() to work without parameters.
//
// Example:
//
//	forge.New(
//	    forge.WithBaseDomain("example.com"),
//	)
func WithBaseDomain(domain string) Option {
	return internal.WithBaseDomain(domain)
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
func WithCookieOptions(opts ...CookieOption) Option {
	return internal.WithCookieOptions(opts...)
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

// StartupHook registers a function to run during server startup.
// Hooks are called in the order they were registered, after the port is bound
// but before serving requests. If any hook fails, the server stops and
// returns the error.
//
// Example:
//
//	forge.StartupHook(worker.Start)
func StartupHook(fn func(context.Context) error) RunOption {
	return internal.StartupHook(fn)
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

// Cookie options

// WithCookieSecret sets the secret for signing and encryption.
// Must be at least 32 bytes.
func WithCookieSecret(secret string) CookieOption {
	return cookie.WithSecret(secret)
}

// WithCookieDomain sets the cookie domain.
func WithCookieDomain(domain string) CookieOption {
	return cookie.WithDomain(domain)
}

// WithCookiePath sets the cookie path.
func WithCookiePath(path string) CookieOption {
	return cookie.WithPath(path)
}

// WithCookieSecure sets the Secure flag.
func WithCookieSecure(secure bool) CookieOption {
	return cookie.WithSecure(secure)
}

// WithCookieHTTPOnly sets the HttpOnly flag.
func WithCookieHTTPOnly(httpOnly bool) CookieOption {
	return cookie.WithHTTPOnly(httpOnly)
}

// WithCookieSameSite sets the SameSite attribute.
func WithCookieSameSite(ss http.SameSite) CookieOption {
	return cookie.WithSameSite(ss)
}

// Cookie errors for checking return values.
var (
	ErrCookieNotFound  = cookie.ErrNotFound
	ErrCookieNoSecret  = cookie.ErrNoSecret
	ErrCookieBadSecret = cookie.ErrBadSecret
	ErrCookieBadSig    = cookie.ErrBadSig
	ErrCookieDecrypt   = cookie.ErrDecrypt
)

// Session options

// WithSession enables server-side session management.
// A SessionStore implementation must be provided (e.g., PostgresStore).
// Sessions are loaded lazily and saved automatically before the response is written.
//
// Example:
//
//	pgStore := postgres.NewSessionStore(pool)
//	forge.New(
//	    forge.WithSession(pgStore,
//	        forge.WithSessionCookieName("__sid"),
//	        forge.WithSessionMaxAge(86400 * 30),
//	    ),
//	)
func WithSession(store SessionStore, opts ...SessionOption) Option {
	return internal.WithSession(store, opts...)
}

// WithSessionCookieName sets the session cookie name.
// Defaults to "__sid".
func WithSessionCookieName(name string) SessionOption {
	return internal.WithSessionCookieName(name)
}

// WithSessionMaxAge sets the session max age in seconds.
// Defaults to 30 days.
func WithSessionMaxAge(seconds int) SessionOption {
	return internal.WithSessionMaxAge(seconds)
}

// WithSessionDomain sets the session cookie domain.
func WithSessionDomain(domain string) SessionOption {
	return internal.WithSessionDomain(domain)
}

// WithSessionPath sets the session cookie path.
// Defaults to "/".
func WithSessionPath(path string) SessionOption {
	return internal.WithSessionPath(path)
}

// WithSessionSecure sets the session cookie Secure flag.
// Defaults to false (should be true in production with HTTPS).
func WithSessionSecure(secure bool) SessionOption {
	return internal.WithSessionSecure(secure)
}

// WithSessionHTTPOnly sets the session cookie HttpOnly flag.
// Defaults to true (recommended for security).
func WithSessionHTTPOnly(httpOnly bool) SessionOption {
	return internal.WithSessionHTTPOnly(httpOnly)
}

// WithSessionSameSite sets the session cookie SameSite attribute.
// Defaults to SameSiteLaxMode.
func WithSessionSameSite(sameSite http.SameSite) SessionOption {
	return internal.WithSessionSameSite(sameSite)
}

// WithSessionFingerprint enables device fingerprinting for session hijacking detection.
// The session manager automatically uses the app's logger for warnings.
//
// Mode determines which components are included in the fingerprint:
//   - FingerprintCookie: Default, excludes IP (recommended for most apps)
//   - FingerprintJWT: Minimal, excludes Accept headers (for JWT apps)
//   - FingerprintHTMX: User-Agent only (for HTMX apps)
//   - FingerprintStrict: Includes IP (high-security, causes false positives)
//
// Strictness determines behavior on mismatch:
//   - FingerprintWarn: Log warning but allow session (visibility without disruption)
//   - FingerprintReject: Invalidate session (strict security)
//
// Example:
//
//	forge.New(
//	    forge.WithSession(store,
//	        forge.WithSessionFingerprint(forge.FingerprintCookie, forge.FingerprintReject),
//	    ),
//	)
func WithSessionFingerprint(mode FingerprintMode, strictness FingerprintStrictness) SessionOption {
	return internal.WithSessionFingerprint(mode, strictness)
}

// Fingerprint types for session configuration.
type (
	// FingerprintMode determines which fingerprint generation algorithm to use.
	FingerprintMode = internal.FingerprintMode

	// FingerprintStrictness determines behavior on fingerprint mismatch.
	FingerprintStrictness = internal.FingerprintStrictness
)

// Fingerprint mode constants.
const (
	// FingerprintDisabled disables fingerprint generation and validation.
	FingerprintDisabled = internal.FingerprintDisabled
	// FingerprintCookie uses default settings, excludes IP. Best for most web apps.
	FingerprintCookie = internal.FingerprintCookie
	// FingerprintJWT uses minimal fingerprint, excludes Accept headers.
	FingerprintJWT = internal.FingerprintJWT
	// FingerprintHTMX uses only User-Agent, avoids HTMX header variations.
	FingerprintHTMX = internal.FingerprintHTMX
	// FingerprintStrict includes IP address. WARNING: causes false positives.
	FingerprintStrict = internal.FingerprintStrict
)

// Fingerprint strictness constants.
const (
	// FingerprintWarn logs a warning but allows the session to continue.
	FingerprintWarn = internal.FingerprintWarn
	// FingerprintReject invalidates the session on fingerprint mismatch.
	FingerprintReject = internal.FingerprintReject
)

// Session errors for checking return values.
var (
	ErrSessionNotConfigured       = session.ErrNotConfigured
	ErrSessionNotFound            = session.ErrNotFound
	ErrSessionExpired             = session.ErrExpired
	ErrSessionInvalidToken        = session.ErrInvalidToken
	ErrSessionFingerprintMismatch = session.ErrFingerprintMismatch
)

// Job options

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
//	    ),
//	)
func WithJobs(pool *pgxpool.Pool, opts ...JobOption) Option {
	return internal.WithJobs(pool, opts...)
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
	return internal.WithJobEnqueuer(pool, opts...)
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
func WithJobWorker(pool *pgxpool.Pool, opts ...JobOption) Option {
	return internal.WithJobWorker(pool, opts...)
}

// Job registration options - re-exported from pkg/job

// WithTask registers a task handler using structural typing.
// The task must implement Name() and Handle(ctx, P) methods.
func WithTask[P any, T interface {
	Name() string
	Handle(context.Context, P) error
}](task T) JobOption {
	return job.WithTask[P, T](task)
}

// WithScheduledTask registers a periodic task.
// The task must implement Name(), Schedule(), and Handle(ctx) methods.
func WithScheduledTask[T interface {
	Name() string
	Schedule() string
	Handle(context.Context) error
}](task T) JobOption {
	return job.WithScheduledTask[T](task)
}

// WithJobQueue configures a named queue with the specified number of workers.
func WithJobQueue(name string, workers int) JobOption {
	return job.WithQueue(name, workers)
}

// WithJobLogger sets the logger for job processing.
func WithJobLogger(l *slog.Logger) JobOption {
	return job.WithLogger(l)
}

// WithJobMaxWorkers sets the default maximum number of workers.
func WithJobMaxWorkers(n int) JobOption {
	return job.WithMaxWorkers(n)
}

// Enqueue options - re-exported from pkg/job

// InQueue specifies which queue to use for the job.
func InQueue(name string) EnqueueOption {
	return job.InQueue(name)
}

// ScheduledAt schedules the job to run at a specific time.
func ScheduledAt(t time.Time) EnqueueOption {
	return job.ScheduledAt(t)
}

// ScheduledIn schedules the job to run after a duration.
func ScheduledIn(d time.Duration) EnqueueOption {
	return job.ScheduledIn(d)
}

// MaxAttempts sets the maximum number of retry attempts for the job.
func MaxAttempts(n int) EnqueueOption {
	return job.MaxAttempts(n)
}

// UniqueFor ensures only one job with this key exists for the specified duration.
func UniqueFor(d time.Duration) EnqueueOption {
	return job.UniqueFor(d)
}

// UniqueKey sets a custom unique key for deduplication.
func UniqueKey(key string) EnqueueOption {
	return job.UniqueKey(key)
}

// JobPriority sets the job priority (lower numbers = higher priority).
func JobPriority(p int) EnqueueOption {
	return job.Priority(p)
}

// JobTags adds metadata tags to the job.
func JobTags(tags ...string) EnqueueOption {
	return job.Tags(tags...)
}

// Job errors for checking return values.
var (
	ErrJobNotConfigured     = job.ErrNotConfigured
	ErrJobUnknownTask       = job.ErrUnknownTask
	ErrJobInvalidPayload    = job.ErrInvalidPayload
	ErrJobHealthcheckFailed = job.ErrHealthcheckFailed
	ErrJobPoolRequired      = job.ErrPoolRequired
)

// JobHealthcheck returns a health check function for the job manager.
func JobHealthcheck(m *JobManager) health.CheckFunc {
	return job.Healthcheck(m)
}

// SessionValue is a typed helper to retrieve session values with type safety.
// Returns an error if the key doesn't exist or type assertion fails.
//
// Example:
//
//	theme, err := forge.SessionValue[string](sess, "theme")
func SessionValue[T any](sess *Session, key string) (T, error) {
	return session.Value[T](sess, key)
}

// SessionValueOr is a typed helper that returns a default value if the key
// doesn't exist or type assertion fails.
//
// Example:
//
//	theme := forge.SessionValueOr(sess, "theme", "light")
func SessionValueOr[T any](sess *Session, key string, defaultVal T) T {
	return session.ValueOr(sess, key, defaultVal)
}
