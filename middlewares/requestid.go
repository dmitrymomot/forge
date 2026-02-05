package middlewares

import (
	"context"
	"log/slog"

	"github.com/dmitrymomot/forge/internal"
	"github.com/dmitrymomot/forge/pkg/id"
	"github.com/dmitrymomot/forge/pkg/logger"
)

// requestIDKey is the context key for storing the request ID.
type requestIDKey struct{}

// DefaultRequestIDHeaders are the headers checked (in order) for an existing request ID.
var DefaultRequestIDHeaders = []string{"X-Request-ID", "X-Request-Id", "X-Correlation-ID"}

// RequestIDConfig configures the request ID middleware.
type RequestIDConfig struct {
	Generator      func() string // ID generator function
	ResponseHeader string        // Response header name
	Headers        []string      // Headers to check for existing ID (in order)
}

// RequestIDOption configures RequestIDConfig.
type RequestIDOption func(*RequestIDConfig)

// WithRequestIDHeaders sets the headers to check for existing request IDs.
func WithRequestIDHeaders(headers ...string) RequestIDOption {
	return func(cfg *RequestIDConfig) {
		cfg.Headers = headers
	}
}

// WithRequestIDGenerator sets a custom ID generator function.
func WithRequestIDGenerator(gen func() string) RequestIDOption {
	return func(cfg *RequestIDConfig) {
		cfg.Generator = gen
	}
}

// WithRequestIDResponseHeader sets the response header name.
func WithRequestIDResponseHeader(header string) RequestIDOption {
	return func(cfg *RequestIDConfig) {
		cfg.ResponseHeader = header
	}
}

// RequestID returns middleware that assigns a unique request ID to each request.
// The ID is extracted from request headers (if present) or generated.
// The ID is stored in the context and set as a response header.
func RequestID(opts ...RequestIDOption) internal.Middleware {
	cfg := &RequestIDConfig{
		Headers:        DefaultRequestIDHeaders,
		Generator:      id.NewULID,
		ResponseHeader: "X-Request-ID",
	}

	for _, opt := range opts {
		opt(cfg)
	}

	return func(next internal.HandlerFunc) internal.HandlerFunc {
		return func(c internal.Context) error {
			// Check headers in priority order; first match is used to preserve upstream tracing IDs
			var reqID string
			for _, header := range cfg.Headers {
				if v := c.Header(header); v != "" {
					reqID = v
					break
				}
			}

			if reqID == "" {
				reqID = cfg.Generator()
			}

			c.Set(requestIDKey{}, reqID)
			c.SetHeader(cfg.ResponseHeader, reqID)

			return next(c)
		}
	}
}

// GetRequestID extracts the request ID from the context.
// Returns an empty string if no request ID is set.
func GetRequestID(c internal.Context) string {
	if v, ok := c.Get(requestIDKey{}).(string); ok {
		return v
	}
	return ""
}

// RequestIDExtractor returns a ContextExtractor for use with WithLogger.
// Automatically adds "request_id" to all log entries.
func RequestIDExtractor() logger.ContextExtractor {
	return func(ctx context.Context) (slog.Attr, bool) {
		if v, ok := ctx.Value(requestIDKey{}).(string); ok && v != "" {
			return slog.String("request_id", v), true
		}
		return slog.Attr{}, false
	}
}
