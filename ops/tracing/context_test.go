package tracing_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/tracing"
)

func TestSpanFromContextReturnsNoopWhenAbsent(t *testing.T) {
	span := tracing.SpanFromContext(t.Context())
	require.NotNil(t, span)
	assert.False(t, span.IsRecording())
	assert.False(t, span.Context().IsValid())
	span.AddEvent("safe") // must not panic
	span.End()
}

func TestSpanFromContextReturnsCurrentSpan(t *testing.T) {
	tr := tracing.New()
	ctx, span := tr.Start(t.Context(), "s")
	defer span.End()

	assert.Equal(t, span.Context(), tracing.SpanFromContext(ctx).Context())
}

func TestSpanContextFromContextPrecedence(t *testing.T) {
	remote, err := tracing.ParseTraceparent(sampleTraceparent)
	require.NoError(t, err)
	ctx := tracing.ContextWithRemote(t.Context(), remote)
	assert.Equal(t, remote, tracing.SpanContextFromContext(ctx))

	tr := tracing.New()
	ctx, span := tr.Start(ctx, "s")
	defer span.End()
	assert.Equal(t, span.Context(), tracing.SpanContextFromContext(ctx), "live span outranks remote")

	assert.Equal(t, tracing.SpanContext{}, tracing.SpanContextFromContext(t.Context()))
}

func TestLogExtractor(t *testing.T) {
	_, ok := tracing.LogExtractor(t.Context())
	assert.False(t, ok)

	remote, err := tracing.ParseTraceparent(sampleTraceparent)
	require.NoError(t, err)
	ctx := tracing.ContextWithRemote(t.Context(), remote)
	attr, ok := tracing.LogExtractor(ctx)
	require.True(t, ok)
	assert.Equal(t, "trace_id", attr.Key)
	assert.Equal(t, sampleTraceHex, attr.Value.String())

	tr := tracing.New()
	ctx, span := tr.Start(context.Background(), "s")
	defer span.End()
	attr, ok = tracing.LogExtractor(ctx)
	require.True(t, ok)
	assert.Equal(t, slog.KindString, attr.Value.Kind())
	assert.Equal(t, span.Context().TraceID.String(), attr.Value.String())
}
