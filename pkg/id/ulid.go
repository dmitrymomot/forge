// Package id provides sortable ID generation utilities.
package id

import (
	"crypto/rand"
	"encoding/binary"
	"time"
)

// Crockford's Base32 alphabet (excludes I, L, O, U to avoid confusion).
const crockfordBase32 = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// NewULID generates a ULID (Universally Unique Lexicographically Sortable Identifier).
// Returns a 26-character string: 10 chars timestamp (48-bit ms) + 16 chars random (80-bit).
// ULIDs are lexicographically sortable by creation time.
func NewULID() string {
	// Get current time in milliseconds
	ms := uint64(time.Now().UnixMilli())

	// Generate 10 random bytes (80 bits)
	randomBytes := make([]byte, 10)
	if _, err := rand.Read(randomBytes); err != nil {
		// Fallback: use time-based entropy (degraded but functional)
		binary.BigEndian.PutUint64(randomBytes[:8], uint64(time.Now().UnixNano()))
	}

	// Build the ULID: 10 timestamp chars + 16 random chars = 26 total
	var ulid [26]byte

	// Encode timestamp (48 bits = 10 base32 chars)
	ulid[0] = crockfordBase32[(ms>>45)&0x1F]
	ulid[1] = crockfordBase32[(ms>>40)&0x1F]
	ulid[2] = crockfordBase32[(ms>>35)&0x1F]
	ulid[3] = crockfordBase32[(ms>>30)&0x1F]
	ulid[4] = crockfordBase32[(ms>>25)&0x1F]
	ulid[5] = crockfordBase32[(ms>>20)&0x1F]
	ulid[6] = crockfordBase32[(ms>>15)&0x1F]
	ulid[7] = crockfordBase32[(ms>>10)&0x1F]
	ulid[8] = crockfordBase32[(ms>>5)&0x1F]
	ulid[9] = crockfordBase32[ms&0x1F]

	// Encode random bytes (80 bits = 16 base32 chars)
	ulid[10] = crockfordBase32[(randomBytes[0]>>3)&0x1F]
	ulid[11] = crockfordBase32[((randomBytes[0]&0x07)<<2)|((randomBytes[1]>>6)&0x03)]
	ulid[12] = crockfordBase32[(randomBytes[1]>>1)&0x1F]
	ulid[13] = crockfordBase32[((randomBytes[1]&0x01)<<4)|((randomBytes[2]>>4)&0x0F)]
	ulid[14] = crockfordBase32[((randomBytes[2]&0x0F)<<1)|((randomBytes[3]>>7)&0x01)]
	ulid[15] = crockfordBase32[(randomBytes[3]>>2)&0x1F]
	ulid[16] = crockfordBase32[((randomBytes[3]&0x03)<<3)|((randomBytes[4]>>5)&0x07)]
	ulid[17] = crockfordBase32[randomBytes[4]&0x1F]
	ulid[18] = crockfordBase32[(randomBytes[5]>>3)&0x1F]
	ulid[19] = crockfordBase32[((randomBytes[5]&0x07)<<2)|((randomBytes[6]>>6)&0x03)]
	ulid[20] = crockfordBase32[(randomBytes[6]>>1)&0x1F]
	ulid[21] = crockfordBase32[((randomBytes[6]&0x01)<<4)|((randomBytes[7]>>4)&0x0F)]
	ulid[22] = crockfordBase32[((randomBytes[7]&0x0F)<<1)|((randomBytes[8]>>7)&0x01)]
	ulid[23] = crockfordBase32[(randomBytes[8]>>2)&0x1F]
	ulid[24] = crockfordBase32[((randomBytes[8]&0x03)<<3)|((randomBytes[9]>>5)&0x07)]
	ulid[25] = crockfordBase32[randomBytes[9]&0x1F]

	return string(ulid[:])
}
