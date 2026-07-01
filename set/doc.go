// Package set provides a generic Set[T comparable] with membership, the set
// algebra stdlib lacks (Union/Intersect/Diff/Equal), and deterministic
// iteration via Sorted. The zero Set is usable; copying a non-empty Set shares
// its backing map (documented on the type).
package set
