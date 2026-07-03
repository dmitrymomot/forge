package slicex

// Map returns a new slice with fn applied to each element of s.
// A nil input yields a nil result; an empty non-nil input yields an empty
// non-nil result (nilness is preserved).
func Map[T, U any](s []T, fn func(T) U) []U {
	if s == nil {
		return nil
	}
	out := make([]U, len(s))
	for i, v := range s {
		out[i] = fn(v)
	}
	return out
}

// Filter returns a new slice of the elements of s for which pred returns true.
func Filter[T any](s []T, pred func(T) bool) []T {
	if s == nil {
		return nil
	}
	out := make([]T, 0, len(s))
	for _, v := range s {
		if pred(v) {
			out = append(out, v)
		}
	}
	return out
}

// Reduce folds s left-to-right into a single value starting from init.
func Reduce[T, U any](s []T, init U, fn func(acc U, v T) U) U {
	acc := init
	for _, v := range s {
		acc = fn(acc, v)
	}
	return acc
}

// GroupBy buckets the elements of s by key, preserving per-bucket order.
func GroupBy[T any, K comparable](s []T, key func(T) K) map[K][]T {
	out := make(map[K][]T)
	for _, v := range s {
		k := key(v)
		out[k] = append(out[k], v)
	}
	return out
}

// KeyBy indexes s by key. On duplicate keys the last element wins.
func KeyBy[T any, K comparable](s []T, key func(T) K) map[K]T {
	out := make(map[K]T, len(s))
	for _, v := range s {
		out[key(v)] = v
	}
	return out
}

// Unique returns the elements of s with duplicates removed, preserving
// first-seen order (unlike slices.Compact, which needs sorted input).
func Unique[T comparable](s []T) []T {
	if s == nil {
		return nil
	}
	seen := make(map[T]struct{}, len(s))
	out := make([]T, 0, len(s))
	for _, v := range s {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// Flatten concatenates the sub-slices of s into one slice.
func Flatten[T any](s [][]T) []T {
	if s == nil {
		return nil
	}
	n := 0
	for _, sub := range s {
		n += len(sub)
	}
	out := make([]T, 0, n)
	for _, sub := range s {
		out = append(out, sub...)
	}
	return out
}

// Chunk splits s into consecutive slices of at most n elements. The final
// chunk may be shorter. Chunk panics if n < 1, matching slices.Chunk. Unlike
// slices.Chunk it returns a materialized [][]T rather than an iterator; each
// chunk has capacity clamped so appending to it cannot overwrite s.
func Chunk[T any](s []T, n int) [][]T {
	if n < 1 {
		panic("slicex: Chunk called with n < 1")
	}
	if s == nil {
		return nil
	}
	out := make([][]T, 0, (len(s)+n-1)/n)
	for i := 0; i < len(s); i += n {
		end := min(i+n, len(s))
		out = append(out, s[i:end:end])
	}
	return out
}
