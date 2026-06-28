package redact

import (
	"encoding/json"
	"log/slog"
	"maps"
)

const placeholder = "REDACTED"

// Secret wraps a value so it renders as "REDACTED" through fmt, encoding/json, and
// log/slog, and is revealed only via Expose.
type Secret[T any] struct {
	v T
}

// New wraps v in a Secret.
func New[T any](v T) Secret[T] { return Secret[T]{v: v} }

// Expose returns the wrapped value. This is the only way to read it.
func (s Secret[T]) Expose() T { return s.v }

// String implements fmt.Stringer.
func (s Secret[T]) String() string { return placeholder }

// GoString implements fmt.GoStringer (the %#v verb).
func (s Secret[T]) GoString() string { return placeholder }

// MarshalJSON implements json.Marshaler.
func (s Secret[T]) MarshalJSON() ([]byte, error) { return json.Marshal(placeholder) }

// LogValue implements slog.LogValuer.
func (s Secret[T]) LogValue() slog.Value { return slog.StringValue(placeholder) }

// String returns a partially masked copy of s, keeping a short prefix and suffix for
// correlation (e.g. "sk_l***f8a2"). Strings of 8 characters or fewer are fully masked.
func String(s string) string {
	const keep = 4
	if len(s) <= keep*2 {
		return placeholder
	}
	return s[:keep] + "***" + s[len(s)-keep:]
}

// Map returns a shallow copy of m with the named keys replaced by "REDACTED". The
// input map is not modified; keys not present are ignored.
func Map(m map[string]any, keys ...string) map[string]any {
	out := make(map[string]any, len(m))
	maps.Copy(out, m)
	for _, k := range keys {
		if _, ok := out[k]; ok {
			out[k] = placeholder
		}
	}
	return out
}
