package tracing

import (
	"context"
	"log/slog"
)

// SpanKind describes a span's role in a trace, mirroring the OpenTelemetry
// vocabulary so drivers map it one-to-one.
type SpanKind uint8

const (
	// KindInternal is an operation internal to the service (the default).
	KindInternal SpanKind = iota
	// KindServer is the handling of an inbound request.
	KindServer
	// KindClient is an outbound synchronous call.
	KindClient
	// KindProducer is the enqueue side of an async hand-off.
	KindProducer
	// KindConsumer is the processing side of an async hand-off.
	KindConsumer
)

// Status is a span's outcome. StatusUnset (the default) means the backend
// derives the outcome itself; only mark StatusError for genuine failures.
type Status uint8

const (
	StatusUnset Status = iota
	StatusOK
	StatusError
)

// Tracer starts spans. Implementations are safe for concurrent use. Packages
// that emit spans take a Tracer option defaulting to NewNoop; swapping the
// implementation (New, tracing/otel) changes wiring, never call sites.
type Tracer interface {
	// Start begins a span named name as a child of the span in ctx — or of a
	// remote parent stored by ContextWithRemote when no live span is present —
	// and returns a context carrying the new span. The caller must call End on
	// the returned span.
	Start(ctx context.Context, name string, opts ...StartOption) (context.Context, Span)
}

// Span is a live span. Mutators are no-ops after End and on non-recording
// spans; guard genuinely expensive attribute computation with IsRecording.
// RecordError only attaches the error as an event — a span that failed should
// also SetStatus(StatusError, ...).
type Span interface {
	// Context returns the span's propagatable identity.
	Context() SpanContext
	// IsRecording reports whether attributes and events are being captured.
	IsRecording() bool
	// SetName renames the span (e.g. once the route pattern is known).
	SetName(name string)
	// SetAttributes adds attributes. Scalar slog kinds map directly; groups
	// flatten to dotted keys; anything else is stringified.
	SetAttributes(attrs ...slog.Attr)
	// AddEvent adds a timestamped event.
	AddEvent(name string, attrs ...slog.Attr)
	// RecordError attaches err as an exception event. A nil err is ignored.
	RecordError(err error)
	// SetStatus sets the span outcome; description is only meaningful for
	// StatusError.
	SetStatus(code Status, description string)
	// End completes the span. Only the first call has an effect.
	End()
}

// StartConfig is the resolved set of Start options. It is exported so Tracer
// implementations outside this package can apply StartOptions via
// NewStartConfig; consumers never construct it directly.
type StartConfig struct {
	Attrs   []slog.Attr
	Kind    SpanKind
	NewRoot bool
}

// StartOption configures Start.
type StartOption func(*StartConfig)

// NewStartConfig applies opts (nil entries are ignored) and returns the
// resolved config. Tracer implementations call this first in Start.
func NewStartConfig(opts ...StartOption) StartConfig {
	var c StartConfig
	for _, o := range opts {
		if o != nil {
			o(&c)
		}
	}
	return c
}

// WithKind sets the span kind (default KindInternal).
func WithKind(k SpanKind) StartOption {
	return func(c *StartConfig) { c.Kind = k }
}

// WithAttributes adds attributes present from the span's start (visible to
// samplers, unlike SetAttributes).
func WithAttributes(attrs ...slog.Attr) StartOption {
	return func(c *StartConfig) { c.Attrs = append(c.Attrs, attrs...) }
}

// WithNewRoot starts a new trace even when ctx carries a parent — e.g. a
// background job triggered by, but not part of, a request.
func WithNewRoot() StartOption {
	return func(c *StartConfig) { c.NewRoot = true }
}
