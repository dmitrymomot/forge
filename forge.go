package forge

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/internal"
	"github.com/dmitrymomot/forge/middlewares"
	"github.com/dmitrymomot/forge/pkg/cookie"
	"github.com/dmitrymomot/forge/pkg/i18n"
	"github.com/dmitrymomot/forge/pkg/job"
	"github.com/dmitrymomot/forge/pkg/logger"
	"github.com/dmitrymomot/forge/pkg/session"
	"github.com/dmitrymomot/forge/pkg/storage"
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

	// CheckFunc is the standard health check function signature.
	CheckFunc = internal.CheckFunc

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

	// Storage defines the interface for file storage operations.
	Storage = storage.Storage

	// StorageConfig holds S3-compatible storage configuration.
	StorageConfig = storage.Config

	// FileInfo contains metadata about an uploaded file.
	FileInfo = storage.FileInfo

	// StorageOption configures Put operations.
	StorageOption = storage.Option

	// URLOption configures URL generation.
	URLOption = storage.URLOption

	// ACL represents access control levels for stored files.
	ACL = storage.ACL

	// ValidationRule defines a validation check for file uploads.
	ValidationRule = storage.ValidationRule

	// FileValidationError represents a file validation failure.
	FileValidationError = storage.FileValidationError

	// HTTPError represents an HTTP error with all data needed for rendering.
	HTTPError = internal.HTTPError

	// HTTPErrorOption configures an HTTPError.
	HTTPErrorOption = internal.HTTPErrorOption

	// Permission represents a named permission string.
	Permission = internal.Permission

	// RolePermissions maps role names to their granted permissions.
	RolePermissions = internal.RolePermissions

	// RoleExtractorFunc extracts the current user's role from the request context.
	RoleExtractorFunc = internal.RoleExtractorFunc

	// TranslatorKey is the context key used to store the i18n Translator.
	TranslatorKey = internal.TranslatorKey

	// LanguageKey is the context key used to store the resolved language string.
	LanguageKey = internal.LanguageKey

	// JWTClaimsKey is the context key used to store parsed JWT claims.
	JWTClaimsKey = internal.JWTClaimsKey

	// Extractor tries multiple sources in order and returns the first match.
	// Use with FromHeader, FromQuery, FromCookie, etc. to compose extraction chains.
	Extractor = internal.Extractor

	// ExtractorSource extracts a value from the request context.
	// Returns the value and true if found, or ("", false) if not present.
	ExtractorSource = internal.ExtractorSource
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
//	            return forge.ContextValue[string](c, roleKey{})
//	        },
//	    ),
//	)
func WithRoles(permissions RolePermissions, extractor RoleExtractorFunc) Option {
	return internal.WithRoles(permissions, extractor)
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
func WithReadinessCheck(name string, fn CheckFunc) HealthOption {
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
	return internal.ContextValue[T](c, key)
}

// Param retrieves a typed URL parameter from the request.
// Uses strconv for type conversion. Returns the zero value of T on parse error.
//
// Example:
//
//	id := forge.Param[int64](c, "id")
//	slug := forge.Param[string](c, "slug")
func Param[T ~string | ~int | ~int64 | ~float64 | ~bool](c Context, name string) T {
	return internal.Param[T](c, name)
}

// Query retrieves a typed query parameter from the request.
// Uses strconv for type conversion. Returns the zero value of T on parse error.
//
// Example:
//
//	page := forge.Query[int](c, "page")
//	verbose := forge.Query[bool](c, "verbose")
func Query[T ~string | ~int | ~int64 | ~float64 | ~bool](c Context, name string) T {
	return internal.Query[T](c, name)
}

// QueryDefault retrieves a typed query parameter with a default value.
// Returns defaultValue if the parameter is empty or cannot be parsed.
//
// Example:
//
//	page := forge.QueryDefault[int](c, "page", 1)
//	limit := forge.QueryDefault[int](c, "limit", 20)
func QueryDefault[T ~string | ~int | ~int64 | ~float64 | ~bool](c Context, name string, defaultValue T) T {
	return internal.QueryDefault[T](c, name, defaultValue)
}

// Extractor helpers

// NewExtractor creates an Extractor that tries the given sources in order.
// Returns the first non-empty value found.
//
// Example:
//
//	ext := forge.NewExtractor(
//	    forge.FromHeader("X-API-Key"),
//	    forge.FromQuery("api_key"),
//	    forge.FromCookie("api_key"),
//	)
//	value, ok := ext.Extract(c)
func NewExtractor(sources ...ExtractorSource) Extractor {
	return internal.NewExtractor(sources...)
}

// FromHeader returns an ExtractorSource that reads from a request header.
func FromHeader(name string) ExtractorSource {
	return internal.FromHeader(name)
}

// FromQuery returns an ExtractorSource that reads from a query parameter.
func FromQuery(name string) ExtractorSource {
	return internal.FromQuery(name)
}

// FromCookie returns an ExtractorSource that reads from a plain cookie.
func FromCookie(name string) ExtractorSource {
	return internal.FromCookie(name)
}

// FromCookieSigned returns an ExtractorSource that reads from a signed cookie.
func FromCookieSigned(name string) ExtractorSource {
	return internal.FromCookieSigned(name)
}

// FromCookieEncrypted returns an ExtractorSource that reads from an encrypted cookie.
func FromCookieEncrypted(name string) ExtractorSource {
	return internal.FromCookieEncrypted(name)
}

// FromParam returns an ExtractorSource that reads from a URL parameter.
func FromParam(name string) ExtractorSource {
	return internal.FromParam(name)
}

// FromForm returns an ExtractorSource that reads from a form field.
func FromForm(name string) ExtractorSource {
	return internal.FromForm(name)
}

// FromSession returns an ExtractorSource that reads from a session value.
// Tries string type assertion first, falls back to fmt.Sprint for non-string values.
func FromSession(key string) ExtractorSource {
	return internal.FromSession(key)
}

// FromBearerToken returns an ExtractorSource that reads a Bearer token
// from the Authorization header. Uses case-insensitive prefix matching.
func FromBearerToken() ExtractorSource {
	return internal.FromBearerToken()
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
func JobHealthcheck(m *JobManager) CheckFunc {
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

// Storage ACL constants.
const (
	// ACLPrivate makes the file accessible only via signed URLs.
	ACLPrivate = storage.ACLPrivate

	// ACLPublicRead makes the file publicly readable.
	ACLPublicRead = storage.ACLPublicRead
)

// Storage options

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
func WithStorage(s Storage) Option {
	return internal.WithStorage(s)
}

// Storage Put options - re-exported from pkg/storage

// WithStorageKey sets an explicit storage key, replacing the auto-generated ULID-based key.
func WithStorageKey(key string) StorageOption {
	return storage.WithKey(key)
}

// WithStoragePrefix sets a path prefix for the uploaded file.
func WithStoragePrefix(prefix string) StorageOption {
	return storage.WithPrefix(prefix)
}

// WithStorageTenant sets a tenant ID for multi-tenant isolation.
func WithStorageTenant(id string) StorageOption {
	return storage.WithTenant(id)
}

// WithStorageContentType overrides the auto-detected content type.
func WithStorageContentType(ct string) StorageOption {
	return storage.WithContentType(ct)
}

// WithStorageACL overrides the default ACL for this upload.
func WithStorageACL(acl ACL) StorageOption {
	return storage.WithACL(acl)
}

// WithStorageValidation adds validation rules to be applied before upload.
func WithStorageValidation(rules ...ValidationRule) StorageOption {
	return storage.WithValidation(rules...)
}

// Storage URL options - re-exported from pkg/storage

// WithURLExpiry sets the expiry duration for signed URLs.
func WithURLExpiry(d time.Duration) URLOption {
	return storage.WithExpiry(d)
}

// WithURLDownload sets the filename for Content-Disposition: attachment header.
func WithURLDownload(filename string) URLOption {
	return storage.WithDownload(filename)
}

// WithURLSigned forces a signed URL regardless of the file's ACL.
func WithURLSigned(expiry time.Duration) URLOption {
	return storage.WithSigned(expiry)
}

// WithURLPublic forces a public URL regardless of the file's ACL.
func WithURLPublic() URLOption {
	return storage.WithPublic()
}

// Storage validation rules - re-exported from pkg/storage

// MaxFileSize returns a rule that rejects files larger than the specified size.
func MaxFileSize(bytes int64) ValidationRule {
	return storage.MaxSize(bytes)
}

// MinFileSize returns a rule that rejects files smaller than the specified size.
func MinFileSize(bytes int64) ValidationRule {
	return storage.MinSize(bytes)
}

// FileNotEmpty returns a rule that rejects empty files.
func FileNotEmpty() ValidationRule {
	return storage.NotEmpty()
}

// AllowedFileTypes returns a rule that only accepts files matching the given MIME patterns.
// Supports wildcards like "image/*".
func AllowedFileTypes(patterns ...string) ValidationRule {
	return storage.AllowedTypes(patterns...)
}

// ImageFilesOnly returns a rule that only accepts image files.
func ImageFilesOnly() ValidationRule {
	return storage.ImageOnly()
}

// DocumentFilesOnly returns a rule that only accepts document files.
func DocumentFilesOnly() ValidationRule {
	return storage.DocumentsOnly()
}

// NewS3Storage creates a new S3-compatible storage client.
func NewS3Storage(cfg StorageConfig) (Storage, error) {
	return storage.New(cfg)
}

// Storage errors for checking return values.
var (
	ErrStorageNotConfigured  = storage.ErrNotConfigured
	ErrStorageInvalidConfig  = storage.ErrInvalidConfig
	ErrStorageEmptyFile      = storage.ErrEmptyFile
	ErrStorageFileTooLarge   = storage.ErrFileTooLarge
	ErrStorageFileTooSmall   = storage.ErrFileTooSmall
	ErrStorageInvalidMIME    = storage.ErrInvalidMIME
	ErrStorageNotFound       = storage.ErrNotFound
	ErrStorageAccessDenied   = storage.ErrAccessDenied
	ErrStorageUploadFailed   = storage.ErrUploadFailed
	ErrStorageDeleteFailed   = storage.ErrDeleteFailed
	ErrStoragePresignFailed  = storage.ErrPresignFailed
	ErrStorageInvalidURL     = storage.ErrInvalidURL
	ErrStorageDownloadFailed = storage.ErrDownloadFailed
)

// Middleware error types - re-exported from middlewares
type (
	// PanicError represents a recovered panic.
	PanicError = middlewares.PanicError

	// TimeoutError represents a request timeout.
	TimeoutError = middlewares.TimeoutError

	// TranslationMap is a map of placeholder keys to values for translation interpolation.
	TranslationMap = i18n.M

	// I18nOption configures the I18n middleware.
	I18nOption = middlewares.I18nOption

	// JWTOption configures the JWT middleware.
	JWTOption = middlewares.JWTOption

	// Translator provides a simplified translation interface with a fixed language and namespace context.
	Translator = i18n.Translator

	// LocaleFormat contains formatting rules for locale-specific formatting.
	LocaleFormat = i18n.LocaleFormat
)

// Middleware helpers - re-exported from middlewares

// GetRequestID extracts the request ID from the context.
// Returns an empty string if no request ID is set.
func GetRequestID(c Context) string {
	return middlewares.GetRequestID(c)
}

// RequestIDExtractor returns a ContextExtractor for use with WithLogger.
// Automatically adds "request_id" to all log entries.
func RequestIDExtractor() ContextExtractor {
	return middlewares.RequestIDExtractor()
}

// IsPanicError returns true if the error is a PanicError.
func IsPanicError(err error) bool {
	return middlewares.IsPanicError(err)
}

// IsTimeoutError returns true if the error is a TimeoutError.
func IsTimeoutError(err error) bool {
	return middlewares.IsTimeoutError(err)
}

// AsPanicError extracts the PanicError from an error if present.
func AsPanicError(err error) (*PanicError, bool) {
	return middlewares.AsPanicError(err)
}

// AsTimeoutError extracts the TimeoutError from an error if present.
func AsTimeoutError(err error) (*TimeoutError, bool) {
	return middlewares.AsTimeoutError(err)
}

// I18n middleware helpers

// GetTranslator extracts the Translator from the context.
// Returns nil if the I18n middleware is not used.
func GetTranslator(c Context) *Translator {
	return middlewares.GetTranslator(c)
}

// GetLanguage extracts the resolved language from the context.
// Returns an empty string if the I18n middleware is not used.
func GetLanguage(c Context) string {
	return middlewares.GetLanguage(c)
}

// T translates a key using the Translator stored in context by the I18n middleware.
// Returns the key itself if no translator is in context.
func T(c Context, key string, placeholders ...TranslationMap) string {
	tr := middlewares.GetTranslator(c)
	if tr == nil {
		return key
	}
	return tr.T(key, placeholders...)
}

// Tn translates a key with pluralization using the Translator stored in context.
// Returns the key itself if no translator is in context.
func Tn(c Context, key string, n int, placeholders ...TranslationMap) string {
	tr := middlewares.GetTranslator(c)
	if tr == nil {
		return key
	}
	return tr.Tn(key, n, placeholders...)
}

// FromAcceptLanguage returns an ExtractorSource that parses the Accept-Language
// header and matches against the available languages.
func FromAcceptLanguage(available []string) ExtractorSource {
	return middlewares.FromAcceptLanguage(available)
}

// JWT middleware helpers

// GetJWTClaims extracts parsed JWT claims from the context.
// Returns nil if the JWT middleware is not applied or the type doesn't match.
func GetJWTClaims[T any](c Context) *T {
	return middlewares.GetJWTClaims[T](c)
}

// WithJWTExtractor sets a custom token extractor for the JWT middleware.
func WithJWTExtractor(ext Extractor) JWTOption {
	return middlewares.WithJWTExtractor(ext)
}

// I18n middleware option constructors

// WithI18nNamespace sets the default namespace for the context translator.
func WithI18nNamespace(ns string) I18nOption {
	return middlewares.WithI18nNamespace(ns)
}

// WithI18nExtractor sets a custom language extractor chain.
func WithI18nExtractor(ext Extractor) I18nOption {
	return middlewares.WithI18nExtractor(ext)
}

// WithI18nFormatMap sets the language-to-format mapping.
func WithI18nFormatMap(m map[string]*LocaleFormat) I18nOption {
	return middlewares.WithI18nFormatMap(m)
}

// WithI18nDefaultFormat sets the fallback locale format.
func WithI18nDefaultFormat(f *LocaleFormat) I18nOption {
	return middlewares.WithI18nDefaultFormat(f)
}

// HTTPError constructors and options - re-exported from internal

// NewHTTPError creates a new HTTPError with the given status code and message.
func NewHTTPError(code int, message string) *HTTPError {
	return internal.NewHTTPError(code, message)
}

// WithTitle sets the error title.
func WithTitle(title string) HTTPErrorOption {
	return internal.WithTitle(title)
}

// WithDetail sets the extended description.
func WithDetail(detail string) HTTPErrorOption {
	return internal.WithDetail(detail)
}

// WithErrorCode sets the application-specific error code.
func WithErrorCode(code string) HTTPErrorOption {
	return internal.WithErrorCode(code)
}

// WithRequestID sets the request tracking ID.
func WithRequestID(id string) HTTPErrorOption {
	return internal.WithRequestID(id)
}

// WithError sets the underlying error.
func WithError(err error) HTTPErrorOption {
	return internal.WithError(err)
}

// Convenience constructors for common HTTP errors.

// ErrBadRequest creates a 400 Bad Request error.
func ErrBadRequest(message string, opts ...HTTPErrorOption) *HTTPError {
	return internal.ErrBadRequest(message, opts...)
}

// ErrUnauthorized creates a 401 Unauthorized error.
func ErrUnauthorized(message string, opts ...HTTPErrorOption) *HTTPError {
	return internal.ErrUnauthorized(message, opts...)
}

// ErrForbidden creates a 403 Forbidden error.
func ErrForbidden(message string, opts ...HTTPErrorOption) *HTTPError {
	return internal.ErrForbidden(message, opts...)
}

// ErrNotFound creates a 404 Not Found error.
func ErrNotFound(message string, opts ...HTTPErrorOption) *HTTPError {
	return internal.ErrNotFound(message, opts...)
}

// ErrConflict creates a 409 Conflict error.
func ErrConflict(message string, opts ...HTTPErrorOption) *HTTPError {
	return internal.ErrConflict(message, opts...)
}

// ErrUnprocessable creates a 422 Unprocessable Entity error.
func ErrUnprocessable(message string, opts ...HTTPErrorOption) *HTTPError {
	return internal.ErrUnprocessable(message, opts...)
}

// ErrInternal creates a 500 Internal Server Error.
func ErrInternal(message string, opts ...HTTPErrorOption) *HTTPError {
	return internal.ErrInternal(message, opts...)
}

// ErrServiceUnavailable creates a 503 Service Unavailable error.
func ErrServiceUnavailable(message string, opts ...HTTPErrorOption) *HTTPError {
	return internal.ErrServiceUnavailable(message, opts...)
}

// Helper functions for error inspection.

// IsHTTPError returns true if the error is an HTTPError.
func IsHTTPError(err error) bool {
	return internal.IsHTTPError(err)
}

// AsHTTPError extracts the HTTPError from an error if present.
// Returns nil if the error is not an HTTPError.
func AsHTTPError(err error) *HTTPError {
	return internal.AsHTTPError(err)
}
