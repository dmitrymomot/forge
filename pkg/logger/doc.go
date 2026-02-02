// Package logger provides structured logging with context extraction and Sentry integration.
//
// This package extends the standard library's log/slog with two key capabilities:
// automatic context-based attribute injection and optional Sentry error reporting.
// It is designed for production applications that need consistent, enriched logs
// with minimal boilerplate.
//
// # Overview
//
// The package provides:
//   - Context extractors that automatically inject request-scoped values (e.g., request IDs, user IDs)
//   - A decorator pattern that wraps any slog.Handler to add extraction behavior
//   - Sentry integration for error tracking with graceful fallback when unconfigured
//   - Multi-handler support for routing logs to multiple destinations
//
// # Basic Usage
//
// Create a logger with context extractors:
//
//	// Define an extractor for request ID
//	requestIDExtractor := func(ctx context.Context) (slog.Attr, bool) {
//		if reqID, ok := ctx.Value("request_id").(string); ok && reqID != "" {
//			return slog.String("request_id", reqID), true
//		}
//		return slog.Attr{}, false
//	}
//
//	// Create logger with extractors
//	log := logger.New(requestIDExtractor)
//
//	// Use with context - request_id is automatically included
//	ctx := context.WithValue(context.Background(), "request_id", "abc-123")
//	log.InfoContext(ctx, "request processed", slog.Int("status", 200))
//	// Output: {"level":"INFO","msg":"request processed","status":200,"request_id":"abc-123"}
//
// # Sentry Integration
//
// For production error tracking, use NewWithSentry:
//
//	cfg := logger.SentryConfig{
//		DSN:         os.Getenv("SENTRY_DSN"),
//		Environment: "production",
//		MinLevel:    slog.LevelWarn, // Send warnings and errors to Sentry
//	}
//
//	log := logger.NewWithSentry(cfg, requestIDExtractor)
//
//	// Errors create Issues in Sentry, warnings are stored for context
//	log.ErrorContext(ctx, "payment failed", slog.String("user_id", "user-456"))
//
// If SENTRY_DSN is empty, the logger gracefully falls back to stdout-only logging,
// making it safe to use the same code path in development and production.
//
// # Context Extractors
//
// A ContextExtractor is a function that extracts a log attribute from context:
//
//	type ContextExtractor func(ctx context.Context) (slog.Attr, bool)
//
// Extractors are called on every log call, ensuring fresh values for request-scoped data.
// Return false from the extractor to skip adding the attribute for that log entry.
//
// Common extractors include:
//   - Request ID extractor for HTTP request tracing
//   - User ID extractor for authentication context
//   - Tenant ID extractor for multi-tenant applications
//
// # Handler Decoration
//
// The LogHandlerDecorator can wrap any slog.Handler to add context extraction:
//
//	// Wrap a custom handler
//	jsonHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
//	decorated := logger.NewLogHandlerDecorator(jsonHandler, extractors...)
//	log := slog.New(decorated)
//
// This allows using context extractors with any handler implementation.
//
// # Architecture
//
// The package uses several design patterns:
//
// Decorator Pattern: LogHandlerDecorator wraps any slog.Handler, intercepting
// Handle calls to inject extracted attributes before delegating to the underlying handler.
//
// Multi-Handler Pattern: An internal multiHandler forwards logs to multiple destinations,
// enabling simultaneous stdout and Sentry logging.
//
// Graceful Degradation: Sentry integration fails gracefully - if DSN is missing or
// initialization fails, logging continues to stdout without disruption.
package logger
