package tracing_test

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/tracing"
)

func TestNewStartsRootSpan(t *testing.T) {
	tr := tracing.New()
	ctx, span := tr.Start(t.Context(), "root")
	defer span.End()

	sc := span.Context()
	assert.True(t, sc.IsValid())
	assert.True(t, sc.Sampled, "propagation-only roots must not suppress downstream sampling")
	assert.False(t, span.IsRecording())
	assert.Equal(t, sc, tracing.SpanContextFromContext(ctx))
}

func TestNewChildInheritsTraceID(t *testing.T) {
	tr := tracing.New()
	ctx, parent := tr.Start(t.Context(), "parent")
	defer parent.End()
	_, child := tr.Start(ctx, "child")
	defer child.End()

	assert.Equal(t, parent.Context().TraceID, child.Context().TraceID)
	assert.NotEqual(t, parent.Context().SpanID, child.Context().SpanID)
}

func TestNewAdoptsRemoteParent(t *testing.T) {
	remote, err := tracing.ParseTraceparent("00-" + sampleTraceHex + "-" + sampleSpanHex + "-00")
	require.NoError(t, err)
	remote.TraceState = "vendor=state"

	tr := tracing.New()
	ctx := tracing.ContextWithRemote(t.Context(), remote)
	_, span := tr.Start(ctx, "server")
	defer span.End()

	sc := span.Context()
	assert.Equal(t, remote.TraceID, sc.TraceID)
	assert.NotEqual(t, remote.SpanID, sc.SpanID, "each hop gets its own span id")
	assert.False(t, sc.Sampled, "inbound sampling decision is inherited")
	assert.Equal(t, "vendor=state", sc.TraceState)
}

func TestNewWithNewRootIgnoresParent(t *testing.T) {
	tr := tracing.New()
	ctx, parent := tr.Start(t.Context(), "parent")
	defer parent.End()
	_, span := tr.Start(ctx, "detached", tracing.WithNewRoot())
	defer span.End()

	assert.NotEqual(t, parent.Context().TraceID, span.Context().TraceID)
	assert.True(t, span.Context().Sampled)
}

func TestNewSpanMutatorsAreNoops(t *testing.T) {
	tr := tracing.New()
	_, span := tr.Start(t.Context(), "s", tracing.WithKind(tracing.KindServer),
		tracing.WithAttributes(slog.String("k", "v")))

	// Nothing to observe — just prove none of these panic on the id-only span.
	span.SetName("renamed")
	span.SetAttributes(slog.Int("n", 1))
	span.AddEvent("event", slog.Bool("ok", true))
	span.RecordError(errors.New("boom"))
	span.SetStatus(tracing.StatusError, "bad")
	span.End()
	span.End()
	assert.True(t, span.Context().IsValid())
}

func TestNewUniqueIDs(t *testing.T) {
	tr := tracing.New()
	seen := make(map[tracing.SpanID]bool)
	_, root := tr.Start(t.Context(), "root")
	traceID := root.Context().TraceID
	for range 100 {
		_, s := tr.Start(t.Context(), "s")
		assert.NotEqual(t, traceID, s.Context().TraceID)
		require.False(t, seen[s.Context().SpanID])
		seen[s.Context().SpanID] = true
	}
}

func TestNoopStartReturnsContextUnchanged(t *testing.T) {
	tr := tracing.NewNoop()
	ctx, span := tr.Start(t.Context(), "ignored")
	defer span.End()

	assert.Equal(t, t.Context(), ctx)
	assert.False(t, span.Context().IsValid())
	assert.False(t, span.IsRecording())
}

func TestNoopPassesRemoteThrough(t *testing.T) {
	remote, err := tracing.ParseTraceparent(sampleTraceparent)
	require.NoError(t, err)

	tr := tracing.NewNoop()
	ctx := tracing.ContextWithRemote(t.Context(), remote)
	ctx, span := tr.Start(ctx, "ignored")
	defer span.End()

	// A disabled service still logs and forwards the trace it received.
	assert.Equal(t, remote, tracing.SpanContextFromContext(ctx))
}
