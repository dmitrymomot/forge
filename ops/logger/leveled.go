package logger

import (
	"context"
	"log/slog"
)

// leveledHandler gates a wrapped handler behind its own minimum level, independent of the
// primary destination's level. slog.MultiHandler consults each child's Enabled per record,
// so gating in Enabled is sufficient; Handle trusts that contract.
type leveledHandler struct {
	next slog.Handler
	min  slog.Level
}

func (h *leveledHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.min && h.next.Enabled(ctx, level)
}

func (h *leveledHandler) Handle(ctx context.Context, rec slog.Record) error {
	return h.next.Handle(ctx, rec)
}

func (h *leveledHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &leveledHandler{next: h.next.WithAttrs(attrs), min: h.min}
}

func (h *leveledHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &leveledHandler{next: h.next.WithGroup(name), min: h.min}
}
