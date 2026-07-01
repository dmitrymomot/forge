package set

import (
	"iter"
	"slices"
)

// Set is a generic set of comparable values backed by a map. The zero Set is
// usable (Add lazily allocates). Because it wraps a map, copying a non-empty
// Set shares the backing store — pass a *Set, or build an independent copy with
// s.Union(set.New[T]()), when you need isolation.
type Set[T comparable] struct {
	m map[T]struct{}
}

// New returns a set containing the given items.
func New[T comparable](items ...T) Set[T] {
	s := Set[T]{m: make(map[T]struct{}, len(items))}
	for _, it := range items {
		s.m[it] = struct{}{}
	}
	return s
}

// Add inserts items, lazily allocating the backing map if needed.
func (s *Set[T]) Add(items ...T) {
	if s.m == nil {
		s.m = make(map[T]struct{}, len(items))
	}
	for _, it := range items {
		s.m[it] = struct{}{}
	}
}

// Remove deletes items that are present.
func (s *Set[T]) Remove(items ...T) {
	for _, it := range items {
		delete(s.m, it)
	}
}

// Contains reports whether v is in the set.
func (s Set[T]) Contains(v T) bool {
	_, ok := s.m[v]
	return ok
}

// Len returns the number of elements.
func (s Set[T]) Len() int { return len(s.m) }

// IsEmpty reports whether the set has no elements.
func (s Set[T]) IsEmpty() bool { return len(s.m) == 0 }

// Union returns a new set with all elements of s and other.
func (s Set[T]) Union(other Set[T]) Set[T] {
	out := New[T]()
	for k := range s.m {
		out.m[k] = struct{}{}
	}
	for k := range other.m {
		out.m[k] = struct{}{}
	}
	return out
}

// Intersect returns a new set with elements present in both s and other.
func (s Set[T]) Intersect(other Set[T]) Set[T] {
	out := New[T]()
	for k := range s.m {
		if _, ok := other.m[k]; ok {
			out.m[k] = struct{}{}
		}
	}
	return out
}

// Diff returns a new set with elements in s that are not in other.
func (s Set[T]) Diff(other Set[T]) Set[T] {
	out := New[T]()
	for k := range s.m {
		if _, ok := other.m[k]; !ok {
			out.m[k] = struct{}{}
		}
	}
	return out
}

// Equal reports whether s and other contain exactly the same elements.
func (s Set[T]) Equal(other Set[T]) bool {
	if len(s.m) != len(other.m) {
		return false
	}
	for k := range s.m {
		if _, ok := other.m[k]; !ok {
			return false
		}
	}
	return true
}

// Slice returns the elements in unspecified order.
func (s Set[T]) Slice() []T {
	out := make([]T, 0, len(s.m))
	for k := range s.m {
		out = append(out, k)
	}
	return out
}

// Sorted returns the elements sorted by less.
func (s Set[T]) Sorted(less func(a, b T) bool) []T {
	out := s.Slice()
	slices.SortFunc(out, func(a, b T) int {
		switch {
		case less(a, b):
			return -1
		case less(b, a):
			return 1
		default:
			return 0
		}
	})
	return out
}

// All iterates the elements in unspecified order.
func (s Set[T]) All() iter.Seq[T] {
	return func(yield func(T) bool) {
		for k := range s.m {
			if !yield(k) {
				return
			}
		}
	}
}
