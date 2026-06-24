package id

import (
	"crypto/rand"
	"encoding/binary"
	"io"
	"time"
)

// shortIDEpoch is the custom epoch (2024-01-01T00:00:00Z, in Unix seconds) used
// by ShortID timestamps. ShortID dedicates only 30 bits to the timestamp, which
// cannot hold the full Unix-millisecond value the way ULID's 48-bit field can.
// Counting whole seconds from a recent epoch keeps the value within 30 bits while
// covering ~34 years (2^30 seconds ≈ 34.04 years, i.e. until ~2058) and — crucially —
// it is never masked, so the encoded timestamp increases monotonically across the
// whole documented range instead of wrapping every ~12.43 days like a masked
// millisecond value would.
const shortIDEpoch = 1704067200

// shortIDMaxTimestamp is the largest value that fits in the 30-bit timestamp field.
const shortIDMaxTimestamp = (1 << 30) - 1

// randReader is the source of randomness for ShortID generation. It is a
// package-level seam (defaulting to crypto/rand.Reader) so tests can exercise the
// rand-failure fallback path without changing the public API.
var randReader io.Reader = rand.Reader

// shortIDTimestamp converts a Unix-seconds value into the 30-bit ShortID timestamp
// field, measured from shortIDEpoch. Values before the epoch or beyond the 30-bit
// range are clamped to [0, shortIDMaxTimestamp] so the encoded timestamp can never
// wrap and lexicographic ordering is preserved across the entire documented range.
func shortIDTimestamp(unixSeconds int64) uint64 {
	secs := unixSeconds - shortIDEpoch
	switch {
	case secs < 0:
		return 0
	case secs > shortIDMaxTimestamp:
		return shortIDMaxTimestamp
	default:
		return uint64(secs)
	}
}

// NewShortID generates a shorter sortable ID.
// Returns a 16-character string: 6 chars timestamp + 10 chars random.
// URL-safe and lexicographically sortable by creation time.
//
// The timestamp is the number of whole seconds since 2024-01-01T00:00:00Z encoded
// in 30 bits, giving a ~34-year sortable range (until ~2058) at second resolution;
// uniqueness within a single second comes from the 48 bits of random data. The
// timestamp is clamped to its valid range rather than masked, so it never wraps
// and sort order holds across the whole range. Sorting is by second: IDs created
// in the same second are not ordered relative to each other (their random suffixes
// differ but are not monotonic).
func NewShortID() string {
	return newShortID(time.Now().Unix())
}

// newShortID builds a ShortID from the given Unix-seconds timestamp. It is the
// testable core of NewShortID, kept unexported so the public signature stays
// NewShortID() string.
func newShortID(unixSeconds int64) string {
	ts := shortIDTimestamp(unixSeconds)

	// Generate 6 random bytes (for 10 base32 chars).
	randomBytes := make([]byte, 6)
	if _, err := io.ReadFull(randReader, randomBytes); err != nil {
		// Fallback: derive entropy from the high-resolution clock without
		// overflowing the 6-byte slice (degraded but functional, never panics).
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], uint64(time.Now().UnixNano()))
		copy(randomBytes, buf[:6])
	}

	// Build the ShortID: 6 timestamp chars + 10 random chars = 16 total
	var shortID [16]byte

	// Encode timestamp (30 bits = 6 base32 chars).
	shortID[0] = crockfordBase32[(ts>>25)&0x1F]
	shortID[1] = crockfordBase32[(ts>>20)&0x1F]
	shortID[2] = crockfordBase32[(ts>>15)&0x1F]
	shortID[3] = crockfordBase32[(ts>>10)&0x1F]
	shortID[4] = crockfordBase32[(ts>>5)&0x1F]
	shortID[5] = crockfordBase32[ts&0x1F]

	// Encode random bytes (48 bits packed into 10 base32 chars).
	shortID[6] = crockfordBase32[(randomBytes[0]>>3)&0x1F]
	shortID[7] = crockfordBase32[((randomBytes[0]&0x07)<<2)|((randomBytes[1]>>6)&0x03)]
	shortID[8] = crockfordBase32[(randomBytes[1]>>1)&0x1F]
	shortID[9] = crockfordBase32[((randomBytes[1]&0x01)<<4)|((randomBytes[2]>>4)&0x0F)]
	shortID[10] = crockfordBase32[((randomBytes[2]&0x0F)<<1)|((randomBytes[3]>>7)&0x01)]
	shortID[11] = crockfordBase32[(randomBytes[3]>>2)&0x1F]
	shortID[12] = crockfordBase32[((randomBytes[3]&0x03)<<3)|((randomBytes[4]>>5)&0x07)]
	shortID[13] = crockfordBase32[randomBytes[4]&0x1F]
	shortID[14] = crockfordBase32[(randomBytes[5]>>3)&0x1F]
	shortID[15] = crockfordBase32[(randomBytes[5]&0x07)<<2]

	return string(shortID[:])
}
