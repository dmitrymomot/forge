package forge

import (
	"context"
	"io/fs"
	"log/slog"
	"time"

	"github.com/dmitrymomot/forge/internal"
	"github.com/dmitrymomot/forge/middlewares"
	"github.com/dmitrymomot/forge/pkg/cookie"
	"github.com/dmitrymomot/forge/pkg/i18n"
	"github.com/dmitrymomot/forge/pkg/job"
	"github.com/dmitrymomot/forge/pkg/logger"
	"github.com/dmitrymomot/forge/pkg/ratelimit"
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

	// Session represents a user session with metadata and data.
	Session = internal.Session

	// SessionOption configures the session manager.
	SessionOption = internal.SessionOption

	// HealthCheckOption adds a readiness check to the health configuration.
	HealthCheckOption = internal.HealthCheckOption

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

	// SessionStore defines the interface for session persistence.
	SessionStore = internal.Store

	// ResponseWriter wraps http.ResponseWriter with hooks and HTMX support.
	ResponseWriter = internal.ResponseWriter

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

	// SSEEvent represents a Server-Sent Event to be streamed to the client.
	SSEEvent = internal.SSEEvent
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
func WithLogger(component string, extractors ...logger.ContextExtractor) Option {
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
func WithCookieConfig(cfg cookie.Config) Option {
	return internal.WithCookieConfig(cfg)
}

// WithSession enables server-side session management.
// A SessionStore implementation must be provided (e.g., PostgresStore).
// Sessions are loaded lazily and saved automatically before the response is written.
//
// Example:
//
//	app := forge.New(
//	    forge.WithSession(postgresStore,
//	        forge.WithSessionTTL(7 * 24 * time.Hour),        // 7 days
//	        forge.WithMaxSessionsPerUser(3),                  // Max 3 devices
//	        forge.WithSessionFingerprint(
//	            forge.FingerprintCookie,                     // Mode
//	            forge.FingerprintWarn,                       // Strictness
//	        ),
//	    ),
//	)
//
// Sessions auto-create on first access. No manual c.InitSession() required.
func WithSession(store SessionStore, opts ...SessionOption) Option {
	return internal.WithSession(store, opts...)
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
	ErrSessionNotConfigured       = internal.ErrSessionNotConfigured
	ErrSessionNotFound            = internal.ErrSessionNotFound
	ErrSessionExpired             = internal.ErrSessionExpired
	ErrSessionInvalidToken        = internal.ErrSessionInvalidToken
	ErrSessionFingerprintMismatch = internal.ErrSessionFingerprintMismatch
)

// SSE event constructors
var (
	// SSEString creates a string SSE event.
	SSEString = internal.SSEString
	// SSEJSON creates a JSON SSE event.
	SSEJSON = internal.SSEJSON
	// SSETempl creates an HTML SSE event from a templ Component.
	SSETempl = internal.SSETempl
	// SSEComment creates an SSE comment (keepalive, etc.).
	SSEComment = internal.SSEComment
	// SSERetry creates an SSE retry directive.
	SSERetry = internal.SSERetry
)

// Session option re-exports
var (
	WithSessionTTL            = internal.WithSessionTTL
	WithMaxSessionsPerUser    = internal.WithMaxSessionsPerUser
	WithSessionTouchThreshold = internal.WithSessionTouchThreshold
	WithSessionCookieName     = internal.WithSessionCookieName
	WithSessionFingerprint    = internal.WithSessionFingerprint
	WithSessionLogger         = internal.WithSessionLogger
)

// SessionGet retrieves a typed value from the session.
// Returns (value, true) if found and type matches, (zero, false) otherwise.
//
// Example:
//
//	func Handler(c forge.Context) error {
//	    userID, ok := forge.SessionGet[string](c, "user_id")
//	    if !ok {
//	        return c.Redirect(http.StatusSeeOther, "/login")
//	    }
//	    // Use userID...
//	}
//
// This automatically creates the session if it doesn't exist.
func SessionGet[T any](c Context, key string) (T, bool) {
	var zero T
	val, err := c.SessionValue(key)
	if err != nil {
		return zero, false
	}
	typed, ok := val.(T)
	if !ok {
		return zero, false
	}
	return typed, true
}

// SessionSet stores a typed value in the session.
//
// Example:
//
//	func LoginHandler(c forge.Context) error {
//	    // After validating credentials...
//	    if err := forge.SessionSet(c, "user_id", user.ID); err != nil {
//	        return err
//	    }
//	    if err := forge.SessionSet(c, "login_time", time.Now()); err != nil {
//	        return err
//	    }
//	    return c.Redirect(http.StatusSeeOther, "/dashboard")
//	}
//
// This automatically creates the session if it doesn't exist and marks it dirty for saving.
func SessionSet[T any](c Context, key string, value T) error {
	return c.SetSessionValue(key, value)
}

// Job options

// WithJobs enables both job enqueueing and worker processing.
// A job.Driver is required for the job queue backend. Workers are started
// automatically when the app runs and stopped gracefully during shutdown.
func WithJobs(driver job.Driver, cfg job.Config, opts ...job.Option) Option {
	return internal.WithJobs(driver, cfg, opts...)
}

// WithJobEnqueuer enables job enqueueing without worker processing.
// Use this for web servers that dispatch work to separate worker processes.
// Workers must be running elsewhere to process the enqueued jobs.
func WithJobEnqueuer(driver job.Driver, opts ...job.EnqueuerOption) Option {
	return internal.WithJobEnqueuer(driver, opts...)
}

// WithJobWorker enables job processing without enqueueing capability.
// Use this for dedicated background worker processes that don't need
// to dispatch additional jobs. Workers are started automatically when
// the app runs and stopped gracefully during shutdown.
func WithJobWorker(driver job.Driver, cfg job.Config, opts ...job.Option) Option {
	return internal.WithJobWorker(driver, cfg, opts...)
}

// Job errors for checking return values.
var (
	ErrJobNotConfigured     = job.ErrNotConfigured
	ErrJobUnknownTask       = job.ErrUnknownTask
	ErrJobInvalidPayload    = job.ErrInvalidPayload
	ErrJobHealthcheckFailed = job.ErrHealthcheckFailed
	ErrJobDriverRequired    = job.ErrDriverRequired
	ErrJobInvalidTx         = job.ErrInvalidTx

	// ErrJobPoolRequired is kept for backward compatibility.
	// Deprecated: Use ErrJobDriverRequired instead.
	ErrJobPoolRequired = job.ErrPoolRequired
)

// WithSSEKeepAlive sets the interval for SSE keepalive comments.
// Defaults to 30 seconds if not set or if d <= 0.
func WithSSEKeepAlive(d time.Duration) Option {
	return internal.WithSSEKeepAlive(d)
}

// WithStorage configures file storage for the application.
// A storage.Storage implementation must be provided (e.g., S3Client).
// Enables c.Upload(), c.UploadFromURL(), c.Download(), c.DeleteFile(), and c.FileURL().
func WithStorage(s storage.Storage) Option {
	return internal.WithStorage(s)
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

// Middleware type aliases - re-exported from middlewares
type (
	// PanicError represents a recovered panic.
	PanicError = middlewares.PanicError

	// I18nOption configures the I18n middleware.
	I18nOption = middlewares.I18nOption

	// JWTOption configures the JWT middleware.
	JWTOption = middlewares.JWTOption

	// CSRFConfig configures the CSRF middleware.
	CSRFConfig = middlewares.CSRFConfig

	// CSRFOption configures runtime dependencies for the CSRF middleware.
	CSRFOption = middlewares.CSRFOption

	// RateLimitOption configures the RateLimit middleware.
	RateLimitOption = middlewares.RateLimitOption

	// AuditEntry represents a single audit log record.
	AuditEntry = middlewares.Entry

	// AuditStore defines the interface for persisting audit log entries.
	AuditStore = middlewares.Store

	// AuditOption configures the AuditLog middleware.
	AuditOption = middlewares.AuditOption
)

// Middleware helpers - re-exported from middlewares

// GetRequestID extracts the request ID from the context.
// Returns an empty string if no request ID is set.
func GetRequestID(c Context) string {
	return middlewares.GetRequestID(c)
}

// RequestIDExtractor returns a ContextExtractor for use with WithLogger.
// Automatically adds "request_id" to all log entries.
func RequestIDExtractor() logger.ContextExtractor {
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
func GetTranslator(c Context) *i18n.Translator {
	return middlewares.GetTranslator(c)
}

// GetLanguage extracts the resolved language from the context.
// Returns an empty string if the I18n middleware is not used.
func GetLanguage(c Context) string {
	return middlewares.GetLanguage(c)
}

// T translates a key using the Translator stored in context by the I18n middleware.
// Returns the key itself if no translator is in context.
func T(c Context, key string, placeholders ...i18n.M) string {
	tr := middlewares.GetTranslator(c)
	if tr == nil {
		return key
	}
	return tr.T(key, placeholders...)
}

// Tn translates a key with pluralization using the Translator stored in context.
// Returns the key itself if no translator is in context.
func Tn(c Context, key string, n int, placeholders ...i18n.M) string {
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

// CSRF middleware helpers

// GetCSRFToken extracts the CSRF token from the context.
// Returns an empty string if no token is set.
func GetCSRFToken(c Context) string {
	return middlewares.GetCSRFToken(c)
}

// CSRF option constructor re-exports
var (
	WithCSRFTokenGenerator = middlewares.WithCSRFTokenGenerator
	WithCSRFErrorHandler   = middlewares.WithCSRFErrorHandler
	WithCSRFSkipFunc       = middlewares.WithCSRFSkipFunc
)

// Rate limit middleware helpers

// GetRateLimitInfo returns the rate limit info stored in the context by the RateLimit middleware.
// Returns nil if the middleware was not applied or the request was skipped.
func GetRateLimitInfo(c Context) *ratelimit.Info {
	return middlewares.GetRateLimitInfo(c)
}

// Rate limit option constructor re-exports
var (
	WithRateLimitKeyFunc      = middlewares.WithRateLimitKeyFunc
	WithRateLimitErrorHandler = middlewares.WithRateLimitErrorHandler
	WithRateLimitSkipFunc     = middlewares.WithRateLimitSkipFunc
)

// Audit middleware helpers

// GetAuditEntry extracts the audit entry from the context.
// Returns nil if the AuditLog middleware is not applied.
func GetAuditEntry(c Context) *AuditEntry {
	return middlewares.GetAuditEntry(c)
}

// SetAuditMetadata adds a key-value pair to the audit entry metadata.
// No-op if the AuditLog middleware is not applied.
func SetAuditMetadata(c Context, key, value string) {
	middlewares.SetAuditMetadata(c, key, value)
}

// Audit option constructor re-exports
var (
	WithAuditLogger       = middlewares.WithAuditLogger
	WithAuditSkipFunc     = middlewares.WithAuditSkipFunc
	WithAuditActionFunc   = middlewares.WithAuditActionFunc
	WithAuditResourceFunc = middlewares.WithAuditResourceFunc
	WithAuditMetadataFunc = middlewares.WithAuditMetadataFunc
	WithAuditTimeout      = middlewares.WithAuditTimeout
)

// I18n middleware option constructors

// WithI18nExtractor sets a custom language extractor chain.
func WithI18nExtractor(ext Extractor) I18nOption {
	return middlewares.WithI18nExtractor(ext)
}

// WithI18nFormatMap sets the language-to-format mapping.
func WithI18nFormatMap(m map[string]*i18n.LocaleFormat) I18nOption {
	return middlewares.WithI18nFormatMap(m)
}

// WithI18nDefaultFormat sets the fallback locale format.
func WithI18nDefaultFormat(f *i18n.LocaleFormat) I18nOption {
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

// ErrTooManyRequests creates a 429 Too Many Requests error.
func ErrTooManyRequests(message string, opts ...HTTPErrorOption) *HTTPError {
	return internal.ErrTooManyRequests(message, opts...)
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
