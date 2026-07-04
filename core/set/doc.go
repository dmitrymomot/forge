// Package set provides a generic Set[T comparable] with membership, the set
// algebra stdlib lacks (Union/Intersect/Diff/Equal), and deterministic
// iteration via Sorted. The zero Set is usable; copying a non-empty Set shares
// its backing map (documented on the type).
//
// # Usage
//
//	a := set.New(1, 2, 3)
//	b := set.New(2, 3, 4)
//
//	a.Add(5)
//	a.Contains(5) // true
//
//	union := a.Union(b)          // {1, 2, 3, 4, 5}
//	inter := a.Intersect(b)      // {2, 3}
//	diff := a.Diff(b)            // {1, 5}
//	union.Sorted(func(x, y int) bool { return x < y }) // [1 2 3 4 5]
package set
