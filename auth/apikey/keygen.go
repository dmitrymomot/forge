package apikey

import (
	"crypto/sha256"
	"encoding/hex"
	"hash/crc32"
	"strings"

	"github.com/dmitrymomot/forge/core/random"
)

const (
	payloadLen  = 43 // base62 chars ≈ 256 bits of entropy
	checksumLen = 6  // fixed-width base62 CRC32 (62^6 > 2^32)
	previewLen  = 12
)

// base62 is the checksum alphabet. The payload draws from the same 62
// characters via random.String's default Alphanumeric set.
const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// newKey mints a plaintext key: <prefix>_<payload><checksum>.
func newKey(prefix string) string {
	payload := random.String(payloadLen)
	var b strings.Builder
	b.Grow(len(prefix) + 1 + payloadLen + checksumLen)
	b.WriteString(prefix)
	b.WriteByte('_')
	b.WriteString(payload)
	b.WriteString(encodeChecksum(crc32.ChecksumIEEE([]byte(payload))))
	return b.String()
}

// encodeChecksum renders v as fixed-width base62, most significant first.
func encodeChecksum(v uint32) string {
	var buf [checksumLen]byte
	for i := checksumLen - 1; i >= 0; i-- {
		buf[i] = base62[v%62]
		v /= 62
	}
	return string(buf[:])
}

// validKey reports whether credential is structurally valid for prefix:
// exact length, prefix match, and payload CRC32 matching the checksum
// suffix. CRC32 detects any burst error up to 32 bits, so every
// single-character corruption is caught. The checksum is compared in
// place (no encodeChecksum string) to keep this reject path
// allocation-free — it is the DoS-relevant surface that shields the
// store from credential-stuffing garbage.
func validKey(prefix, credential string) bool {
	wantLen := len(prefix) + 1 + payloadLen + checksumLen
	if len(credential) != wantLen {
		return false
	}
	if credential[:len(prefix)] != prefix || credential[len(prefix)] != '_' {
		return false
	}
	payload := credential[len(prefix)+1 : wantLen-checksumLen]
	sum := crc32.ChecksumIEEE([]byte(payload))
	suffix := credential[wantLen-checksumLen:]
	for i := checksumLen - 1; i >= 0; i-- {
		if suffix[i] != base62[sum%62] {
			return false
		}
		sum /= 62
	}
	return true
}

// hashKey returns the hex SHA-256 of the full plaintext key — the stored
// lookup hash. Unsalted is safe: the payload carries ~256 bits of entropy,
// so preimage search is infeasible.
func hashKey(credential string) string {
	sum := sha256.Sum256([]byte(credential))
	return hex.EncodeToString(sum[:])
}

// validPrefix reports whether p is non-empty [a-z0-9_]+.
func validPrefix(p string) bool {
	if p == "" {
		return false
	}
	for i := range len(p) {
		c := p[i]
		if c != '_' && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}
