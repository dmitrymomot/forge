// Package id is the framework's exclusive source of unique identifiers.
//
// It provides three sortable ID schemes as value types, each carrying a 48-bit
// big-endian millisecond timestamp prefix so that byte order equals time order:
//
//   - UUID:  128-bit RFC 9562 version-7 UUID, canonical 36-char hex, native
//     Postgres uuid column.
//   - ULID:  128-bit, 26-char Crockford base32, the standard sortable ID.
//   - Short: 80-bit, 16-char Crockford base32, compact and URL-safe (link
//     shorteners and similar).
//
// The zero-argument free functions cover the common case:
//
//	u := id.NewUUID()
//	l := id.NewULID()
//	s := id.NewShort()
//
// For deterministic tests or strictly-increasing same-millisecond ordering, use
// a Generator:
//
//	g := id.NewGenerator(id.WithClock(clk), id.WithMonotonic())
//	a, b := g.Short(), g.Short() // strictly increasing
//
// Under heavy concurrent generation, a single shared Generator created with
// WithMonotonic can outperform the free functions: the free functions call
// crypto/rand on every call (which serializes internally), while monotonic mode
// draws randomness once per millisecond and increments in between.
//
// All generation reads crypto/rand and panics only if the OS RNG fails, an
// unrecoverable condition. Parsing and Scan are case-insensitive; the Parse
// functions and Scan return ErrMalformed (via errors.Is) on invalid input.
package id
