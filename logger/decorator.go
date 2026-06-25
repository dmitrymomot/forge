package logger

import (
	"context"
	"log/slog"
)

// ContextExtractor extracts a slog attribute from context. Return ok=false to skip.
// Callers supply funcs; the package owns the internal handler that applies them.
type ContextExtractor func(ctx context.Context) (slog.Attr, bool)

// handlerOp records a WithAttrs or WithGroup call so the chain can be rebuilt with
// context-extracted attributes injected at the root level. Exactly one field is set.
type handlerOp struct {
	group string
	attrs []slog.Attr
}

// contextHandler wraps a slog.Handler and injects context-extracted attributes at the
// record's top level on every Handle call, ahead of any group opened with WithGroup.
// All methods are pure (they return new handlers and copy slices), so it is safe for
// concurrent use like any slog.Handler.
type contextHandler struct {
	root       slog.Handler // handler before any recorded WithAttrs/WithGroup ops
	next       slog.Handler // root with all ops applied (fast path)
	ops        []handlerOp
	extractors []ContextExtractor
}

// newContextHandler wraps next with the given extractors, filtering nil entries.
func newContextHandler(next slog.Handler, extractors ...ContextExtractor) *contextHandler {
	clean := make([]ContextExtractor, 0, len(extractors))
	for _, ex := range extractors {
		if ex != nil {
			clean = append(clean, ex)
		}
	}
	return &contextHandler{root: next, next: next, extractors: clean}
}

func (h *contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *contextHandler) Handle(ctx context.Context, rec slog.Record) error {
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
	// Fast path: no group active, so adding to the record keeps extracted attrs at the
	// top level without rebuilding the chain.
	if !h.hasGroup() {
		rec.AddAttrs(extracted...)
		return h.next.Handle(ctx, rec)
	}
	// A group is active: attach extracted attrs to the root handler (top level), then
	// replay the recorded ops on top before handling.
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

func (h *contextHandler) hasGroup() bool {
	for _, op := range h.ops {
		if op.group != "" {
			return true
		}
	}
	return false
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{
		root:       h.root,
		next:       h.next.WithAttrs(attrs),
		ops:        h.appendOp(handlerOp{attrs: attrs}),
		extractors: h.extractors,
	}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &contextHandler{
		root:       h.root,
		next:       h.next.WithGroup(name),
		ops:        h.appendOp(handlerOp{group: name}),
		extractors: h.extractors,
	}
}

// appendOp returns a new ops slice with op appended, without mutating the receiver.
func (h *contextHandler) appendOp(op handlerOp) []handlerOp {
	ops := make([]handlerOp, len(h.ops), len(h.ops)+1)
	copy(ops, h.ops)
	return append(ops, op)
}
