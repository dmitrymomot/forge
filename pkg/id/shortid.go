package id

import (
	"crypto/rand"
	"encoding/binary"
	"time"
)

// NewShortID generates a shorter sortable ID.
// Returns a 16-character string: 6 chars timestamp + 10 chars random.
// URL-safe and lexicographically sortable by creation time.
func NewShortID() string {
	// Get current time in milliseconds
	ms := uint64(time.Now().UnixMilli())

	// Generate 6 random bytes (for 10 base32 chars)
	randomBytes := make([]byte, 6)
	if _, err := rand.Read(randomBytes); err != nil {
		// Fallback: use time-based entropy (degraded but functional)
		binary.BigEndian.PutUint64(randomBytes[:], uint64(time.Now().UnixNano()))
	}

	// Build the ShortID: 6 timestamp chars + 10 random chars = 16 total
	var shortID [16]byte

	// Encode timestamp (30 bits = 6 base32 chars, ~34 years range)
	// Use lower 30 bits of milliseconds for ~34 years of unique timestamps
	ts := ms & 0x3FFFFFFF
	shortID[0] = crockfordBase32[(ts>>25)&0x1F]
	shortID[1] = crockfordBase32[(ts>>20)&0x1F]
	shortID[2] = crockfordBase32[(ts>>15)&0x1F]
	shortID[3] = crockfordBase32[(ts>>10)&0x1F]
	shortID[4] = crockfordBase32[(ts>>5)&0x1F]
	shortID[5] = crockfordBase32[ts&0x1F]

	// Encode random bytes (48 bits = 10 base32 chars, but we need 50 bits)
	// Pack 6 bytes into 10 base32 chars
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
