// Package tracing is a minimal distributed-tracing facade: a Tracer/Span
// seam, W3C trace-context middleware, a trace_id log extractor, and header
// propagation for outbound calls. Application and forge packages start spans
// against the Tracer seam; swapping the backend (tracing/otel is the only
// driver) changes wiring, never call sites. Attributes are plain slog.Attr —
// the same vocabulary as logging.
//
// # Zero-dependency default
//
// New returns a propagation-only tracer: it continues the inbound trace (or
// mints a new one) and gives every span a fresh id, but records nothing — the
// full correlation story with no exporter and no dependencies:
//
//	tr := tracing.New()
//
//	log, err := logger.New(logger.WithContextExtractors(tracing.LogExtractor))
//
//	mux := http.NewServeMux()
//	mux.Handle("GET /users/{id}", getUser)
//	handler := middleware.Wrap(mux,
//		tracing.NewMiddleware(tr, tracing.WithSkip(func(r *http.Request) bool {
//			return r.URL.Path == "/livez"
//		})),
//		recoverer.New(), // inside tracing: panicking requests still end their span
//	)
//
// The middleware parses an inbound traceparent into a remote parent, wraps the
// handler in a KindServer span named "GET /users/{id}" (the matched r.Pattern;
// WithPathFunc is the hook for non-ServeMux routers), and marks 5xx responses
// StatusError. Every log line through the extractor carries trace_id.
//
// # Outbound propagation
//
// Pair with httpclient so outbound calls join the trace:
//
//	client := httpclient.New(httpclient.WithContextHeaders(tracing.PropagationHeaders))
//
// For non-httpclient transports, Inject(ctx, header) writes the same headers.
// Cross-queue hand-off works the same way: carry SpanContextFromContext(ctx)
// in the message (its Traceparent() string) and restore it on the consumer
// side with ParseTraceparent + ContextWithRemote.
//
// # Instrumenting code
//
//	ctx, span := tr.Start(ctx, "sync invoices", tracing.WithAttributes(slog.Int("batch", n)))
//	defer span.End()
//	...
//	if err != nil {
//		span.RecordError(err)
//		span.SetStatus(tracing.StatusError, "sync failed")
//	}
//
// SpanFromContext(ctx) returns the current span (a no-op span when absent) so
// helpers deep in the call stack can add events without plumbing.
//
// # Multi-tenant scoping
//
// WithContextAttr is the construction-time tenancy seam; single-tenant apps
// skip it and pay nothing:
//
//	tracing.NewMiddleware(tr,
//		tracing.WithContextAttr("tenant", func(ctx context.Context) string {
//			id, _ := tenant.FromContext(ctx)
//			return id // "" records as "unknown" (fail-closed)
//		}),
//	)
//
// # OpenTelemetry
//
// tracing/otel adapts any otel TracerProvider (sampling and export stay SDK
// concerns, wired in the consumer's main):
//
//	tr := otel.New(sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter)))
//
// Packages that emit spans take a Tracer option defaulting to NewNoop, which
// still passes an inbound trace through to logs and outbound headers.
package tracing
