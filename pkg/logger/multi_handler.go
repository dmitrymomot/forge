package logger

import (
	"context"
	"log/slog"
)

// multiHandler forwards log records to multiple handlers.
type multiHandler struct {
	handlers []slog.Handler
}

// newMultiHandler creates a handler that writes to all provided handlers.
func newMultiHandler(handlers ...slog.Handler) slog.Handler {
	return &multiHandler{handlers: handlers}
}

func (h *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *multiHandler) Handle(ctx context.Context, rec slog.Record) error {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, rec.Level) {
			if err := handler.Handle(ctx, rec.Clone()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithAttrs(attrs)
	}
	return newMultiHandler(handlers...)
}

func (h *multiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithGroup(name)
	}
	return newMultiHandler(handlers...)
}
