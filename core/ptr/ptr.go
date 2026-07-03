package ptr

// From dereferences p, returning the zero value of T when p is nil.
func From[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// FromOr dereferences p, returning def when p is nil.
func FromOr[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

// Equal reports whether a and b point to equal values. Two nil pointers are
// equal; a nil and a non-nil pointer are not.
func Equal[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
