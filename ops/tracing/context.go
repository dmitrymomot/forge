package tracing

import (
	"context"
	"log/slog"

	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/ops/logger"
)

var (
	spanKey   = ctxkey.New[Span]("tracing.span")
	remoteKey = ctxkey.New[SpanContext]("tracing.remote")
)

// ContextWithSpan returns ctx carrying s as the current span. Tracer
// implementations call it from Start; consumers rarely need it directly.
func ContextWithSpan(ctx context.Context, s Span) context.Context {
	return spanKey.With(ctx, s)
}

// SpanFromContext returns the current span, or a no-op span when none is
// present, so call sites add attributes and events without nil checks.
func SpanFromContext(ctx context.Context) Span {
	if s, ok := spanKey.From(ctx); ok {
		return s
	}
	return noopSpan{}
}

// ContextWithRemote returns ctx carrying a remote parent span context — one
// parsed from an inbound traceparent header (NewMiddleware does this) or
// restored from a queue message. Tracers adopt it as the parent when no live
// span is in ctx, and SpanContextFromContext falls back to it, so trace
// correlation works even before (or without) a local span.
func ContextWithRemote(ctx context.Context, sc SpanContext) context.Context {
	return remoteKey.With(ctx, sc)
}

// SpanContextFromContext returns the identity of the current span when one is
// in ctx, else the remote parent stored by ContextWithRemote, else the zero
// SpanContext. LogExtractor and header propagation read through this.
func SpanContextFromContext(ctx context.Context) SpanContext {
	if s, ok := spanKey.From(ctx); ok {
		if sc := s.Context(); sc.IsValid() {
			return sc
		}
	}
	if sc, ok := remoteKey.From(ctx); ok {
		return sc
	}
	return SpanContext{}
}

// LogExtractor adds a "trace_id" attribute when ctx carries a span or a remote
// parent. Register with logger.WithContextExtractors to correlate every log
// line with its trace.
var LogExtractor logger.ContextExtractor = func(ctx context.Context) (slog.Attr, bool) {
	sc := SpanContextFromContext(ctx)
	if !sc.TraceID.IsValid() {
		return slog.Attr{}, false
	}
	return slog.String("trace_id", sc.TraceID.String()), true
}

type noopSpan struct{}

func (noopSpan) Context() SpanContext          { return SpanContext{} }
func (noopSpan) IsRecording() bool             { return false }
func (noopSpan) SetName(string)                {}
func (noopSpan) SetAttributes(...slog.Attr)    {}
func (noopSpan) AddEvent(string, ...slog.Attr) {}
func (noopSpan) RecordError(error)             {}
func (noopSpan) SetStatus(Status, string)      {}
func (noopSpan) End()                          {}
