package invite

import (
	"crypto/sha256"
	"encoding/hex"
	"hash/crc32"
	"strings"

	"github.com/dmitrymomot/forge/core/random"
)

// Invite tokens follow the apikey discipline: <prefix>_<payload><checksum>
// with a fixed "inv" prefix, a 43-char base62 payload (~256 bits of
// entropy), and a CRC32 checksum so garbage is rejected allocation-free
// before any store access.
const (
	tokenPrefix = "inv"
	payloadLen  = 43 // base62 chars ≈ 256 bits of entropy
	checksumLen = 6  // fixed-width base62 CRC32 (62^6 > 2^32)
	tokenLen    = len(tokenPrefix) + 1 + payloadLen + checksumLen
)

// base62 is the checksum alphabet. The payload draws from the same 62
// characters via random.String's default Alphanumeric set.
const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// newToken mints a plaintext invite token: inv_<payload><checksum>.
func newToken() string {
	payload := random.String(payloadLen)
	var b strings.Builder
	b.Grow(tokenLen)
	b.WriteString(tokenPrefix)
	b.WriteByte('_')
	b.WriteString(payload)
	b.WriteString(encodeChecksum(crc32String(payload)))
	return b.String()
}

// crc32String computes the IEEE CRC32 of s without allocating. The
// []byte(s) conversion crc32.ChecksumIEEE requires escapes to the heap
// through the hardware-accelerated path, so validToken — the DoS-relevant
// reject surface — computes the checksum over the string bytes directly
// via the standard table-driven update. The result is identical to
// crc32.ChecksumIEEE (same IEEE polynomial).
func crc32String(s string) uint32 {
	crc := ^uint32(0)
	for i := range len(s) {
		crc = crc32.IEEETable[byte(crc)^s[i]] ^ (crc >> 8)
	}
	return ^crc
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

// validToken reports whether token is structurally valid: exact length,
// "inv_" prefix, and payload CRC32 matching the checksum suffix. The
// checksum is computed over the string bytes directly (crc32String) and
// compared in place (no encodeChecksum string) to keep this reject path
// allocation-free — it shields the store from token-stuffing garbage.
func validToken(token string) bool {
	if len(token) != tokenLen {
		return false
	}
	if token[:len(tokenPrefix)] != tokenPrefix || token[len(tokenPrefix)] != '_' {
		return false
	}
	payload := token[len(tokenPrefix)+1 : tokenLen-checksumLen]
	sum := crc32String(payload)
	suffix := token[tokenLen-checksumLen:]
	for i := checksumLen - 1; i >= 0; i-- {
		if suffix[i] != base62[sum%62] {
			return false
		}
		sum /= 62
	}
	return true
}

// hashToken returns the hex SHA-256 of the full plaintext token — the
// stored lookup hash. Unsalted is safe: the payload carries ~256 bits of
// entropy, so preimage search is infeasible.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
