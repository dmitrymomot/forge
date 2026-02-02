package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/dmitrymomot/forge/pkg/binder"
	"github.com/dmitrymomot/forge/pkg/cookie"
	"github.com/dmitrymomot/forge/pkg/htmx"
	"github.com/dmitrymomot/forge/pkg/sanitizer"
	"github.com/dmitrymomot/forge/pkg/session"
	"github.com/dmitrymomot/forge/pkg/validator"
	"github.com/go-chi/chi/v5"
)

// ValidationErrors is a collection of validation errors.
type ValidationErrors = validator.ValidationErrors

// Component is the interface for renderable templates.
// This is compatible with templ.Component.
type Component interface {
	Render(ctx context.Context, w io.Writer) error
}

// Context provides request/response access and helper methods.
type Context interface {
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

	// Error writes an error response.
	Error(code int, message string) error

	// IsHTMX returns true if the request originated from HTMX.
	IsHTMX() bool

	// Render renders a component with the given status code.
	// For HTMX requests: always uses HTTP 200 (HTMX requires 2xx for swapping).
	// For regular requests: uses the provided status code.
	// Compatible with templ.Component.
	Render(code int, component Component) error

	// RenderPartial renders different components based on request type.
	// For HTMX requests: renders partial with HTTP 200.
	// For regular requests: renders fullPage with the provided status code.
	RenderPartial(code int, fullPage, partial Component) error

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
}

// requestContext implements the Context interface.
type requestContext struct {
	request        *http.Request
	response       http.ResponseWriter
	responseWriter *ResponseWriter
	logger         *slog.Logger
	cookieManager  *cookie.Manager

	// Session management
	sessionManager        *SessionManager
	session               *session.Session
	sessionLoaded         bool
	sessionHookRegistered bool
}

// newContext creates a new context with the response wrapper.
func newContext(w http.ResponseWriter, r *http.Request, logger *slog.Logger, cm *cookie.Manager, sm *SessionManager) *requestContext {
	// Create response wrapper
	rw := NewResponseWriter(w, htmx.IsHTMX(r))

	return &requestContext{
		request:        r,
		response:       rw,
		responseWriter: rw,
		logger:         logger,
		cookieManager:  cm,
		sessionManager: sm,
	}
}

// Request returns the underlying *http.Request.
func (c *requestContext) Request() *http.Request {
	return c.request
}

// Response returns the underlying http.ResponseWriter.
func (c *requestContext) Response() http.ResponseWriter {
	return c.response
}

// Context returns the request's context.Context.
func (c *requestContext) Context() context.Context {
	return c.request.Context()
}

// Param returns the URL parameter value by name.
func (c *requestContext) Param(name string) string {
	return chi.URLParam(c.request, name)
}

// Query returns the query parameter value by name.
func (c *requestContext) Query(name string) string {
	return c.request.URL.Query().Get(name)
}

// QueryDefault returns the query parameter value or a default.
func (c *requestContext) QueryDefault(name, defaultValue string) string {
	v := c.request.URL.Query().Get(name)
	if v == "" {
		return defaultValue
	}
	return v
}

// Header returns the request header value by name.
func (c *requestContext) Header(name string) string {
	return c.request.Header.Get(name)
}

// SetHeader sets a response header.
func (c *requestContext) SetHeader(name, value string) {
	c.response.Header().Set(name, value)
}

// JSON writes a JSON response with the given status code.
func (c *requestContext) JSON(code int, v any) error {
	c.response.Header().Set("Content-Type", "application/json; charset=utf-8")
	c.response.WriteHeader(code)
	return json.NewEncoder(c.response).Encode(v)
}

// String writes a plain text response with the given status code.
func (c *requestContext) String(code int, s string) error {
	c.response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.response.WriteHeader(code)
	_, err := c.response.Write([]byte(s))
	return err
}

// NoContent writes a response with no body.
func (c *requestContext) NoContent(code int) error {
	c.response.WriteHeader(code)
	return nil
}

// Redirect redirects to the given URL with the given status code.
// Handles both regular HTTP redirects and HTMX requests.
func (c *requestContext) Redirect(code int, url string) error {
	htmx.RedirectWithStatus(c.response, c.request, url, code)
	return nil
}

// Error writes an error response.
func (c *requestContext) Error(code int, message string) error {
	http.Error(c.response, message, code)
	return nil
}

// IsHTMX returns true if the request originated from HTMX.
func (c *requestContext) IsHTMX() bool {
	return htmx.IsHTMX(c.request)
}

// Render renders a component with the given status code.
// For HTMX requests: the ResponseWriter transforms non-200 to 200.
// For regular requests: uses the provided status code.
func (c *requestContext) Render(code int, component Component) error {
	c.response.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.response.WriteHeader(code)
	return component.Render(c.request.Context(), c.response)
}

// RenderPartial renders different components based on request type.
// For HTMX requests: renders partial with HTTP 200.
// For regular requests: renders fullPage with the provided status code.
func (c *requestContext) RenderPartial(code int, fullPage, partial Component) error {
	if htmx.IsHTMX(c.request) {
		return c.Render(code, partial)
	}
	return c.Render(code, fullPage)
}

// Bind binds form data, sanitizes, and validates into a struct.
func (c *requestContext) Bind(v any) (ValidationErrors, error) {
	if err := binder.Form()(c.request, v); err != nil {
		return nil, fmt.Errorf("bind form: %w", err)
	}
	if err := sanitizer.SanitizeStruct(v); err != nil {
		return nil, fmt.Errorf("sanitize: %w", err)
	}
	if err := validator.ValidateStruct(v); err != nil {
		if validator.IsValidationError(err) {
			return validator.ExtractValidationErrors(err), nil
		}
		return nil, fmt.Errorf("validate: %w", err)
	}
	return nil, nil
}

// BindQuery binds query parameters, sanitizes, and validates into a struct.
func (c *requestContext) BindQuery(v any) (ValidationErrors, error) {
	if err := binder.Query()(c.request, v); err != nil {
		return nil, fmt.Errorf("bind query: %w", err)
	}
	if err := sanitizer.SanitizeStruct(v); err != nil {
		return nil, fmt.Errorf("sanitize: %w", err)
	}
	if err := validator.ValidateStruct(v); err != nil {
		if validator.IsValidationError(err) {
			return validator.ExtractValidationErrors(err), nil
		}
		return nil, fmt.Errorf("validate: %w", err)
	}
	return nil, nil
}

// BindJSON binds JSON body, sanitizes, and validates into a struct.
func (c *requestContext) BindJSON(v any) (ValidationErrors, error) {
	if err := binder.JSON()(c.request, v); err != nil {
		return nil, fmt.Errorf("bind json: %w", err)
	}
	if err := sanitizer.SanitizeStruct(v); err != nil {
		return nil, fmt.Errorf("sanitize: %w", err)
	}
	if err := validator.ValidateStruct(v); err != nil {
		if validator.IsValidationError(err) {
			return validator.ExtractValidationErrors(err), nil
		}
		return nil, fmt.Errorf("validate: %w", err)
	}
	return nil, nil
}

// Written returns true if a response has already been written.
func (c *requestContext) Written() bool {
	return c.responseWriter.Written()
}

// Logger returns the logger for advanced usage.
func (c *requestContext) Logger() *slog.Logger {
	return c.logger
}

// LogDebug logs a debug message with optional attributes.
func (c *requestContext) LogDebug(msg string, attrs ...any) {
	c.logger.DebugContext(c.request.Context(), msg, attrs...)
}

// LogInfo logs an info message with optional attributes.
func (c *requestContext) LogInfo(msg string, attrs ...any) {
	c.logger.InfoContext(c.request.Context(), msg, attrs...)
}

// LogWarn logs a warning message with optional attributes.
func (c *requestContext) LogWarn(msg string, attrs ...any) {
	c.logger.WarnContext(c.request.Context(), msg, attrs...)
}

// LogError logs an error message with optional attributes.
func (c *requestContext) LogError(msg string, attrs ...any) {
	c.logger.ErrorContext(c.request.Context(), msg, attrs...)
}

// Set stores a value in the request context.
func (c *requestContext) Set(key, value any) {
	ctx := context.WithValue(c.request.Context(), key, value)
	c.request = c.request.WithContext(ctx)
}

// Get retrieves a value from the request context.
func (c *requestContext) Get(key any) any {
	return c.request.Context().Value(key)
}

// Cookie returns a plain cookie value.
func (c *requestContext) Cookie(name string) (string, error) {
	return c.cookieManager.Get(c.request, name)
}

// SetCookie sets a plain cookie.
func (c *requestContext) SetCookie(name, value string, maxAge int) {
	c.cookieManager.Set(c.response, name, value, maxAge)
}

// DeleteCookie removes a cookie.
func (c *requestContext) DeleteCookie(name string) {
	c.cookieManager.Delete(c.response, name)
}

// CookieSigned returns a signed cookie value.
func (c *requestContext) CookieSigned(name string) (string, error) {
	return c.cookieManager.GetSigned(c.request, name)
}

// SetCookieSigned sets a signed cookie.
func (c *requestContext) SetCookieSigned(name, value string, maxAge int) error {
	return c.cookieManager.SetSigned(c.response, name, value, maxAge)
}

// CookieEncrypted returns an encrypted cookie value.
func (c *requestContext) CookieEncrypted(name string) (string, error) {
	return c.cookieManager.GetEncrypted(c.request, name)
}

// SetCookieEncrypted sets an encrypted cookie.
func (c *requestContext) SetCookieEncrypted(name, value string, maxAge int) error {
	return c.cookieManager.SetEncrypted(c.response, name, value, maxAge)
}

// Flash reads and deletes a flash message.
func (c *requestContext) Flash(key string, dest any) error {
	return c.cookieManager.Flash(c.response, c.request, key, dest)
}

// SetFlash sets a flash message.
func (c *requestContext) SetFlash(key string, value any) error {
	return c.cookieManager.SetFlash(c.response, key, value)
}

// registerSessionHook ensures the session flush hook is registered once.
// This is called lazily when the session is first accessed.
func (c *requestContext) registerSessionHook() {
	if c.sessionHookRegistered || c.sessionManager == nil || c.responseWriter == nil {
		return
	}
	c.sessionHookRegistered = true
	c.responseWriter.OnBeforeWrite(func() {
		if c.session != nil && c.session.IsDirty() {
			// Best-effort save; errors are logged but not propagated
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
	sess, _ := c.Session()
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

// SessionValue retrieves a value from the session.
// Returns session.ErrNotConfigured if WithSession was not called.
// Returns session.ErrNotFound if no session exists.
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

// SetSessionValue stores a value in the session.
// Returns session.ErrNotConfigured if WithSession was not called.
// Returns session.ErrNotFound if no session exists.
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

// DeleteSessionValue removes a value from the session.
// Returns session.ErrNotConfigured if WithSession was not called.
// Returns session.ErrNotFound if no session exists.
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

// DestroySession removes the session and clears the cookie.
// Returns session.ErrNotConfigured if WithSession was not called.
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

// ResponseWriter returns the underlying ResponseWriter for advanced usage.
func (c *requestContext) ResponseWriter() *ResponseWriter {
	return c.responseWriter
}
