package random

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
)

// Read fills p with cryptographically secure random bytes. It is the error-returning
// escape hatch; most callers want Bytes.
func Read(p []byte) error {
	if _, err := rand.Read(p); err != nil {
		return fmt.Errorf("random: read: %w", err)
	}
	return nil
}

// Bytes returns n cryptographically secure random bytes. It panics only if crypto/rand
// fails, which indicates a broken OS RNG — an unrecoverable condition.
func Bytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Errorf("random: crypto/rand failed: %w", err))
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
		panic("random: Int max must be > 0")
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		panic(fmt.Errorf("random: crypto/rand failed: %w", err))
	}
	return int(v.Int64())
}

// Charset constants for String. Each is an ASCII byte string.
const (
	Lowercase    = "abcdefghijklmnopqrstuvwxyz"
	Uppercase    = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	Digits       = "0123456789"
	Alphabetic   = Lowercase + Uppercase
	Alphanumeric = Alphabetic + Digits
	Symbols      = "~!@#$%^&*()-_+={}[]|\\;:\"<>,./?`"
)

// String returns a cryptographically secure random string of n characters drawn
// uniformly (bias-free, via crypto/rand rejection sampling) from the concatenation
// of charsets. With no charsets it defaults to Alphanumeric. Overlapping charsets
// are de-duplicated (first occurrence wins) so the distribution stays uniform over
// the distinct characters. The charset is treated as bytes; multi-byte UTF-8
// alphabets are not supported. It panics if n < 0 or the combined charset is empty.
func String(n int, charsets ...string) string {
	if n < 0 {
		panic("random: String n must be >= 0")
	}
	set := dedupeCharset(charsets)
	k := len(set)
	if k == 0 {
		panic("random: String charset is empty")
	}
	if n == 0 {
		return ""
	}
	out := make([]byte, n)
	// limit is the largest multiple of k within a byte; bytes at or above it are
	// rejected to remove modulo bias. De-duping over bytes guarantees k <= 256.
	limit := 256 - (256 % k)
	buf := make([]byte, n)
	bi := len(buf) // force an initial fill
	for i := 0; i < n; {
		if bi >= len(buf) {
			if _, err := rand.Read(buf); err != nil {
				panic(fmt.Errorf("random: crypto/rand failed: %w", err))
			}
			bi = 0
		}
		b := int(buf[bi])
		bi++
		if b < limit {
			out[i] = set[b%k]
			i++
		}
	}
	return string(out)
}

// DigitCode returns an n-digit decimal string with leading zeros preserved,
// suitable for OTP and email verification codes. It panics if n <= 0.
func DigitCode(n int) string {
	if n <= 0 {
		panic("random: DigitCode n must be > 0")
	}
	return String(n, Digits)
}

// dedupeCharset joins charsets (defaulting to Alphanumeric) and removes duplicate
// bytes, preserving first-occurrence order, so String stays uniform over distinct
// characters.
func dedupeCharset(charsets []string) []byte {
	joined := Alphanumeric
	if len(charsets) > 0 {
		joined = strings.Join(charsets, "")
	}
	var seen [256]bool
	out := make([]byte, 0, len(joined))
	for i := range len(joined) {
		c := joined[i]
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}
