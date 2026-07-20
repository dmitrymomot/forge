package tracing

import (
	"context"
	"encoding/binary"
	"log/slog"
	"math/rand/v2"
)

// New returns the zero-dependency default Tracer: propagation-only. It manages
// trace identity — continuing the inbound trace or minting a new one, with a
// fresh span id per span — but records nothing, so trace_id log correlation
// and downstream header propagation work without any exporter. New roots are
// marked sampled: this tracer makes no sampling decision of its own, and an
// unsampled flag would tell parent-based samplers downstream to drop the
// trace. Swap in tracing/otel for real span export without touching call
// sites.
func New() Tracer { return idTracer{} }

type idTracer struct{}

func (idTracer) Start(ctx context.Context, _ string, opts ...StartOption) (context.Context, Span) {
	cfg := NewStartConfig(opts...)
	var sc SpanContext
	if parent := SpanContextFromContext(ctx); !cfg.NewRoot && parent.TraceID.IsValid() {
		sc.TraceID = parent.TraceID
		sc.Sampled = parent.Sampled
		sc.TraceState = parent.TraceState
	} else {
		sc.TraceID = newTraceID()
		sc.Sampled = true
	}
	sc.SpanID = newSpanID()
	s := idSpan{sc: sc}
	return ContextWithSpan(ctx, s), s
}

// idSpan carries identity only; every mutator is a no-op.
type idSpan struct{ sc SpanContext }

func (s idSpan) Context() SpanContext        { return s.sc }
func (idSpan) IsRecording() bool             { return false }
func (idSpan) SetName(string)                {}
func (idSpan) SetAttributes(...slog.Attr)    {}
func (idSpan) AddEvent(string, ...slog.Attr) {}
func (idSpan) RecordError(error)             {}
func (idSpan) SetStatus(Status, string)      {}
func (idSpan) End()                          {}

// NewNoop returns a Tracer that does nothing at all: Start returns ctx
// unchanged and a span with no identity. Because ctx is untouched, a remote
// parent stored by the middleware stays visible to LogExtractor and
// propagation — a service with tracing disabled still logs and forwards the
// trace it received. Use as the safe default when tracing is an optional
// dependency.
func NewNoop() Tracer { return noopTracer{} }

type noopTracer struct{}

func (noopTracer) Start(ctx context.Context, _ string, _ ...StartOption) (context.Context, Span) {
	return ctx, noopSpan{}
}

// Trace ids need uniqueness, not secrecy: math/rand/v2's global generator
// (ChaCha8, crypto-seeded, per-P states) avoids a crypto/rand read per span.
// The loops re-draw the astronomically unlikely all-zero value, which the W3C
// spec declares invalid.

func newTraceID() TraceID {
	var id TraceID
	for id == (TraceID{}) {
		binary.BigEndian.PutUint64(id[:8], rand.Uint64())
		binary.BigEndian.PutUint64(id[8:], rand.Uint64())
	}
	return id
}

func newSpanID() SpanID {
	var id SpanID
	for id == (SpanID{}) {
		binary.BigEndian.PutUint64(id[:], rand.Uint64())
	}
	return id
}
