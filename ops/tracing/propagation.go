package tracing

import (
	"context"
	"net/http"
)

// Inject writes the trace context from ctx onto h as W3C traceparent (and
// tracestate, when present) headers. It is a no-op when h is nil or ctx
// carries no valid span context, so it is always safe to call.
func Inject(ctx context.Context, h http.Header) {
	sc := SpanContextFromContext(ctx)
	if h == nil || !sc.IsValid() {
		return
	}
	h.Set(TraceparentHeader, sc.Traceparent())
	if sc.TraceState != "" {
		h.Set(TracestateHeader, sc.TraceState)
	}
}

// PropagationHeaders returns the W3C trace context headers for ctx — nil when
// none — shaped for httpclient's context-header seam:
//
//	client := httpclient.New(httpclient.WithContextHeaders(tracing.PropagationHeaders))
//
// Every request made with a span (or remote parent) in its context then joins
// the trace downstream.
func PropagationHeaders(ctx context.Context) http.Header {
	sc := SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return nil
	}
	h := make(http.Header, 2)
	h[TraceparentHeader] = []string{sc.Traceparent()}
	if sc.TraceState != "" {
		h[TracestateHeader] = []string{sc.TraceState}
	}
	return h
}
