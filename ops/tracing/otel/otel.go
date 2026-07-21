// Package otel adapts the OpenTelemetry trace API to the tracing.Tracer seam.
// Sampling, resources, and exporters remain OTel SDK concerns wired in the
// consumer's main; this driver only translates the seam's vocabulary.
package otel

import (
	"context"
	"log/slog"
	"math"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/dmitrymomot/forge/ops/tracing"
)

type config struct {
	name string
}

// Option configures New.
type Option func(*config)

// WithTracerName sets the OTel instrumentation-scope name (default the import
// path of this package). An empty name is ignored.
func WithTracerName(name string) Option {
	return func(c *config) {
		if name != "" {
			c.name = name
		}
	}
}

// New returns a tracing.Tracer backed by tp — typically the OTel SDK's
// sdktrace.NewTracerProvider with the consumer's sampler and exporter. Panics
// on a nil provider.
func New(tp oteltrace.TracerProvider, opts ...Option) *Tracer {
	if tp == nil {
		panic("otel: New requires a TracerProvider")
	}
	c := config{name: "github.com/dmitrymomot/forge/ops/tracing/otel"}
	for _, o := range opts {
		o(&c)
	}
	return &Tracer{tr: tp.Tracer(c.name)}
}

// Tracer implements tracing.Tracer over an OpenTelemetry tracer. Spans it
// starts live in both the forge and the OTel context slots, so third-party
// OTel instrumentation parents onto them and vice versa.
type Tracer struct {
	tr oteltrace.Tracer
}

var _ tracing.Tracer = (*Tracer)(nil)

func (t *Tracer) Start(ctx context.Context, name string, opts ...tracing.StartOption) (context.Context, tracing.Span) {
	cfg := tracing.NewStartConfig(opts...)
	// A remote parent stored by the tracing middleware (or a queue consumer)
	// only exists in the forge context slot; seed it into the OTel slot so the
	// SDK parents and samples off it. A live OTel span in ctx always wins.
	if !oteltrace.SpanContextFromContext(ctx).IsValid() {
		if sc := tracing.SpanContextFromContext(ctx); sc.IsValid() {
			ctx = oteltrace.ContextWithRemoteSpanContext(ctx, toOtelSpanContext(sc))
		}
	}
	startOpts := make([]oteltrace.SpanStartOption, 0, 3)
	startOpts = append(startOpts, oteltrace.WithSpanKind(toOtelKind(cfg.Kind)))
	if len(cfg.Attrs) > 0 {
		startOpts = append(startOpts, oteltrace.WithAttributes(convertAttrs(cfg.Attrs)...))
	}
	if cfg.NewRoot {
		startOpts = append(startOpts, oteltrace.WithNewRoot())
	}
	ctx, s := t.tr.Start(ctx, name, startOpts...)
	sp := span{s: s}
	return tracing.ContextWithSpan(ctx, sp), sp
}

type span struct{ s oteltrace.Span }

func (sp span) Context() tracing.SpanContext {
	sc := sp.s.SpanContext()
	out := tracing.SpanContext{
		TraceID: tracing.TraceID(sc.TraceID()),
		SpanID:  tracing.SpanID(sc.SpanID()),
		Sampled: sc.IsSampled(),
	}
	if ts := sc.TraceState(); ts.Len() > 0 {
		out.TraceState = ts.String()
	}
	return out
}

func (sp span) IsRecording() bool { return sp.s.IsRecording() }

func (sp span) SetName(name string) { sp.s.SetName(name) }

func (sp span) SetAttributes(attrs ...slog.Attr) {
	sp.s.SetAttributes(convertAttrs(attrs)...)
}

func (sp span) AddEvent(name string, attrs ...slog.Attr) {
	if len(attrs) == 0 {
		sp.s.AddEvent(name)
		return
	}
	sp.s.AddEvent(name, oteltrace.WithAttributes(convertAttrs(attrs)...))
}

func (sp span) RecordError(err error) {
	if err != nil {
		sp.s.RecordError(err)
	}
}

func (sp span) SetStatus(code tracing.Status, description string) {
	switch code {
	case tracing.StatusOK:
		sp.s.SetStatus(codes.Ok, description)
	case tracing.StatusError:
		sp.s.SetStatus(codes.Error, description)
	default:
		sp.s.SetStatus(codes.Unset, description)
	}
}

func (sp span) End() { sp.s.End() }

func toOtelSpanContext(sc tracing.SpanContext) oteltrace.SpanContext {
	cfg := oteltrace.SpanContextConfig{
		TraceID: oteltrace.TraceID(sc.TraceID),
		SpanID:  oteltrace.SpanID(sc.SpanID),
		Remote:  true,
	}
	if sc.Sampled {
		cfg.TraceFlags = oteltrace.FlagsSampled
	}
	if sc.TraceState != "" {
		if ts, err := oteltrace.ParseTraceState(sc.TraceState); err == nil {
			cfg.TraceState = ts
		}
	}
	return oteltrace.NewSpanContext(cfg)
}

func toOtelKind(k tracing.SpanKind) oteltrace.SpanKind {
	switch k {
	case tracing.KindServer:
		return oteltrace.SpanKindServer
	case tracing.KindClient:
		return oteltrace.SpanKindClient
	case tracing.KindProducer:
		return oteltrace.SpanKindProducer
	case tracing.KindConsumer:
		return oteltrace.SpanKindConsumer
	default:
		return oteltrace.SpanKindInternal
	}
}

func convertAttrs(attrs []slog.Attr) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, len(attrs))
	for _, a := range attrs {
		out = appendAttr(out, "", a)
	}
	return out
}

// appendAttr maps one slog.Attr onto OTel attributes: scalar kinds map
// directly, groups flatten to dotted keys (slog's empty-key groups inline, as
// in logging), LogValuers resolve first, and anything without a native OTel
// type is stringified.
func appendAttr(dst []attribute.KeyValue, prefix string, a slog.Attr) []attribute.KeyValue {
	v := a.Value.Resolve()
	key := a.Key
	if prefix != "" {
		if key == "" {
			key = prefix
		} else {
			key = prefix + "." + key
		}
	}
	switch v.Kind() {
	case slog.KindGroup:
		for _, ga := range v.Group() {
			dst = appendAttr(dst, key, ga)
		}
		return dst
	case slog.KindString:
		return append(dst, attribute.String(key, v.String()))
	case slog.KindInt64:
		return append(dst, attribute.Int64(key, v.Int64()))
	case slog.KindUint64:
		if u := v.Uint64(); u <= math.MaxInt64 {
			return append(dst, attribute.Int64(key, int64(u)))
		}
		return append(dst, attribute.String(key, strconv.FormatUint(v.Uint64(), 10)))
	case slog.KindFloat64:
		return append(dst, attribute.Float64(key, v.Float64()))
	case slog.KindBool:
		return append(dst, attribute.Bool(key, v.Bool()))
	case slog.KindDuration:
		return append(dst, attribute.String(key, v.Duration().String()))
	case slog.KindTime:
		return append(dst, attribute.String(key, v.Time().Format(time.RFC3339Nano)))
	default:
		return append(dst, attribute.String(key, v.String()))
	}
}
