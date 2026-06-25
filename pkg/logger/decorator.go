package logger

import (
	"context"
	"log/slog"
)

// ContextExtractor extracts a slog attribute from context.
type ContextExtractor func(ctx context.Context) (slog.Attr, bool)

// handlerOp records a WithAttrs or WithGroup call so the handler chain can be rebuilt with
// context-extracted attributes injected at the root level (see LogHandlerDecorator.Handle).
// Exactly one of attrs/group is set per op.
type handlerOp struct {
	group string
	attrs []slog.Attr
}

// LogHandlerDecorator wraps a slog.Handler and injects context-extracted attributes during logging.
// Extraction occurs per-log-call to capture fresh request-scoped values (e.g., request IDs).
//
// Extracted attributes are placed at the root level of the log record, not nested inside
// groups opened via WithGroup. This keeps request-scoped metadata (request IDs, user IDs)
// addressable at a stable top-level key regardless of any grouping a caller has applied.
type LogHandlerDecorator struct {
	// root is the underlying handler before any WithAttrs/WithGroup calls were applied.
	// Extracted attributes are attached here so they land at the top level, ahead of any group.
	root slog.Handler
	// next is root with all recorded ops applied; it handles records when no extractors are
	// configured (or none match), avoiding a per-call rebuild.
	next slog.Handler
	// ops records WithAttrs/WithGroup calls in order so the chain can be replayed on top of the
	// root handler after extracted attributes are injected.
	ops        []handlerOp
	extractors []ContextExtractor
}

// NewLogHandlerDecorator creates a new decorated handler with context extractors.
// Filters nil extractors to prevent runtime panics from misconfigured options.
func NewLogHandlerDecorator(next slog.Handler, extractors ...ContextExtractor) slog.Handler {
	clean := make([]ContextExtractor, 0, len(extractors))
	for _, ex := range extractors {
		if ex != nil {
			clean = append(clean, ex)
		}
	}
	return &LogHandlerDecorator{root: next, next: next, extractors: clean}
}

func (h *LogHandlerDecorator) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle extracts context attributes and delegates to the underlying handler.
//
// When groups are active, extracted attributes are attached to the root (ungrouped) handler
// and the recorded WithAttrs/WithGroup operations are re-applied on top, so the attributes
// appear at the top level instead of being nested inside the most recently opened group.
func (h *LogHandlerDecorator) Handle(ctx context.Context, rec slog.Record) error {
	if len(h.extractors) == 0 {
		return h.next.Handle(ctx, rec)
	}

	extracted := make([]slog.Attr, 0, len(h.extractors))
	for _, ex := range h.extractors {
		if attr, ok := ex(ctx); ok {
			extracted = append(extracted, attr)
		}
	}
	if len(extracted) == 0 {
		return h.next.Handle(ctx, rec)
	}

	// Fast path: no group is active, so adding to the record keeps the extracted attributes
	// at the top level and avoids rebuilding the handler chain on every call.
	if !h.hasGroup() {
		rec.AddAttrs(extracted...)
		return h.next.Handle(ctx, rec)
	}

	// A group is active: attach extracted attrs to the root handler first so they land at the
	// top level, then replay the recorded ops (static attrs + groups) on top before handling.
	target := h.root.WithAttrs(extracted)
	for _, op := range h.ops {
		if op.group != "" {
			target = target.WithGroup(op.group)
		} else {
			target = target.WithAttrs(op.attrs)
		}
	}
	return target.Handle(ctx, rec)
}

// hasGroup reports whether any WithGroup call has been recorded.
func (h *LogHandlerDecorator) hasGroup() bool {
	for _, op := range h.ops {
		if op.group != "" {
			return true
		}
	}
	return false
}

// WithAttrs creates a new decorated handler with additional static attributes.
func (h *LogHandlerDecorator) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &LogHandlerDecorator{
		root:       h.root,
		next:       h.next.WithAttrs(attrs),
		ops:        h.appendOp(handlerOp{attrs: attrs}),
		extractors: h.extractors,
	}
}

// WithGroup creates a new decorated handler with attribute grouping.
//
// The group name is recorded so that context-extracted attributes can still be injected
// at the root level (see Handle) rather than being nested inside the group.
func (h *LogHandlerDecorator) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &LogHandlerDecorator{
		root:       h.root,
		next:       h.next.WithGroup(name),
		ops:        h.appendOp(handlerOp{group: name}),
		extractors: h.extractors,
	}
}

// appendOp returns a new ops slice with op appended, without mutating the receiver's slice.
func (h *LogHandlerDecorator) appendOp(op handlerOp) []handlerOp {
	ops := make([]handlerOp, len(h.ops), len(h.ops)+1)
	copy(ops, h.ops)
	return append(ops, op)
}
