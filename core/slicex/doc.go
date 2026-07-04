// Package slicex provides generic slice helpers that the standard library
// slices package does not: Map, Filter, Reduce, GroupBy, KeyBy, Unique,
// Flatten, and a materialized Chunk.
//
// slicex is a gap-filler, not a superset. It deliberately does NOT re-implement
// or re-export functions that already live in stdlib slices (Sort, SortFunc,
// Contains, Index, Equal, Reverse, Compact, ...). Import "slices" directly
// alongside slicex. Aliasing stdlib is avoided on purpose: generic functions
// cannot be cheaply aliased (var Sort = slices.Sort is illegal; each alias
// would be a hand-written generic wrapper), such wrappers drift as stdlib grows
// new helpers, and two names for one function is a two-sources-of-truth footgun.
//
// # Usage
//
//	doubled := slicex.Map([]int{1, 2, 3}, func(v int) int { return v * 2 })
//	// doubled == []int{2, 4, 6}
//
//	evens := slicex.Filter(doubled, func(v int) bool { return v%4 == 0 })
//	// evens == []int{4}
//
//	groups := slicex.GroupBy([]int{1, 2, 3, 4}, func(v int) int { return v % 2 })
//	// groups == map[int][]int{0: {2, 4}, 1: {1, 3}}
package slicex
