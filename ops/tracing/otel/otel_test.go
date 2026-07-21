package otel_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/dmitrymomot/forge/ops/tracing"
	"github.com/dmitrymomot/forge/ops/tracing/otel"
	"github.com/dmitrymomot/forge/web/middleware"
)

const sampleTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

func newTracer(t *testing.T) (*otel.Tracer, *tracetest.InMemoryExporter) {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return otel.New(tp), exp
}

func exported(t *testing.T, exp *tracetest.InMemoryExporter) tracetest.SpanStub {
	t.Helper()
	spans := exp.GetSpans()
	require.Len(t, spans, 1)
	return spans[0]
}

func attrValue(t *testing.T, stub tracetest.SpanStub, key string) attribute.Value {
	t.Helper()
	for _, kv := range stub.Attributes {
		if string(kv.Key) == key {
			return kv.Value
		}
	}
	t.Fatalf("attribute %q not exported", key)
	return attribute.Value{}
}

func TestNewPanicsOnNilProvider(t *testing.T) {
	assert.Panics(t, func() { otel.New(nil) })
}

func TestStartExportsSpan(t *testing.T) {
	tr, exp := newTracer(t)
	ctx, span := tr.Start(t.Context(), "work",
		tracing.WithKind(tracing.KindClient),
		tracing.WithAttributes(slog.String("k", "v")),
	)
	assert.True(t, span.IsRecording())
	assert.True(t, span.Context().IsValid())
	assert.Equal(t, span.Context(), tracing.SpanContextFromContext(ctx))
	span.End()

	stub := exported(t, exp)
	assert.Equal(t, "work", stub.Name)
	assert.Equal(t, oteltrace.SpanKindClient, stub.SpanKind)
	assert.Equal(t, "v", attrValue(t, stub, "k").AsString())
}

func TestStartAdoptsForgeRemoteParent(t *testing.T) {
	remote, err := tracing.ParseTraceparent(sampleTraceparent)
	require.NoError(t, err)
	remote.TraceState = "vendor=state"

	tr, exp := newTracer(t)
	ctx := tracing.ContextWithRemote(t.Context(), remote)
	_, span := tr.Start(ctx, "server")
	span.End()

	stub := exported(t, exp)
	assert.Equal(t, remote.TraceID.String(), stub.SpanContext.TraceID().String())
	assert.Equal(t, remote.SpanID.String(), stub.Parent.SpanID().String())
	assert.True(t, stub.Parent.IsRemote())
	assert.Equal(t, "vendor=state", span.Context().TraceState)
}

func TestChildSpansNest(t *testing.T) {
	tr, exp := newTracer(t)
	ctx, parent := tr.Start(t.Context(), "parent")
	_, child := tr.Start(ctx, "child")
	child.End()
	parent.End()

	spans := exp.GetSpans()
	require.Len(t, spans, 2)
	assert.Equal(t, parent.Context().SpanID.String(), spans[0].Parent.SpanID().String())
	assert.Equal(t, parent.Context().TraceID, child.Context().TraceID)
}

func TestWithNewRootDetaches(t *testing.T) {
	tr, exp := newTracer(t)
	ctx, parent := tr.Start(t.Context(), "parent")
	_, detached := tr.Start(ctx, "detached", tracing.WithNewRoot())
	detached.End()
	parent.End()

	assert.NotEqual(t, parent.Context().TraceID, detached.Context().TraceID)
	require.Len(t, exp.GetSpans(), 2)
}

func TestSpanMutators(t *testing.T) {
	tr, exp := newTracer(t)
	_, span := tr.Start(t.Context(), "before")
	span.SetName("after")
	span.SetAttributes(slog.Int("n", 7))
	span.AddEvent("checkpoint", slog.Bool("ok", true))
	span.AddEvent("bare")
	span.RecordError(errors.New("boom"))
	span.RecordError(nil)
	span.SetStatus(tracing.StatusError, "failed")
	span.End()

	stub := exported(t, exp)
	assert.Equal(t, "after", stub.Name)
	assert.Equal(t, int64(7), attrValue(t, stub, "n").AsInt64())
	require.Len(t, stub.Events, 3) // checkpoint + bare + exception
	assert.Equal(t, "checkpoint", stub.Events[0].Name)
	assert.Equal(t, "exception", stub.Events[2].Name)
	assert.Equal(t, codes.Error, stub.Status.Code)
	assert.Equal(t, "failed", stub.Status.Description)
}

func TestStatusOKMapping(t *testing.T) {
	tr, exp := newTracer(t)
	_, span := tr.Start(t.Context(), "s")
	span.SetStatus(tracing.StatusOK, "")
	span.End()

	assert.Equal(t, codes.Ok, exported(t, exp).Status.Code)
}

func TestAttrConversion(t *testing.T) {
	tr, exp := newTracer(t)
	ts := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	_, span := tr.Start(t.Context(), "s")
	span.SetAttributes(
		slog.String("s", "v"),
		slog.Int64("i", -5),
		slog.Uint64("u", 12),
		slog.Uint64("big", 1<<63),
		slog.Float64("f", 1.5),
		slog.Bool("b", true),
		slog.Duration("d", 1500*time.Millisecond),
		slog.Time("t", ts),
		slog.Group("g", slog.String("inner", "x"), slog.Group("gg", slog.Int("deep", 1))),
		slog.Any("any", struct{ X int }{X: 1}),
	)
	span.End()

	stub := exported(t, exp)
	assert.Equal(t, "v", attrValue(t, stub, "s").AsString())
	assert.Equal(t, int64(-5), attrValue(t, stub, "i").AsInt64())
	assert.Equal(t, int64(12), attrValue(t, stub, "u").AsInt64())
	assert.Equal(t, "9223372036854775808", attrValue(t, stub, "big").AsString(), "uint64 overflow stringified")
	assert.InDelta(t, 1.5, attrValue(t, stub, "f").AsFloat64(), 1e-9)
	assert.True(t, attrValue(t, stub, "b").AsBool())
	assert.Equal(t, "1.5s", attrValue(t, stub, "d").AsString())
	assert.Equal(t, "2026-07-21T10:00:00Z", attrValue(t, stub, "t").AsString())
	assert.Equal(t, "x", attrValue(t, stub, "g.inner").AsString())
	assert.Equal(t, int64(1), attrValue(t, stub, "g.gg.deep").AsInt64())
	assert.Equal(t, "{1}", attrValue(t, stub, "any").AsString())
}

func TestUnsampledParentNotRecorded(t *testing.T) {
	remote, err := tracing.ParseTraceparent("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00")
	require.NoError(t, err)

	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exp),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
	)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tr := otel.New(tp)

	ctx := tracing.ContextWithRemote(t.Context(), remote)
	_, span := tr.Start(ctx, "s")
	assert.False(t, span.IsRecording(), "parent-based sampler honors the inbound unsampled flag")
	assert.False(t, span.Context().Sampled)
	span.End()
	assert.Empty(t, exp.GetSpans())
}

func TestWithTracerName(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tr := otel.New(tp, otel.WithTracerName("custom-scope"))
	_, span := tr.Start(t.Context(), "s")
	span.End()

	assert.Equal(t, "custom-scope", exported(t, exp).InstrumentationScope.Name)
}

// TestMiddlewareIntegration wires the core middleware to the otel driver and
// verifies a full server span export with trace continuation.
func TestMiddlewareIntegration(t *testing.T) {
	tr, exp := newTracer(t)
	mux := http.NewServeMux()
	var inHandler tracing.SpanContext
	mux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		inHandler = tracing.SpanContextFromContext(r.Context())
		w.WriteHeader(http.StatusTeapot)
	})
	h := middleware.Wrap(mux, tracing.NewMiddleware(tr))

	r := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	r.Header.Set("traceparent", sampleTraceparent)
	h.ServeHTTP(httptest.NewRecorder(), r)

	stub := exported(t, exp)
	assert.Equal(t, "GET /users/{id}", stub.Name)
	assert.Equal(t, oteltrace.SpanKindServer, stub.SpanKind)
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", stub.SpanContext.TraceID().String())
	assert.Equal(t, "00f067aa0ba902b7", stub.Parent.SpanID().String())
	assert.Equal(t, int64(http.StatusTeapot), attrValue(t, stub, "http.response.status_code").AsInt64())
	assert.True(t, inHandler.IsValid())
	assert.Equal(t, stub.SpanContext.SpanID().String(), inHandler.SpanID.String())
}
