package null

import (
	"bytes"
	"database/sql"
	"encoding/json"
)

// Null is a nullable value of type T. It embeds sql.Null[T] for Scan/Value and
// adds JSON marshaling as null when not valid.
type Null[T any] struct {
	sql.Null[T]
}

// Of returns a valid Null carrying v.
func Of[T any](v T) Null[T] {
	return Null[T]{sql.Null[T]{V: v, Valid: true}}
}

// Empty returns an invalid (null) Null.
func Empty[T any]() Null[T] {
	return Null[T]{}
}

// Get returns the value and whether it is valid (non-null).
func (n Null[T]) Get() (T, bool) {
	return n.V, n.Valid
}

// Ptr returns a pointer to a copy of the value, or nil when not valid.
func (n Null[T]) Ptr() *T {
	if !n.Valid {
		return nil
	}
	v := n.V
	return &v
}

// FromPtr returns Empty when p is nil, otherwise Of(*p).
func FromPtr[T any](p *T) Null[T] {
	if p == nil {
		return Empty[T]()
	}
	return Of(*p)
}

// MarshalJSON encodes the value, or JSON null when not valid.
func (n Null[T]) MarshalJSON() ([]byte, error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(n.V)
}

// UnmarshalJSON decodes JSON null as an invalid Null; any other value sets the
// value and marks it valid.
func (n *Null[T]) UnmarshalJSON(b []byte) error {
	if string(bytes.TrimSpace(b)) == "null" {
		var zero T
		n.V, n.Valid = zero, false
		return nil
	}
	if err := json.Unmarshal(b, &n.V); err != nil {
		return err
	}
	n.Valid = true
	return nil
}
