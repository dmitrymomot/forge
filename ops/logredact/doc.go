// Package logredact is an slog.Handler middleware that redacts attribute
// values before they reach a downstream handler.
//
// Attributes are matched either by leaf key (WithKeys), which matches at any
// nesting depth, or by dotted group path (WithPaths), which matches only the
// exact group prefix. Matching values are replaced with DefaultReplacement
// ("[REDACTED]") or a custom string set via WithReplacement.
//
// Redaction is unconditional: there is no level knob, and it applies equally
// to attributes passed to a log call, attributes baked in via
// slog.Logger.With (which routes through Handler.WithAttrs), and attributes
// nested inside slog.Group values. The group prefix is tracked across
// slog.Logger.WithGroup so dotted-path matches resolve correctly regardless
// of nesting depth.
package logredact
