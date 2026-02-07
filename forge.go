package forge

import (
	"context"
	"io/fs"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/internal"
	"github.com/dmitrymomot/forge/middlewares"
	"github.com/dmitrymomot/forge/pkg/cookie"
	db "github.com/dmitrymomot/forge/pkg/db"
	"github.com/dmitrymomot/forge/pkg/i18n"
	"github.com/dmitrymomot/forge/pkg/job"
	"github.com/dmitrymomot/forge/pkg/logger"
	redis "github.com/dmitrymomot/forge/pkg/redis"
	"github.com/dmitrymomot/forge/pkg/session"
	"github.com/dmitrymomot/forge/pkg/storage"
)

// Type aliases - public API
type (
	// App orchestrates the application lifecycle.
	// It manages HTTP routing, middleware, and graceful shutdown.
	App = internal.App

	// AppConfig holds externally configurable application settings.
	AppConfig = internal.AppConfig

	// RunConfig holds externally configurable runtime settings.
	RunConfig = internal.RunConfig

	// SessionConfig holds session manager configuration.
	SessionConfig = internal.SessionConfig

	// HealthCheckOption adds a readiness check to the health configuration.
	HealthCheckOption = internal.HealthCheckOption

	// CookieConfig holds cookie manager configuration.
	CookieConfig = cookie.Config

	// DBConfig holds database connection configuration.
	DBConfig = db.Config

	// RedisConfig holds Redis connection configuration.
	RedisConfig = redis.Config

	// JobConfig holds job manager configuration.
	JobConfig = job.Config

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

	// CheckFunc is the standard health check function signature.
	CheckFunc = internal.CheckFunc

	// ContextExtractor extracts a slog attribute from context.
	// Used with WithLogger to add request-scoped values to logs.
	ContextExtractor = logger.ContextExtractor

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

// New creates a new application with the given config and options.
// The App is immutable after creation.
func New(cfg AppConfig, opts ...Option) *App {
	return internal.New(cfg, opts...)
}

// Run starts a multi-domain HTTP server and blocks until shutdown.
// Use this for composing multiple Apps under different domain patterns.
func Run(cfg RunConfig, opts ...RunOption) error {
	return internal.Run(cfg, opts...)
}

// LoadConfig parses environment variables into dst using struct tags.
// It loads .env from the working directory automatically.
// Struct fields use `env:"KEY"`, `envDefault:"value"`, and `envSeparator:","`
// tags to declare their bindings.
func LoadConfig(dst any) error {
	return internal.LoadConfig(dst)
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

// WithHealthChecks enables health check endpoints.
// Liveness: /_live — always returns OK if process is running.
// Readiness: /_ready — runs all configured checks.
func WithHealthChecks(checks ...HealthCheckOption) Option {
	return internal.WithHealthChecks(checks...)
}

// HealthCheck creates a named readiness check for use with WithHealthChecks.
func HealthCheck(name string, fn CheckFunc) HealthCheckOption {
	return internal.HealthCheck(name, fn)
}

// WithLogger creates a logger with a component name and optional extractors.
// The component name is added to every log entry for easy filtering.
// Extractors pull values from context (e.g., request_id, user_id).
func WithLogger(component string, extractors ...ContextExtractor) Option {
	return internal.WithLogger(component, extractors...)
}

// WithCustomLogger sets a fully custom logger.
// Use this when you need complete control over logging configuration.
func WithCustomLogger(l *slog.Logger) Option {
	return internal.WithCustomLogger(l)
}

// WithRoles configures role-based access control for the application.
// The permissions map defines which permissions each role grants.
// The extractor function determines the current user's role from the request context.
// Roles are extracted lazily (once per request) and cached.
func WithRoles(permissions RolePermissions, extractor RoleExtractorFunc) Option {
	return internal.WithRoles(permissions, extractor)
}

// WithCookieConfig configures the cookie manager.
func WithCookieConfig(cfg CookieConfig) Option {
	return internal.WithCookieConfig(cfg)
}

// WithSession enables server-side session management.
// A SessionStore implementation must be provided (e.g., PostgresStore).
// Sessions are loaded lazily and saved automatically before the response is written.
func WithSession(store SessionStore, cfg SessionConfig) Option {
	return internal.WithSession(store, cfg)
}

// Run options

// WithRunLogger sets the application logger for the runtime.
// If nil, logging is disabled.
func WithRunLogger(l *slog.Logger) RunOption {
	return internal.WithRunLogger(l)
}

// WithStartupHook registers a function to run during server startup.
// Hooks are called in the order they were registered, after the port is bound
// but before serving requests. If any hook fails, the server stops and
// returns the error.
func WithStartupHook(fn func(context.Context) error) RunOption {
	return internal.WithStartupHook(fn)
}

// WithShutdownHook registers a cleanup function to run during shutdown.
// Hooks are called in the order they were registered.
// Each hook receives a context with the shutdown timeout.
func WithShutdownHook(fn func(context.Context) error) RunOption {
	return internal.WithShutdownHook(fn)
}

// WithDomain maps a host pattern to an App.
// Patterns: "api.example.com" (exact) or "*.example.com" (wildcard)
func WithDomain(pattern string, app *App) RunOption {
	return internal.WithDomain(pattern, app)
}

// WithFallback sets the default App for requests that don't match any domain.
// If no domains are configured, the fallback becomes the main handler.
func WithFallback(app *App) RunOption {
	return internal.WithFallback(app)
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
func ContextValue[T any](c Context, key any) T {
	return internal.ContextValue[T](c, key)
}

// Param retrieves a typed URL parameter from the request.
// Uses strconv for type conversion. Returns the zero value of T on parse error.
func Param[T ~string | ~int | ~int64 | ~float64 | ~bool](c Context, name string) T {
	return internal.Param[T](c, name)
}

// Query retrieves a typed query parameter from the request.
// Uses strconv for type conversion. Returns the zero value of T on parse error.
func Query[T ~string | ~int | ~int64 | ~float64 | ~bool](c Context, name string) T {
	return internal.Query[T](c, name)
}

// QueryDefault retrieves a typed query parameter with a default value.
// Returns defaultValue if the parameter is empty or cannot be parsed.
func QueryDefault[T ~string | ~int | ~int64 | ~float64 | ~bool](c Context, name string, defaultValue T) T {
	return internal.QueryDefault[T](c, name, defaultValue)
}

// Extractor helpers

// NewExtractor creates an Extractor that tries the given sources in order.
// Returns the first non-empty value found.
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

// Cookie errors for checking return values.
var (
	ErrCookieNotFound  = cookie.ErrNotFound
	ErrCookieNoSecret  = cookie.ErrNoSecret
	ErrCookieBadSecret = cookie.ErrBadSecret
	ErrCookieBadSig    = cookie.ErrBadSig
	ErrCookieDecrypt   = cookie.ErrDecrypt
)

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
func WithJobs(pool *pgxpool.Pool, cfg JobConfig, opts ...JobOption) Option {
	return internal.WithJobs(pool, cfg, opts...)
}

// WithJobEnqueuer enables job enqueueing without worker processing.
// Use this for web servers that dispatch work to separate worker processes.
// Workers must be running elsewhere to process the enqueued jobs.
func WithJobEnqueuer(pool *pgxpool.Pool, opts ...EnqueuerOption) Option {
	return internal.WithJobEnqueuer(pool, opts...)
}

// WithJobWorker enables job processing without enqueueing capability.
// Use this for dedicated background worker processes that don't need
// to dispatch additional jobs. Workers are started automatically when
// the app runs and stopped gracefully during shutdown.
func WithJobWorker(pool *pgxpool.Pool, cfg JobConfig, opts ...JobOption) Option {
	return internal.WithJobWorker(pool, cfg, opts...)
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

// WithJobQueueWorkers configures a named queue with the specified number of workers.
func WithJobQueueWorkers(name string, workers int) JobOption {
	return job.WithQueueWorkers(name, workers)
}

// WithJobLogger sets the logger for job processing.
func WithJobLogger(l *slog.Logger) JobOption {
	return job.WithLogger(l)
}

// Enqueue options - re-exported from pkg/job

// WithQueue specifies which queue to use for the job.
func WithQueue(name string) EnqueueOption {
	return job.WithQueue(name)
}

// WithScheduledAt schedules the job to run at a specific time.
func WithScheduledAt(t time.Time) EnqueueOption {
	return job.WithScheduledAt(t)
}

// WithScheduledIn schedules the job to run after a duration.
func WithScheduledIn(d time.Duration) EnqueueOption {
	return job.WithScheduledIn(d)
}

// WithMaxAttempts sets the maximum number of retry attempts for the job.
func WithMaxAttempts(n int) EnqueueOption {
	return job.WithMaxAttempts(n)
}

// WithUniqueFor ensures only one job with this key exists for the specified duration.
func WithUniqueFor(d time.Duration) EnqueueOption {
	return job.WithUniqueFor(d)
}

// WithUniqueKey sets a custom unique key for deduplication.
func WithUniqueKey(key string) EnqueueOption {
	return job.WithUniqueKey(key)
}

// WithJobPriority sets the job priority (lower numbers = higher priority).
func WithJobPriority(p int) EnqueueOption {
	return job.WithPriority(p)
}

// WithJobTags adds metadata tags to the job.
func WithJobTags(tags ...string) EnqueueOption {
	return job.WithTags(tags...)
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
func SessionValue[T any](sess *Session, key string) (T, error) {
	return session.Value[T](sess, key)
}

// SessionValueOr is a typed helper that returns a default value if the key
// doesn't exist or type assertion fails.
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

// AsPanicError extracts the PanicError from an error if present.
func AsPanicError(err error) (*PanicError, bool) {
	return middlewares.AsPanicError(err)
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
