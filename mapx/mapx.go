package mapx

import stdmaps "maps"

// Merge returns a new map containing all keys from the given maps. When a key
// appears in more than one map the value from the later map wins.
func Merge[K comparable, V any](maps ...map[K]V) map[K]V {
	out := make(map[K]V)
	for _, m := range maps {
		stdmaps.Copy(out, m)
	}
	return out
}

// MapValues returns a new map with fn applied to each value of m.
func MapValues[K comparable, V, U any](m map[K]V, fn func(V) U) map[K]U {
	out := make(map[K]U, len(m))
	for k, v := range m {
		out[k] = fn(v)
	}
	return out
}

// Invert swaps keys and values. On duplicate values the last key wins.
func Invert[K, V comparable](m map[K]V) map[V]K {
	out := make(map[V]K, len(m))
	for k, v := range m {
		out[v] = k
	}
	return out
}

// Filter returns a new map of the entries of m for which pred returns true.
func Filter[K comparable, V any](m map[K]V, pred func(K, V) bool) map[K]V {
	out := make(map[K]V)
	for k, v := range m {
		if pred(k, v) {
			out[k] = v
		}
	}
	return out
}

// Entry is a single key/value pair.
type Entry[K comparable, V any] struct {
	Key   K
	Value V
}

// Entries returns the entries of m in unspecified order.
func Entries[K comparable, V any](m map[K]V) []Entry[K, V] {
	out := make([]Entry[K, V], 0, len(m))
	for k, v := range m {
		out = append(out, Entry[K, V]{Key: k, Value: v})
	}
	return out
}

// FromEntries builds a map from a slice of entries. On duplicate keys the last
// entry wins.
func FromEntries[K comparable, V any](es []Entry[K, V]) map[K]V {
	out := make(map[K]V, len(es))
	for _, e := range es {
		out[e.Key] = e.Value
	}
	return out
}
