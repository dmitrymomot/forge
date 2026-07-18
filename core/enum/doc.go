// Package enum provides Values[T ~string], a fixed closed set of values over a
// named string type, declared once via New. It offers Valid, Parse, and Values
// without per-enum boilerplate. Unlike set.Set (a mutable collection), enum is
// an immutable declared value-domain.
//
// # Usage
//
//	type Priority string
//
//	const (
//		PriorityLow  Priority = "low"
//		PriorityHigh Priority = "high"
//	)
//
//	var Priorities = enum.New(PriorityLow, PriorityHigh)
//
//	Priorities.Valid(PriorityLow) // true
//
//	v, err := Priorities.Parse("high")
//	// v == PriorityHigh, err == nil
//
//	Priorities.Values() // []Priority{PriorityLow, PriorityHigh}
package enum
