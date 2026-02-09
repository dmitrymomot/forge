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

// RequestIDConfig configures the request ID middleware.
type RequestIDConfig struct {
	ResponseHeader string   `env:"RESPONSE_HEADER" envDefault:"X-Request-ID"`
	Headers        []string `env:"HEADERS"         envSeparator:"," envDefault:"X-Request-ID,X-Correlation-ID"`
}

type requestIDOptions struct {
	generator func() string
}

// RequestIDOption configures runtime dependencies for the request ID middleware.
type RequestIDOption func(*requestIDOptions)

// WithRequestIDGenerator sets a custom ID generator function.
func WithRequestIDGenerator(gen func() string) RequestIDOption {
	return func(o *requestIDOptions) {
		o.generator = gen
	}
}

// RequestID returns middleware that assigns a unique request ID to each request.
// The ID is extracted from request headers (if present) or generated.
// The ID is stored in the context and set as a response header.
func RequestID(cfg RequestIDConfig, opts ...RequestIDOption) internal.Middleware {
	o := &requestIDOptions{}
	for _, opt := range opts {
		opt(o)
	}

	if cfg.ResponseHeader == "" {
		cfg.ResponseHeader = "X-Request-ID"
	}
	if len(cfg.Headers) == 0 {
		cfg.Headers = []string{"X-Request-ID", "X-Correlation-ID"}
	}
	if o.generator == nil {
		o.generator = id.NewULID
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
				reqID = o.generator()
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
