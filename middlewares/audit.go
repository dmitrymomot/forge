package middlewares

import (
	"context"
	"log/slog"
	"time"

	"github.com/dmitrymomot/forge/internal"
	"github.com/dmitrymomot/forge/pkg/id"
)

// Entry represents a single audit log record.
// Fields are flat and SQL-friendly — no nested types.
//
//   - ID: unique identifier (ULID)
//   - Timestamp: when the request was received
//   - RequestID: correlation ID from the RequestID middleware
//   - UserID: authenticated user (empty if anonymous)
//   - Action: what happened (default: HTTP method)
//   - Resource: what was acted on (default: URL path)
//   - Method: HTTP method
//   - Path: URL path
//   - StatusCode: HTTP response status
//   - IPAddress: client IP
//   - UserAgent: client user agent
//   - Error: error message (empty on success)
//   - Metadata: handler-enriched key-value pairs
type Entry struct {
	Timestamp  time.Time
	Metadata   map[string]string // handler-enriched key-value pairs
	ID         string
	RequestID  string
	UserID     string // empty if unauthenticated
	Action     string // default: HTTP method
	Resource   string // default: URL path
	Method     string
	Path       string
	IPAddress  string
	UserAgent  string
	Error      string // empty on success
	StatusCode int
}

// Store defines the interface for persisting audit log entries.
// Implementations handle storage (PostgreSQL, file, queue, etc.).
// The middleware does not own the store lifecycle — no Close method.
type Store interface {
	Log(ctx context.Context, entry *Entry) error
}

type auditEntryKey struct{}

type auditOptions struct {
	logger       *slog.Logger
	skipFunc     func(internal.Context) bool
	actionFunc   func(internal.Context) string
	resourceFunc func(internal.Context) string
	metadataFunc func(internal.Context) map[string]string
	timeout      time.Duration
}

// AuditOption configures the AuditLog middleware.
type AuditOption func(*auditOptions)

// WithAuditLogger sets the logger for store errors.
// Default: slog.Default().
func WithAuditLogger(l *slog.Logger) AuditOption {
	return func(o *auditOptions) {
		o.logger = l
	}
}

// WithAuditSkipFunc sets a function to bypass audit logging for specific requests.
// Use this to skip health checks, static files, etc.
func WithAuditSkipFunc(fn func(internal.Context) bool) AuditOption {
	return func(o *auditOptions) {
		o.skipFunc = fn
	}
}

// WithAuditActionFunc sets a custom action extractor.
// Called after the handler completes. Default: HTTP method.
func WithAuditActionFunc(fn func(internal.Context) string) AuditOption {
	return func(o *auditOptions) {
		o.actionFunc = fn
	}
}

// WithAuditResourceFunc sets a custom resource extractor.
// Called after the handler completes. Default: URL path.
func WithAuditResourceFunc(fn func(internal.Context) string) AuditOption {
	return func(o *auditOptions) {
		o.resourceFunc = fn
	}
}

// WithAuditMetadataFunc sets a global metadata extraction function.
// Called after the handler completes. Handler-set metadata takes precedence.
func WithAuditMetadataFunc(fn func(internal.Context) map[string]string) AuditOption {
	return func(o *auditOptions) {
		o.metadataFunc = fn
	}
}

// WithAuditTimeout sets the timeout for the async store.Log call.
// Default: 5 seconds.
func WithAuditTimeout(d time.Duration) AuditOption {
	return func(o *auditOptions) {
		o.timeout = d
	}
}

// AuditLog returns middleware that records audit trail entries.
// Entries are written asynchronously after the handler completes.
// The store.Log call runs in a goroutine with a timeout context,
// so it never blocks the HTTP response.
// Panics if store is nil.
func AuditLog(store Store, opts ...AuditOption) internal.Middleware {
	if store == nil {
		panic("middlewares: AuditLog requires a non-nil store")
	}

	o := &auditOptions{
		timeout: 5 * time.Second,
	}
	for _, opt := range opts {
		opt(o)
	}

	if o.logger == nil {
		o.logger = slog.Default()
	}

	return func(next internal.HandlerFunc) internal.HandlerFunc {
		return func(c internal.Context) error {
			// Skip check
			if o.skipFunc != nil && o.skipFunc(c) {
				return next(c)
			}

			// Create entry with pre-handler fields
			entry := &Entry{
				ID:        id.NewULID(),
				Timestamp: time.Now(),
				Method:    c.Request().Method,
				Path:      c.Request().URL.Path,
				IPAddress: c.Request().RemoteAddr,
				UserAgent: c.Request().UserAgent(),
				Metadata:  make(map[string]string),
			}

			// Store entry in context for handler enrichment
			c.Set(auditEntryKey{}, entry)

			// Execute handler
			handlerErr := next(c)

			// Fill post-handler fields
			entry.UserID = c.UserID()
			entry.RequestID = GetRequestID(c)

			// Determine status code
			if status := c.ResponseWriter().Status(); status != 0 {
				entry.StatusCode = status
			} else if handlerErr != nil {
				if httpErr := internal.AsHTTPError(handlerErr); httpErr != nil {
					entry.StatusCode = httpErr.StatusCode()
				} else {
					entry.StatusCode = 500
				}
			} else {
				entry.StatusCode = 200
			}

			// Capture error
			if handlerErr != nil {
				entry.Error = handlerErr.Error()
			}

			// Set action (custom or default)
			if o.actionFunc != nil {
				entry.Action = o.actionFunc(c)
			} else {
				entry.Action = c.Request().Method
			}

			// Set resource (custom or default)
			if o.resourceFunc != nil {
				entry.Resource = o.resourceFunc(c)
			} else {
				entry.Resource = c.Request().URL.Path
			}

			// Merge metadata from option func (handler values take precedence)
			if o.metadataFunc != nil {
				global := o.metadataFunc(c)
				for k, v := range global {
					if _, exists := entry.Metadata[k]; !exists {
						entry.Metadata[k] = v
					}
				}
			}

			// Async write — goroutine has zero dependency on request context
			logger := o.logger
			timeout := o.timeout
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), timeout)
				defer cancel()
				if err := store.Log(ctx, entry); err != nil {
					logger.Warn("audit: failed to log entry", "error", err)
				}
			}()

			return handlerErr
		}
	}
}

// GetAuditEntry extracts the audit entry from the context.
// Returns nil if the AuditLog middleware is not applied.
func GetAuditEntry(c internal.Context) *Entry {
	v, ok := c.Get(auditEntryKey{}).(*Entry)
	if !ok {
		return nil
	}
	return v
}

// SetAuditMetadata adds a key-value pair to the audit entry metadata.
// No-op if the AuditLog middleware is not applied.
func SetAuditMetadata(c internal.Context, key, value string) {
	entry := GetAuditEntry(c)
	if entry == nil {
		return
	}
	entry.Metadata[key] = value
}
