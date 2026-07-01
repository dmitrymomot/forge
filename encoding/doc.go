// Package encoding provides compact, URL-safe, human-typable codecs: base62 for
// integers and byte slices, and Crockford base32 (excludes I/L/O/U) for
// sortable IDs and short codes. The Crockford codec uses the ULID-canonical
// MSB-first, left-padded layout so a 16-byte value encodes to 26 characters and
// a 10-byte value to 16.
package encoding
