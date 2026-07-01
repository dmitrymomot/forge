package enum

import (
	"errors"
	"fmt"
)

// ErrInvalidValue is returned by Parse for a value outside the declared set.
var ErrInvalidValue = errors.New("enum: invalid value")

// Values is an immutable, declared-once closed set of values over a named
// string type. It is distinct from set.Set (a mutable runtime collection):
// Values is a fixed value-domain.
type Values[T ~string] struct {
	set     map[T]struct{}
	ordered []T
}

// New declares a value set. Duplicate values are ignored, preserving first
// declaration order.
func New[T ~string](vals ...T) Values[T] {
	e := Values[T]{
		ordered: make([]T, 0, len(vals)),
		set:     make(map[T]struct{}, len(vals)),
	}
	for _, v := range vals {
		if _, ok := e.set[v]; ok {
			continue
		}
		e.set[v] = struct{}{}
		e.ordered = append(e.ordered, v)
	}
	return e
}

// Valid reports whether v is a declared value.
func (e Values[T]) Valid(v T) bool {
	_, ok := e.set[v]
	return ok
}

// Parse converts s to T when it is a declared value, else returns
// ErrInvalidValue.
func (e Values[T]) Parse(s string) (T, error) {
	v := T(s)
	if _, ok := e.set[v]; ok {
		return v, nil
	}
	var zero T
	return zero, fmt.Errorf("enum: invalid value %q: %w", s, ErrInvalidValue)
}

// Values returns a copy of the declared values in declaration order.
func (e Values[T]) Values() []T {
	out := make([]T, len(e.ordered))
	copy(out, e.ordered)
	return out
}
