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
// The zero-argument free functions (NewUUID, NewULID, NewShort) cover the
// common case. For deterministic tests or strictly-increasing same-millisecond
// ordering, construct a Generator instead: WithClock injects a test clock, and
// WithMonotonic makes a single shared Generator strictly increasing — and,
// under heavy concurrent generation, faster than the free functions, since it
// draws randomness once per millisecond and increments it instead of calling
// crypto/rand (which serializes internally) on every call.
//
// All generation reads crypto/rand and panics only if the OS RNG fails, an
// unrecoverable condition. Parsing and Scan are case-insensitive; the Parse
// functions and Scan return ErrMalformed (via errors.Is) on invalid input.
//
// # Usage
//
//	u := id.NewUUID()
//	l := id.NewULID()
//	s := id.NewShort()
//	g := id.NewGenerator(id.WithMonotonic())
//	first, second := g.Short(), g.Short() // strictly increasing
package id
