// Package id provides sortable ID generation utilities for use throughout the framework.
//
// This package is the exclusive source of ID generation in Forge. All identifiers
// must be created using the functions provided here — no direct uuid.New(), ulid.Make(),
// or custom ID generation is permitted.
//
// # Overview
//
// The package provides two ID formats, both using Crockford's Base32 encoding
// for URL-safe, case-insensitive, lexicographically sortable identifiers:
//
//   - ULID: 26-character full-precision identifier (48-bit timestamp + 80-bit random)
//   - ShortID: 16-character compact identifier (30-bit timestamp + 48-bit random)
//
// # ULID
//
// NewULID generates a Universally Unique Lexicographically Sortable Identifier.
// ULIDs are 26 characters long and contain a millisecond-precision timestamp
// followed by cryptographically random bytes. They sort chronologically by
// creation time.
//
//	userID := id.NewULID()    // e.g., "01HZXK5M3N0000ABCDEFGH1234"
//	orderID := id.NewULID()   // e.g., "01HZXK5M3N0000IJKLMNOP5678"
//
// Use ULIDs as the default for database primary keys, external identifiers,
// and any case where global uniqueness matters.
//
// # ShortID
//
// NewShortID generates a shorter sortable identifier. ShortIDs are 16 characters
// long and use a reduced timestamp range (~34 years) with less random entropy.
// They remain lexicographically sortable by creation time.
//
//	ref := id.NewShortID()    // e.g., "0K5M3NABCDEF1234"
//	code := id.NewShortID()   // e.g., "0K5M3NIJKLMN5678"
//
// Use ShortIDs for user-facing codes, short references, or anywhere a compact
// identifier is preferred and the reduced uniqueness guarantees are acceptable.
package id
