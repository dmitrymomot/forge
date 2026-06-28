package subtlex

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
)

// BytesEqual reports whether a and b are equal in constant time, without leaking their
// length via an early return. Each side is reduced to a fixed-length HMAC-SHA256 digest
// under a fresh per-call key (the "double HMAC" pattern), then compared with
// crypto/subtle. Comparing values of different lengths therefore reveals nothing through
// timing.
func BytesEqual(a, b []byte) bool {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		// Unreachable in practice; fall back to keyless digests, still constant-time.
		ha := sha256.Sum256(a)
		hb := sha256.Sum256(b)
		return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
	}
	return subtle.ConstantTimeCompare(mac(key, a), mac(key, b)) == 1
}

// StringEqual is BytesEqual for strings.
func StringEqual(a, b string) bool { return BytesEqual([]byte(a), []byte(b)) }

func mac(key, msg []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(msg)
	return m.Sum(nil)
}
