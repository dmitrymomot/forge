package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/dmitrymomot/forge/pkg/binder"
	"github.com/dmitrymomot/forge/pkg/htmx"
	"github.com/dmitrymomot/forge/pkg/sanitizer"
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
}

// requestContext implements the Context interface.
type requestContext struct {
	request  *http.Request
	response http.ResponseWriter
	written  bool
	logger   *slog.Logger
}

// newContext creates a new context.
func newContext(w http.ResponseWriter, r *http.Request, logger *slog.Logger) *requestContext {
	return &requestContext{
		request:  r,
		response: w,
		logger:   logger,
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
	c.written = true
	return json.NewEncoder(c.response).Encode(v)
}

// String writes a plain text response with the given status code.
func (c *requestContext) String(code int, s string) error {
	c.response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.response.WriteHeader(code)
	c.written = true
	_, err := c.response.Write([]byte(s))
	return err
}

// NoContent writes a response with no body.
func (c *requestContext) NoContent(code int) error {
	c.response.WriteHeader(code)
	c.written = true
	return nil
}

// Redirect redirects to the given URL with the given status code.
// Handles both regular HTTP redirects and HTMX requests.
func (c *requestContext) Redirect(code int, url string) error {
	c.written = true
	htmx.RedirectWithStatus(c.response, c.request, url, code)
	return nil
}

// Error writes an error response.
func (c *requestContext) Error(code int, message string) error {
	c.written = true
	http.Error(c.response, message, code)
	return nil
}

// IsHTMX returns true if the request originated from HTMX.
func (c *requestContext) IsHTMX() bool {
	return htmx.IsHTMX(c.request)
}

// Render renders a component with the given status code.
// For HTMX requests: always uses HTTP 200 (HTMX requires 2xx for swapping).
// For regular requests: uses the provided status code.
func (c *requestContext) Render(code int, component Component) error {
	c.response.Header().Set("Content-Type", "text/html; charset=utf-8")
	if htmx.IsHTMX(c.request) {
		code = http.StatusOK
	}
	c.response.WriteHeader(code)
	c.written = true
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
	return c.written
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
