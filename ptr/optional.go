package ptr

import "encoding/json"

// Optional is a two-state value: either defined (carrying a T) or not. It is
// the "was this field provided?" signal for JSON PATCH bodies: UnmarshalJSON is
// only invoked by encoding/json when the key is present, so an absent key
// leaves the Optional undefined while a present key (even null) marks it
// defined.
//
// The three-way absent / null / value distinction is obtained by composition
// rather than a third state: Optional[*T] gives absent (!IsDefined), explicit
// null (defined, inner *T nil), and value.
type Optional[T any] struct {
	value   T
	defined bool
}

// Some returns a defined Optional carrying v.
func Some[T any](v T) Optional[T] { return Optional[T]{value: v, defined: true} }

// None returns an undefined Optional.
func None[T any]() Optional[T] { return Optional[T]{} }

// Get returns the value and whether the Optional is defined.
func (o Optional[T]) Get() (T, bool) { return o.value, o.defined }

// IsDefined reports whether a value is present.
func (o Optional[T]) IsDefined() bool { return o.defined }

// OrElse returns the value when defined, otherwise def.
func (o Optional[T]) OrElse(def T) T {
	if o.defined {
		return o.value
	}
	return def
}

// IsZero reports whether the Optional is undefined. It enables the
// encoding/json ",omitzero" tag to omit an undefined Optional from output.
func (o Optional[T]) IsZero() bool { return !o.defined }

// MarshalJSON emits the encoded value when defined, otherwise null.
func (o Optional[T]) MarshalJSON() ([]byte, error) {
	if !o.defined {
		return []byte("null"), nil
	}
	return json.Marshal(o.value)
}

// UnmarshalJSON marks the Optional defined (the key was present) and decodes
// the value. A JSON null leaves the value as the zero value of T (for pointer T
// that is a nil pointer, i.e. an explicit clear).
func (o *Optional[T]) UnmarshalJSON(b []byte) error {
	o.defined = true
	if string(b) == "null" {
		var zero T
		o.value = zero
		return nil
	}
	return json.Unmarshal(b, &o.value)
}
