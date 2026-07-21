package tracing_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/tracing"
)

func TestPropagationHeadersEmptyContext(t *testing.T) {
	assert.Nil(t, tracing.PropagationHeaders(t.Context()))
}

func TestPropagationHeadersFromSpan(t *testing.T) {
	tr := tracing.New()
	ctx, span := tr.Start(t.Context(), "s")
	defer span.End()

	h := tracing.PropagationHeaders(ctx)
	require.NotNil(t, h)
	assert.Equal(t, span.Context().Traceparent(), h.Get("Traceparent"))
	assert.Empty(t, h.Get("Tracestate"))
}

func TestPropagationHeadersCarryTracestate(t *testing.T) {
	remote, err := tracing.ParseTraceparent(sampleTraceparent)
	require.NoError(t, err)
	remote.TraceState = "vendor=state"
	ctx := tracing.ContextWithRemote(t.Context(), remote)

	h := tracing.PropagationHeaders(ctx)
	require.NotNil(t, h)
	assert.Equal(t, sampleTraceparent, h.Get("Traceparent"))
	assert.Equal(t, "vendor=state", h.Get("Tracestate"))
}

func TestInject(t *testing.T) {
	h := http.Header{}
	tracing.Inject(t.Context(), h)
	assert.Empty(t, h)

	tr := tracing.New()
	ctx, span := tr.Start(t.Context(), "s")
	defer span.End()
	assert.NotPanics(t, func() { tracing.Inject(ctx, nil) }, "nil header is a no-op")

	remote, err := tracing.ParseTraceparent(sampleTraceparent)
	require.NoError(t, err)
	remote.TraceState = "vendor=state"
	tracing.Inject(tracing.ContextWithRemote(t.Context(), remote), h)
	assert.Equal(t, sampleTraceparent, h.Get("Traceparent"))
	assert.Equal(t, "vendor=state", h.Get("Tracestate"))
}

// TestEndToEndPropagation proves the full hop: middleware continues an inbound
// trace and PropagationHeaders forwards the server span downstream.
func TestEndToEndPropagation(t *testing.T) {
	tr := tracing.New()
	var outbound http.Header
	h := tracing.NewMiddleware(tr)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		outbound = tracing.PropagationHeaders(r.Context())
	}))

	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("traceparent", sampleTraceparent)
	r.Header.Set("tracestate", "vendor=state")
	h.ServeHTTP(httptest.NewRecorder(), r)

	require.NotNil(t, outbound)
	forwarded, err := tracing.ParseTraceparent(outbound.Get("Traceparent"))
	require.NoError(t, err)
	assert.Equal(t, sampleTraceHex, forwarded.TraceID.String(), "same trace continues downstream")
	assert.NotEqual(t, sampleSpanHex, forwarded.SpanID.String(), "parent id is this hop's span")
	assert.True(t, forwarded.Sampled)
	assert.Equal(t, "vendor=state", outbound.Get("Tracestate"))
}
