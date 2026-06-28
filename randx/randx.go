package randx

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
)

// Read fills p with cryptographically secure random bytes. It is the error-returning
// escape hatch; most callers want Bytes.
func Read(p []byte) error {
	if _, err := rand.Read(p); err != nil {
		return fmt.Errorf("randx: read: %w", err)
	}
	return nil
}

// Bytes returns n cryptographically secure random bytes. It panics only if crypto/rand
// fails, which indicates a broken OS RNG — an unrecoverable condition.
func Bytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Errorf("randx: crypto/rand failed: %w", err))
	}
	return b
}

// Hex returns the hex encoding of n random bytes (2n characters).
func Hex(n int) string { return hex.EncodeToString(Bytes(n)) }

// URLSafe returns the unpadded base64url encoding of n random bytes.
func URLSafe(n int) string { return base64.RawURLEncoding.EncodeToString(Bytes(n)) }

// Int returns an unbiased random integer in [0, max). It panics if max <= 0.
func Int(max int) int {
	if max <= 0 {
		panic("randx: Int max must be > 0")
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		panic(fmt.Errorf("randx: crypto/rand failed: %w", err))
	}
	return int(v.Int64())
}
