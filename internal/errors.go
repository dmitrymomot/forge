package internal

import "net/http"

// HTTPError represents an HTTP error with all data needed for rendering.
// It implements the error interface and provides structured data for
// error handlers to render error pages or toasts.
type HTTPError struct {
	// Err is the underlying error (for logging, not exposed to users).
	Err error

	// Message is the user-facing error message.
	Message string

	// Title is an optional title for the error (defaults derived from Code).
	Title string

	// Detail is an optional extended description.
	Detail string

	// ErrorCode is an application-specific error code (for i18n, client handling).
	ErrorCode string

	// RequestID is the request tracking ID.
	RequestID string

	// Code is the HTTP status code (e.g., 404, 500).
	Code int
}

func (e *HTTPError) Error() string {
	return e.Message
}

func (e *HTTPError) Unwrap() error {
	return e.Err
}

func (e *HTTPError) StatusCode() int {
	return e.Code
}

func (e *HTTPError) StatusText() string {
	return http.StatusText(e.Code)
}

// HTTPErrorOption configures an HTTPError.
type HTTPErrorOption func(*HTTPError)

// NewHTTPError creates a new HTTPError with the given status code and message.
func NewHTTPError(code int, message string) *HTTPError {
	return &HTTPError{
		Code:    code,
		Message: message,
	}
}

func WithTitle(title string) HTTPErrorOption {
	return func(e *HTTPError) {
		e.Title = title
	}
}

func WithDetail(detail string) HTTPErrorOption {
	return func(e *HTTPError) {
		e.Detail = detail
	}
}

func WithErrorCode(code string) HTTPErrorOption {
	return func(e *HTTPError) {
		e.ErrorCode = code
	}
}

func WithRequestID(id string) HTTPErrorOption {
	return func(e *HTTPError) {
		e.RequestID = id
	}
}

func WithError(err error) HTTPErrorOption {
	return func(e *HTTPError) {
		e.Err = err
	}
}

// Convenience constructors for common HTTP errors.

func ErrBadRequest(message string, opts ...HTTPErrorOption) *HTTPError {
	e := NewHTTPError(http.StatusBadRequest, message)
	for _, opt := range opts {
		opt(e)
	}
	return e
}

func ErrUnauthorized(message string, opts ...HTTPErrorOption) *HTTPError {
	e := NewHTTPError(http.StatusUnauthorized, message)
	for _, opt := range opts {
		opt(e)
	}
	return e
}

func ErrForbidden(message string, opts ...HTTPErrorOption) *HTTPError {
	e := NewHTTPError(http.StatusForbidden, message)
	for _, opt := range opts {
		opt(e)
	}
	return e
}

func ErrNotFound(message string, opts ...HTTPErrorOption) *HTTPError {
	e := NewHTTPError(http.StatusNotFound, message)
	for _, opt := range opts {
		opt(e)
	}
	return e
}

func ErrConflict(message string, opts ...HTTPErrorOption) *HTTPError {
	e := NewHTTPError(http.StatusConflict, message)
	for _, opt := range opts {
		opt(e)
	}
	return e
}

func ErrUnprocessable(message string, opts ...HTTPErrorOption) *HTTPError {
	e := NewHTTPError(http.StatusUnprocessableEntity, message)
	for _, opt := range opts {
		opt(e)
	}
	return e
}

func ErrInternal(message string, opts ...HTTPErrorOption) *HTTPError {
	e := NewHTTPError(http.StatusInternalServerError, message)
	for _, opt := range opts {
		opt(e)
	}
	return e
}

func ErrServiceUnavailable(message string, opts ...HTTPErrorOption) *HTTPError {
	e := NewHTTPError(http.StatusServiceUnavailable, message)
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Helper functions for error inspection.

func IsHTTPError(err error) bool {
	_, ok := err.(*HTTPError)
	return ok
}

// AsHTTPError extracts the HTTPError from an error if present.
// Returns nil if the error is not an HTTPError.
func AsHTTPError(err error) *HTTPError {
	if err == nil {
		return nil
	}
	if httpErr, ok := err.(*HTTPError); ok {
		return httpErr
	}
	return nil
}
