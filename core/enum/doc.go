// Package enum provides Values[T ~string], a fixed closed set of values over a
// named string type, declared once via New. It offers Valid, Parse, and Values
// without per-enum boilerplate. Unlike set.Set (a mutable collection), enum is
// an immutable declared value-domain.
//
// # Usage
//
//	type Status string
//
//	const (
//		StatusActive Status = "active"
//		StatusPaused Status = "paused"
//	)
//
//	var Statuses = enum.New(StatusActive, StatusPaused)
//
//	func Example() {
//		Statuses.Valid(StatusActive) // true
//
//		v, err := Statuses.Parse("paused")
//		// v == StatusPaused, err == nil
//
//		Statuses.Values() // []Status{StatusActive, StatusPaused}
//	}
package enum
