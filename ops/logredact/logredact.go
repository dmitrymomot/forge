package logredact

import (
	"context"
	"log/slog"
)

// DefaultReplacement is substituted for a redacted attribute value.
const DefaultReplacement = "[REDACTED]"

type config struct {
	keys        map[string]struct{}
	paths       map[string]struct{}
	replacement string
}

// Option configures New.
type Option func(*config)

// WithKeys redacts any attribute whose leaf key matches, at any nesting depth.
func WithKeys(keys ...string) Option {
	return func(c *config) {
		for _, k := range keys {
			c.keys[k] = struct{}{}
		}
	}
}

// WithPaths redacts an attribute by its dotted group path, e.g. "user.ssn"
// matches key "ssn" inside group "user" but not a top-level "ssn". Paths are
// resolved relative to the root of this handler — any slog.WithGroup already
// applied to the handler that New wraps is not part of the matched path.
func WithPaths(paths ...string) Option {
	return func(c *config) {
		for _, p := range paths {
			c.paths[p] = struct{}{}
		}
	}
}

// WithReplacement overrides the "[REDACTED]" placeholder.
func WithReplacement(s string) Option { return func(c *config) { c.replacement = s } }

// handler wraps next, redacting matching attribute values before they reach it.
type handler struct {
	next  slog.Handler
	cfg   *config
	group string // dotted prefix of currently-open groups; "" at root
}

// New wraps next so attribute values matching WithKeys / WithPaths are replaced
// before reaching next. It redacts record attrs (Handle), attrs baked in via
// WithAttrs, and nested group values, tracking the group prefix across
// WithGroup so dotted paths resolve at any depth.
func New(next slog.Handler, opts ...Option) slog.Handler {
	c := &config{
		keys:        make(map[string]struct{}),
		paths:       make(map[string]struct{}),
		replacement: DefaultReplacement,
	}
	for _, o := range opts {
		o(c)
	}
	return &handler{next: next, cfg: c}
}

func (h *handler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(h.redact(h.group, a))
		return true
	})
	return h.next.Handle(ctx, out)
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	red := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		red[i] = h.redact(h.group, a) // attrs are baked now — redact eagerly
	}
	return &handler{next: h.next.WithAttrs(red), cfg: h.cfg, group: h.group}
}

func (h *handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &handler{next: h.next.WithGroup(name), cfg: h.cfg, group: joinPath(h.group, name)}
}

// redact replaces a's value when it matches by key or dotted path; group-valued
// attrs are recursed into. LogValuer values are resolved first so their shape
// is concrete for matching.
func (h *handler) redact(prefix string, a slog.Attr) slog.Attr {
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		sub := a.Value.Group()
		gp := joinPath(prefix, a.Key)
		red := make([]slog.Attr, len(sub))
		for i, s := range sub {
			red[i] = h.redact(gp, s)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(red...)}
	}
	if h.matches(prefix, a.Key) {
		return slog.String(a.Key, h.cfg.replacement)
	}
	return a
}

func (h *handler) matches(prefix, key string) bool {
	if _, ok := h.cfg.keys[key]; ok {
		return true
	}
	if len(h.cfg.paths) > 0 {
		if _, ok := h.cfg.paths[joinPath(prefix, key)]; ok {
			return true
		}
	}
	return false
}

func joinPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}
