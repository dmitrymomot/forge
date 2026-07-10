package logsample

import (
	"context"
	"log/slog"
	"sync/atomic"
)

// handler samples records below cfg.minLevel and passes the rest to next.
type handler struct {
	next  slog.Handler
	count *atomic.Uint64
	cfg   config
}

// New wraps next so records below the threshold level are sampled "keep 1 of N"
// while records at or above it always pass. Handlers derived via WithAttrs /
// WithGroup share one atomic counter, so a logger and its With children sample
// the same logical stream as one.
func New(next slog.Handler, opts ...Option) slog.Handler {
	c := config{rate: 10, minLevel: slog.LevelWarn}
	for _, o := range opts {
		o(&c)
	}
	return &handler{next: next, count: new(atomic.Uint64), cfg: c}
}

func (h *handler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= h.cfg.minLevel {
		return h.next.Handle(ctx, r)
	}
	// 1-based counter: keep the 1st, (1+N)th, (1+2N)th, ... sub-threshold record,
	// so the first occurrence of a burst is never lost and rate 1 keeps all.
	n := h.count.Add(1)
	if (n-1)%uint64(h.cfg.rate) == 0 {
		return h.next.Handle(ctx, r)
	}
	return nil
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &handler{next: h.next.WithAttrs(attrs), count: h.count, cfg: h.cfg}
}

func (h *handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &handler{next: h.next.WithGroup(name), count: h.count, cfg: h.cfg}
}
