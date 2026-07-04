// Package encoding provides compact, URL-safe, human-typable codecs: base62 for
// integers and byte slices, and Crockford base32 (excludes I/L/O/U) for
// sortable IDs and short codes. The Crockford codec uses the ULID-canonical
// MSB-first, left-padded layout so a 16-byte value encodes to 26 characters and
// a 10-byte value to 16.
//
// # Usage
//
//	s := encoding.EncodeInt(1234567890)
//	n, err := encoding.DecodeInt(s)
//
//	s = encoding.Encode([]byte{0x00, 0xff})
//	b, err := encoding.Decode(s)
//
//	s = encoding.Encode32([]byte{0xff})
//	b, err = encoding.Decode32(s)
package encoding
