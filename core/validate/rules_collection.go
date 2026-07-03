package validate

// MinItems requires at least n elements.
func MinItems[T any](n int) Rule[[]T] {
	return func(items []T) Violation {
		if len(items) < n {
			return Violation{Key: "validation.min_items", Params: []Param{{Key: "min", Value: n}}}
		}
		return Violation{}
	}
}

// MaxItems requires at most n elements.
func MaxItems[T any](n int) Rule[[]T] {
	return func(items []T) Violation {
		if len(items) > n {
			return Violation{Key: "validation.max_items", Params: []Param{{Key: "max", Value: n}}}
		}
		return Violation{}
	}
}

// UniqueItems requires all elements to be distinct.
func UniqueItems[T comparable](items []T) Violation {
	seen := make(map[T]struct{}, len(items))
	for _, it := range items {
		if _, dup := seen[it]; dup {
			return Violation{Key: "validation.unique_items"}
		}
		seen[it] = struct{}{}
	}
	return Violation{}
}

// Is wraps a caller predicate into a Rule[T] with a caller-named key — the escape
// hatch for custom checks (any func(T) Violation literal is also a valid Rule[T]).
func Is[T any](pred func(T) bool, key string) Rule[T] {
	return func(v T) Violation {
		if !pred(v) {
			return Violation{Key: key}
		}
		return Violation{}
	}
}
