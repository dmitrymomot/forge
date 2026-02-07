package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/dmitrymomot/forge/pkg/binder"
	"github.com/dmitrymomot/forge/pkg/cookie"
	"github.com/dmitrymomot/forge/pkg/hostrouter"
	"github.com/dmitrymomot/forge/pkg/htmx"
	"github.com/dmitrymomot/forge/pkg/i18n"
	"github.com/dmitrymomot/forge/pkg/job"
	"github.com/dmitrymomot/forge/pkg/sanitizer"
	"github.com/dmitrymomot/forge/pkg/session"
	"github.com/dmitrymomot/forge/pkg/storage"
	"github.com/dmitrymomot/forge/pkg/validator"
)

// ValidationErrors is a collection of validation errors.
type ValidationErrors = validator.ValidationErrors

// Permission represents a named permission string.
type Permission string

// RolePermissions maps role names to their granted permissions.
type RolePermissions = map[string][]Permission

// RoleExtractorFunc extracts the current user's role from the request context.
type RoleExtractorFunc = func(Context) string

// TranslatorKey is the context key used to store the i18n Translator.
type TranslatorKey struct{}

// LanguageKey is the context key used to store the resolved language string.
type LanguageKey struct{}

// JWTClaimsKey is the context key used to store parsed JWT claims.
type JWTClaimsKey struct{}

// Component is the interface for renderable templates.
// This is compatible with templ.Component.
type Component interface {
	Render(ctx context.Context, w io.Writer) error
}

// Context provides request/response access and helper methods.
// It also implements context.Context by delegating to the underlying request context.
type Context interface {
	context.Context

	// Request returns the underlying *http.Request.
	Request() *http.Request

	// Response returns the underlying http.ResponseWriter.
	Response() http.ResponseWriter

	// Context returns the request's context.Context.
	Context() context.Context

	// Param returns the URL parameter value by name.
	// Returns empty string if the parameter doesn't exist.
	Param(name string) string

	// Query returns the query parameter value by name.
	// Returns empty string if the parameter doesn't exist.
	Query(name string) string

	// QueryDefault returns the query parameter value or a default.
	QueryDefault(name, defaultValue string) string

	// Form returns the form value by name.
	// Calls ParseForm/ParseMultipartForm internally on first access.
	// Returns empty string if the field doesn't exist.
	Form(name string) string

	// FormFile returns the first file for the given form key.
	// Returns the file, its header, and any error.
	FormFile(name string) (multipart.File, *multipart.FileHeader, error)

	// UserID returns the authenticated user's ID from the session.
	// Loads the session lazily on first call.
	// Returns empty string if no session, no session manager, or no user.
	UserID() string

	// IsAuthenticated returns true if a user is associated with the session.
	IsAuthenticated() bool

	// IsCurrentUser returns true if the authenticated user's ID matches the given id.
	IsCurrentUser(id string) bool

	// Can returns true if the current user's role grants the given permission.
	// Returns false if RBAC is not configured or the user has no matching permission.
	// The role is extracted lazily and cached for the lifetime of the request.
	Can(permission Permission) bool

	// Domain returns the normalized domain from the request Host header.
	// Strips port, handles IPv6, and converts to lowercase.
	Domain() string

	// Subdomain extracts the subdomain from the request.
	// Uses the base domain configured via WithBaseDomain.
	// Returns empty string if no base domain configured or host doesn't match.
	Subdomain() string

	// Header returns the request header value by name.
	Header(name string) string

	// SetHeader sets a response header.
	SetHeader(name, value string)

	// JSON writes a JSON response with the given status code.
	JSON(code int, v any) error

	// String writes a plain text response with the given status code.
	String(code int, s string) error

	// NoContent writes a response with no body.
	NoContent(code int) error

	// Redirect redirects to the given URL with the given status code.
	// Handles both regular HTTP redirects and HTMX requests.
	Redirect(code int, url string) error

	// Error creates and returns an HTTPError without writing a response.
	// The error should be returned from the handler to trigger the error handler.
	Error(code int, message string, opts ...HTTPErrorOption) *HTTPError

	// IsHTMX returns true if the request originated from HTMX.
	IsHTMX() bool

	// Render renders a component with the given status code.
	// For HTMX requests: always uses HTTP 200 (HTMX requires 2xx for swapping).
	// For regular requests: uses the provided status code.
	// Compatible with templ.Component.
	// Optional render options configure HTMX response headers.
	Render(code int, component Component, opts ...htmx.RenderOption) error

	// RenderPartial renders different components based on request type.
	// For HTMX requests: renders partial with HTTP 200.
	// For regular requests: renders fullPage with the provided status code.
	// Optional render options configure HTMX response headers (only applied for HTMX requests).
	RenderPartial(code int, fullPage, partial Component, opts ...htmx.RenderOption) error

	// Bind binds form data, sanitizes, and validates into a struct.
	// Returns validation errors separately from system errors.
	Bind(v any) (ValidationErrors, error)

	// BindQuery binds query parameters, sanitizes, and validates into a struct.
	// Returns validation errors separately from system errors.
	BindQuery(v any) (ValidationErrors, error)

	// BindJSON binds JSON body, sanitizes, and validates into a struct.
	// Returns validation errors separately from system errors.
	BindJSON(v any) (ValidationErrors, error)

	// Written returns true if a response has already been written.
	Written() bool

	// Logger returns the logger for advanced usage.
	Logger() *slog.Logger

	// LogDebug logs a debug message with optional attributes.
	LogDebug(msg string, attrs ...any)

	// LogInfo logs an info message with optional attributes.
	LogInfo(msg string, attrs ...any)

	// LogWarn logs a warning message with optional attributes.
	LogWarn(msg string, attrs ...any)

	// LogError logs an error message with optional attributes.
	LogError(msg string, attrs ...any)

	// Set stores a value in the request context.
	// The value can be retrieved using Get or from c.Context().Value(key).
	Set(key any, value any)

	// Get retrieves a value from the request context.
	// Returns nil if the key is not found.
	Get(key any) any

	// Cookie returns a plain cookie value.
	Cookie(name string) (string, error)

	// SetCookie sets a plain cookie.
	SetCookie(name, value string, maxAge int)

	// DeleteCookie removes a cookie.
	DeleteCookie(name string)

	// CookieSigned returns a signed cookie value.
	// Returns cookie.ErrNoSecret if no secret is configured.
	CookieSigned(name string) (string, error)

	// SetCookieSigned sets a signed cookie.
	// Returns cookie.ErrNoSecret if no secret is configured.
	SetCookieSigned(name, value string, maxAge int) error

	// CookieEncrypted returns an encrypted cookie value.
	// Returns cookie.ErrNoSecret if no secret is configured.
	CookieEncrypted(name string) (string, error)

	// SetCookieEncrypted sets an encrypted cookie.
	// Returns cookie.ErrNoSecret if no secret is configured.
	SetCookieEncrypted(name, value string, maxAge int) error

	// Flash reads and deletes a flash message.
	// Returns cookie.ErrNoSecret if no secret is configured.
	Flash(key string, dest any) error

	// SetFlash sets a flash message.
	// Returns cookie.ErrNoSecret if no secret is configured.
	SetFlash(key string, value any) error

	// Session returns the current session, loading or creating it as needed.
	// Returns session.ErrNotConfigured if WithSession was not called.
	// Returns nil, nil if no session exists and lazy loading is disabled.
	Session() (*session.Session, error)

	// InitSession creates a new session for this request.
	// Returns session.ErrNotConfigured if WithSession was not called.
	InitSession() error

	// AuthenticateSession associates a user with the session and rotates the token.
	// Creates a new session if one doesn't exist.
	// Returns session.ErrNotConfigured if WithSession was not called.
	AuthenticateSession(userID string) error

	// SessionValue retrieves a typed value from the session.
	// Returns session.ErrNotConfigured if WithSession was not called.
	// Returns session.ErrNotFound if no session exists.
	SessionValue(key string) (any, error)

	// SetSessionValue stores a value in the session.
	// Returns session.ErrNotConfigured if WithSession was not called.
	// Returns session.ErrNotFound if no session exists.
	SetSessionValue(key string, val any) error

	// DeleteSessionValue removes a value from the session.
	// Returns session.ErrNotConfigured if WithSession was not called.
	// Returns session.ErrNotFound if no session exists.
	DeleteSessionValue(key string) error

	// DestroySession removes the session and clears the cookie.
	// Returns session.ErrNotConfigured if WithSession was not called.
	DestroySession() error

	// ResponseWriter returns the underlying ResponseWriter for advanced usage.
	// Returns nil if not using the wrapped response writer.
	ResponseWriter() *ResponseWriter

	// Enqueue adds a job to the queue for background processing.
	// Returns job.ErrNotConfigured if WithJobs was not called.
	// Returns job.ErrUnknownTask if the task name is not registered.
	Enqueue(name string, payload any, opts ...job.EnqueueOption) error

	// EnqueueTx adds a job to the queue within a transaction.
	// The job is only visible after the transaction commits.
	// Returns job.ErrNotConfigured if WithJobs was not called.
	// Returns job.ErrUnknownTask if the task name is not registered.
	EnqueueTx(tx pgx.Tx, name string, payload any, opts ...job.EnqueueOption) error

	// Storage returns the configured storage client.
	// Returns storage.ErrNotConfigured if WithStorage was not called.
	Storage() (storage.Storage, error)

	// Upload stores data and returns file info.
	// Returns storage.ErrNotConfigured if WithStorage was not called.
	Upload(r io.Reader, size int64, opts ...storage.Option) (*storage.FileInfo, error)

	// Download retrieves a file from storage.
	// Returns storage.ErrNotConfigured if WithStorage was not called.
	Download(key string) (io.ReadCloser, error)

	// DeleteFile removes a file from storage.
	// Returns storage.ErrNotConfigured if WithStorage was not called.
	DeleteFile(key string) error

	// FileURL generates a URL for accessing the file.
	// Returns storage.ErrNotConfigured if WithStorage was not called.
	FileURL(key string, opts ...storage.URLOption) (string, error)

	// T translates a key using the Translator stored in context by the I18n middleware.
	// Returns the key itself if no translator is in context.
	T(key string, placeholders ...i18n.M) string

	// Tn translates a key with pluralization using the Translator stored in context.
	// Returns the key itself if no translator is in context.
	Tn(key string, n int, placeholders ...i18n.M) string

	// Language returns the resolved language from the I18n middleware.
	// Returns an empty string if no translator is in context.
	Language() string

	// FormatNumber formats a number using locale-specific separators.
	// Falls back to fmt.Sprintf if no translator is in context.
	FormatNumber(n float64) string

	// FormatCurrency formats a currency amount using locale-specific formatting.
	// Falls back to fmt.Sprintf if no translator is in context.
	FormatCurrency(amount float64) string

	// FormatPercent formats a percentage using locale-specific formatting.
	// Falls back to fmt.Sprintf if no translator is in context.
	FormatPercent(n float64) string

	// FormatDate formats a date using locale-specific formatting.
	// Falls back to time.Format if no translator is in context.
	FormatDate(date time.Time) string

	// FormatTime formats a time value using locale-specific formatting.
	// Falls back to time.Format if no translator is in context.
	FormatTime(t time.Time) string

	// FormatDateTime formats a datetime using locale-specific formatting.
	// Falls back to time.Format if no translator is in context.
	FormatDateTime(datetime time.Time) string
}

// requestContext implements the Context interface.
type requestContext struct {
	response       http.ResponseWriter
	request        *http.Request
	responseWriter *ResponseWriter
	logger         *slog.Logger
	cookieManager  *cookie.Manager

	// Session management
	sessionManager *SessionManager
	session        *session.Session

	// Job management
	jobEnqueuer *JobEnqueuer

	// Storage
	storage storage.Storage

	// RBAC
	rolePermissions RolePermissions
	roleExtractor   RoleExtractorFunc
	cachedRole      *string

	baseDomain string

	sessionLoaded         bool
	sessionHookRegistered bool
}

// newContext creates a new context with the response wrapper.
func newContext(w http.ResponseWriter, r *http.Request, app *App) *requestContext {
	// Create response wrapper
	rw := NewResponseWriter(w, htmx.IsHTMX(r))

	return &requestContext{
		request:         r,
		response:        rw,
		responseWriter:  rw,
		logger:          app.logger,
		cookieManager:   app.cookieManager,
		sessionManager:  app.sessionManager,
		jobEnqueuer:     app.jobEnqueuer,
		storage:         app.storage,
		baseDomain:      app.baseDomain,
		rolePermissions: app.rolePermissions,
		roleExtractor:   app.roleExtractor,
	}
}

func (c *requestContext) Request() *http.Request {
	return c.request
}

func (c *requestContext) Response() http.ResponseWriter {
	return c.response
}

func (c *requestContext) Context() context.Context {
	return c.request.Context()
}

func (c *requestContext) Param(name string) string {
	return chi.URLParam(c.request, name)
}

func (c *requestContext) Query(name string) string {
	return c.request.URL.Query().Get(name)
}

func (c *requestContext) QueryDefault(name, defaultValue string) string {
	v := c.request.URL.Query().Get(name)
	if v == "" {
		return defaultValue
	}
	return v
}

func (c *requestContext) Form(name string) string {
	return c.request.FormValue(name)
}

func (c *requestContext) FormFile(name string) (multipart.File, *multipart.FileHeader, error) {
	return c.request.FormFile(name)
}

func (c *requestContext) Deadline() (time.Time, bool) {
	return c.request.Context().Deadline()
}

func (c *requestContext) Done() <-chan struct{} {
	return c.request.Context().Done()
}

func (c *requestContext) Err() error {
	return c.request.Context().Err()
}

func (c *requestContext) Value(key any) any {
	return c.request.Context().Value(key)
}

func (c *requestContext) UserID() string {
	// Use cached session if already loaded, otherwise try loading via Session()
	sess := c.session
	if !c.sessionLoaded {
		var err error
		sess, err = c.Session()
		if err != nil {
			return ""
		}
	}
	if sess == nil || sess.UserID == nil {
		return ""
	}
	return *sess.UserID
}

func (c *requestContext) IsAuthenticated() bool {
	return c.UserID() != ""
}

func (c *requestContext) IsCurrentUser(id string) bool {
	uid := c.UserID()
	return uid != "" && uid == id
}

func (c *requestContext) Can(permission Permission) bool {
	if c.rolePermissions == nil || c.roleExtractor == nil {
		return false
	}

	// Lazy role extraction, cached per request.
	// Set sentinel before calling extractor to prevent infinite recursion
	// if the extractor itself calls Can().
	if c.cachedRole == nil {
		empty := ""
		c.cachedRole = &empty
		role := c.roleExtractor(c)
		c.cachedRole = &role
	}

	perms, ok := c.rolePermissions[*c.cachedRole]
	if !ok {
		return false
	}

	return slices.Contains(perms, permission)
}

func (c *requestContext) Domain() string {
	return hostrouter.GetDomain(c.request)
}

func (c *requestContext) Subdomain() string {
	if c.baseDomain == "" {
		return ""
	}
	return hostrouter.GetSubdomain(c.request, c.baseDomain)
}

func (c *requestContext) Header(name string) string {
	return c.request.Header.Get(name)
}

func (c *requestContext) SetHeader(name, value string) {
	c.response.Header().Set(name, value)
}

func (c *requestContext) JSON(code int, v any) error {
	c.response.Header().Set("Content-Type", "application/json; charset=utf-8")
	c.response.WriteHeader(code)
	return json.NewEncoder(c.response).Encode(v)
}

func (c *requestContext) String(code int, s string) error {
	c.response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.response.WriteHeader(code)
	_, err := c.response.Write([]byte(s))
	return err
}

func (c *requestContext) NoContent(code int) error {
	c.response.WriteHeader(code)
	return nil
}

func (c *requestContext) Redirect(code int, url string) error {
	htmx.RedirectWithStatus(c.response, c.request, url, code)
	return nil
}

func (c *requestContext) Error(code int, message string, opts ...HTTPErrorOption) *HTTPError {
	err := NewHTTPError(code, message)
	for _, opt := range opts {
		opt(err)
	}
	return err
}

func (c *requestContext) IsHTMX() bool {
	return htmx.IsHTMX(c.request)
}

// Render renders a component with the given status code.
// For HTMX requests: the ResponseWriter transforms non-200 to 200.
// For regular requests: uses the provided status code.
// Optional render options configure HTMX response headers.
func (c *requestContext) Render(code int, component Component, opts ...htmx.RenderOption) error {

	c.response.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Build config from options
	var cfg *htmx.Config
	if len(opts) > 0 {
		cfg = htmx.NewConfig(opts...)
	}

	// Apply HTMX headers only for HTMX requests
	if cfg != nil && htmx.IsHTMX(c.request) {
		cfg.ApplyHeaders(c.response)
	}

	c.response.WriteHeader(code)

	// Render main component
	if err := component.Render(c.request.Context(), c.response); err != nil {
		return err
	}

	// Render OOB components only for HTMX requests
	if cfg != nil && htmx.IsHTMX(c.request) {
		for _, oob := range cfg.OOBComponents {
			if err := oob.Render(c.request.Context(), c.response); err != nil {
				return err
			}
		}
	}

	return nil
}

// RenderPartial renders different components based on request type.
// For HTMX requests: renders partial with HTTP 200.
// For regular requests: renders fullPage with the provided status code.
// Optional render options are passed through (only applied for HTMX requests).
func (c *requestContext) RenderPartial(code int, fullPage, partial Component, opts ...htmx.RenderOption) error {
	if htmx.IsHTMX(c.request) {
		return c.Render(code, partial, opts...)
	}
	return c.Render(code, fullPage) // opts ignored for non-HTMX (graceful degradation)
}

func (c *requestContext) Bind(v any) (ValidationErrors, error) {
	return c.bindAndValidate(binder.Form(), v, "bind form")
}

func (c *requestContext) BindQuery(v any) (ValidationErrors, error) {
	return c.bindAndValidate(binder.Query(), v, "bind query")
}

func (c *requestContext) BindJSON(v any) (ValidationErrors, error) {
	return c.bindAndValidate(binder.JSON(), v, "bind json")
}

// bindAndValidate binds request data, sanitizes, and validates into a struct.
func (c *requestContext) bindAndValidate(bind func(*http.Request, any) error, v any, label string) (ValidationErrors, error) {
	if err := bind(c.request, v); err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	if err := sanitizer.SanitizeStruct(v); err != nil {
		return nil, fmt.Errorf("sanitize: %w", err)
	}
	if err := validator.ValidateStruct(v); err != nil {
		if validator.IsValidationError(err) {
			ve := validator.ExtractValidationErrors(err)
			if tr := c.translator(); tr != nil {
				ve.Translate(tr.TranslateMessage)
			}
			return ve, nil
		}
		return nil, fmt.Errorf("validate: %w", err)
	}
	return nil, nil
}

func (c *requestContext) Written() bool {
	return c.responseWriter.Written()
}

func (c *requestContext) Logger() *slog.Logger {
	return c.logger
}

func (c *requestContext) LogDebug(msg string, attrs ...any) {
	c.logger.DebugContext(c.request.Context(), msg, attrs...)
}

func (c *requestContext) LogInfo(msg string, attrs ...any) {
	c.logger.InfoContext(c.request.Context(), msg, attrs...)
}

func (c *requestContext) LogWarn(msg string, attrs ...any) {
	c.logger.WarnContext(c.request.Context(), msg, attrs...)
}

func (c *requestContext) LogError(msg string, attrs ...any) {
	c.logger.ErrorContext(c.request.Context(), msg, attrs...)
}

func (c *requestContext) Set(key, value any) {
	ctx := context.WithValue(c.request.Context(), key, value)
	c.request = c.request.WithContext(ctx)
}

func (c *requestContext) Get(key any) any {
	return c.request.Context().Value(key)
}

func (c *requestContext) Cookie(name string) (string, error) {
	return c.cookieManager.Get(c.request, name)
}

func (c *requestContext) SetCookie(name, value string, maxAge int) {
	c.cookieManager.Set(c.response, name, value, maxAge)
}

func (c *requestContext) DeleteCookie(name string) {
	c.cookieManager.Delete(c.response, name)
}

func (c *requestContext) CookieSigned(name string) (string, error) {
	return c.cookieManager.GetSigned(c.request, name)
}

func (c *requestContext) SetCookieSigned(name, value string, maxAge int) error {
	return c.cookieManager.SetSigned(c.response, name, value, maxAge)
}

func (c *requestContext) CookieEncrypted(name string) (string, error) {
	return c.cookieManager.GetEncrypted(c.request, name)
}

func (c *requestContext) SetCookieEncrypted(name, value string, maxAge int) error {
	return c.cookieManager.SetEncrypted(c.response, name, value, maxAge)
}

func (c *requestContext) Flash(key string, dest any) error {
	return c.cookieManager.Flash(c.response, c.request, key, dest)
}

func (c *requestContext) SetFlash(key string, value any) error {
	return c.cookieManager.SetFlash(c.response, key, value)
}

// registerSessionHook ensures the session flush hook is registered once.
// It runs before the response is written to persist any session changes.
func (c *requestContext) registerSessionHook() {
	if c.sessionHookRegistered || c.sessionManager == nil || c.responseWriter == nil {
		return
	}
	c.sessionHookRegistered = true
	c.responseWriter.OnBeforeWrite(func() {
		if c.session != nil && c.session.IsDirty() {
			// Best-effort save; errors are logged but not propagated
			// to avoid interrupting response rendering
			if err := c.sessionManager.Store().Update(c.Context(), c.session); err != nil {
				c.logger.ErrorContext(c.Context(), "failed to save session", "error", err)
				return
			}
			c.session.ClearDirty()
		}
	})
}

// Session returns the current session, loading it from the store if needed.
// Returns session.ErrNotConfigured if WithSession was not called.
func (c *requestContext) Session() (*session.Session, error) {
	if c.sessionManager == nil {
		return nil, session.ErrNotConfigured
	}

	// Register flush hook (lazy, only once)
	c.registerSessionHook()

	// Return cached session if already loaded
	if c.sessionLoaded {
		return c.session, nil
	}

	// Load from store
	sess, err := c.sessionManager.LoadSession(c.Context(), c.request)
	if err != nil {
		return nil, err
	}

	c.session = sess
	c.sessionLoaded = true
	return c.session, nil
}

// InitSession creates a new session for this request.
// Returns session.ErrNotConfigured if WithSession was not called.
func (c *requestContext) InitSession() error {
	if c.sessionManager == nil {
		return session.ErrNotConfigured
	}

	// Register flush hook (lazy, only once)
	c.registerSessionHook()

	sess, err := c.sessionManager.CreateSession(c.Context(), c.request)
	if err != nil {
		return err
	}

	c.session = sess
	c.sessionLoaded = true
	c.sessionManager.SaveSession(c.response, sess)
	return nil
}

// AuthenticateSession associates a user with the session and rotates the token.
// Creates a new session if one doesn't exist.
// Returns session.ErrNotConfigured if WithSession was not called.
func (c *requestContext) AuthenticateSession(userID string) error {
	if c.sessionManager == nil {
		return session.ErrNotConfigured
	}

	// Get or create session
	sess, err := c.Session()
	if err != nil {
		c.logger.WarnContext(c.Context(), "failed to load session", "error", err)
	}
	if sess == nil {
		if err := c.InitSession(); err != nil {
			return err
		}
		sess = c.session
	}

	// Set user ID
	sess.UserID = &userID
	sess.MarkDirty()

	// CRITICAL: Rotate token to prevent session fixation attacks
	if err := c.sessionManager.RotateToken(c.Context(), sess); err != nil {
		return err
	}

	// Update cookie with new token
	c.sessionManager.SaveSession(c.response, sess)
	return nil
}

func (c *requestContext) SessionValue(key string) (any, error) {
	sess, err := c.Session()
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, session.ErrNotFound
	}

	val, ok := sess.GetValue(key)
	if !ok {
		return nil, nil
	}
	return val, nil
}

func (c *requestContext) SetSessionValue(key string, val any) error {
	sess, err := c.Session()
	if err != nil {
		return err
	}
	if sess == nil {
		return session.ErrNotFound
	}

	sess.SetValue(key, val)
	return nil
}

func (c *requestContext) DeleteSessionValue(key string) error {
	sess, err := c.Session()
	if err != nil {
		return err
	}
	if sess == nil {
		return session.ErrNotFound
	}

	sess.DeleteValue(key)
	return nil
}

func (c *requestContext) DestroySession() error {
	if c.sessionManager == nil {
		return session.ErrNotConfigured
	}

	// Delete from store if we have a session
	if c.session != nil {
		if err := c.sessionManager.Store().Delete(c.Context(), c.session.ID); err != nil {
			return err
		}
	}

	// Clear cookie
	c.sessionManager.DeleteSession(c.response)

	// Clear cached session
	c.session = nil
	c.sessionLoaded = true // Mark as loaded (with nil) to prevent reload

	return nil
}

func (c *requestContext) ResponseWriter() *ResponseWriter {
	return c.responseWriter
}

func (c *requestContext) Enqueue(name string, payload any, opts ...job.EnqueueOption) error {
	if c.jobEnqueuer == nil {
		return job.ErrNotConfigured
	}
	return c.jobEnqueuer.Enqueue(c.Context(), name, payload, opts...)
}

// EnqueueTx adds a job to the queue within a transaction.
// The job is only visible after the transaction commits.
func (c *requestContext) EnqueueTx(tx pgx.Tx, name string, payload any, opts ...job.EnqueueOption) error {
	if c.jobEnqueuer == nil {
		return job.ErrNotConfigured
	}
	return c.jobEnqueuer.EnqueueTx(c.Context(), tx, name, payload, opts...)
}

func (c *requestContext) Storage() (storage.Storage, error) {
	if c.storage == nil {
		return nil, storage.ErrNotConfigured
	}
	return c.storage, nil
}

func (c *requestContext) Upload(r io.Reader, size int64, opts ...storage.Option) (*storage.FileInfo, error) {
	if c.storage == nil {
		return nil, storage.ErrNotConfigured
	}
	return c.storage.Put(c.Context(), r, size, opts...)
}

func (c *requestContext) Download(key string) (io.ReadCloser, error) {
	if c.storage == nil {
		return nil, storage.ErrNotConfigured
	}
	return c.storage.Get(c.Context(), key)
}

func (c *requestContext) DeleteFile(key string) error {
	if c.storage == nil {
		return storage.ErrNotConfigured
	}
	return c.storage.Delete(c.Context(), key)
}

func (c *requestContext) FileURL(key string, opts ...storage.URLOption) (string, error) {
	if c.storage == nil {
		return "", storage.ErrNotConfigured
	}
	return c.storage.URL(c.Context(), key, opts...)
}

func (c *requestContext) translator() *i18n.Translator {
	if tr, ok := c.Get(TranslatorKey{}).(*i18n.Translator); ok {
		return tr
	}
	return nil
}

func (c *requestContext) T(key string, placeholders ...i18n.M) string {
	if tr := c.translator(); tr != nil {
		return tr.T(key, placeholders...)
	}
	return key
}

func (c *requestContext) Tn(key string, n int, placeholders ...i18n.M) string {
	if tr := c.translator(); tr != nil {
		return tr.Tn(key, n, placeholders...)
	}
	return key
}

func (c *requestContext) Language() string {
	if tr := c.translator(); tr != nil {
		return tr.Language()
	}
	return ""
}

func (c *requestContext) FormatNumber(n float64) string {
	if tr := c.translator(); tr != nil {
		return tr.FormatNumber(n)
	}
	return fmt.Sprintf("%g", n)
}

func (c *requestContext) FormatCurrency(amount float64) string {
	if tr := c.translator(); tr != nil {
		return tr.FormatCurrency(amount)
	}
	return fmt.Sprintf("%.2f", amount)
}

func (c *requestContext) FormatPercent(n float64) string {
	if tr := c.translator(); tr != nil {
		return tr.FormatPercent(n)
	}
	return fmt.Sprintf("%.0f%%", n*100)
}

func (c *requestContext) FormatDate(date time.Time) string {
	if tr := c.translator(); tr != nil {
		return tr.FormatDate(date)
	}
	return date.Format("2006-01-02")
}

func (c *requestContext) FormatTime(t time.Time) string {
	if tr := c.translator(); tr != nil {
		return tr.FormatTime(t)
	}
	return t.Format("15:04:05")
}

func (c *requestContext) FormatDateTime(datetime time.Time) string {
	if tr := c.translator(); tr != nil {
		return tr.FormatDateTime(datetime)
	}
	return datetime.Format("2006-01-02 15:04:05")
}
