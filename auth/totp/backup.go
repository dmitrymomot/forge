package totp

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/dmitrymomot/forge/core/random"
	"github.com/dmitrymomot/forge/crypto/consttime"
)

// backupAlphabet is lowercase alphanumeric minus the ambiguous 0/1/i/l/o —
// backup codes are printed and retyped by hand.
const backupAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// GenerateBackupCodes mints count one-time recovery codes of length random
// characters each. It returns the display forms (dash-grouped every 5, e.g.
// "abcde-fghij") to show the user exactly once, and the SHA-256 hashes of
// the normalized forms to persist. A plain hash suffices: the codes are
// high-entropy random strings, not human-chosen passwords — hence the
// length floor of 8 (~40 bits over this alphabet).
func GenerateBackupCodes(count, length int) (codes []string, hashes [][]byte, err error) {
	if count < 1 {
		return nil, nil, fmt.Errorf("totp: backup code count must be >= 1, got %d", count)
	}
	if length < 8 {
		return nil, nil, fmt.Errorf("totp: backup code length must be >= 8, got %d", length)
	}
	codes = make([]string, count)
	hashes = make([][]byte, count)
	for i := range count {
		raw := random.String(length, backupAlphabet)
		codes[i] = formatBackupCode(raw)
		hashes[i] = hashBackupCode(raw)
	}
	return codes, hashes, nil
}

// VerifyBackupCode reports whether code matches one of hashes, comparing
// against every entry in constant time (no early exit), and returns the
// matched index so the caller can consume exactly that hash. Input is
// normalized first, so case and dash/space grouping don't matter.
func VerifyBackupCode(code string, hashes [][]byte) (idx int, ok bool) {
	sum := hashBackupCode(normalizeBackupCode(code))
	idx = -1
	for i, h := range hashes {
		if consttime.BytesEqual(sum, h) && !ok {
			idx, ok = i, true
		}
	}
	return idx, ok
}

// formatBackupCode inserts a dash every 5 characters for readability.
func formatBackupCode(raw string) string {
	var b strings.Builder
	b.Grow(len(raw) + len(raw)/5)
	for i := 0; i < len(raw); i += 5 {
		if i > 0 {
			b.WriteByte('-')
		}
		end := min(i+5, len(raw))
		b.WriteString(raw[i:end])
	}
	return b.String()
}

// normalizeBackupCode lowercases and strips dashes and spaces so user
// typing quirks never fail a valid code. Hashes are always computed over
// the normalized form.
func normalizeBackupCode(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "-", "")
	return strings.ReplaceAll(s, " ", "")
}

func hashBackupCode(normalized string) []byte {
	sum := sha256.Sum256([]byte(normalized))
	return sum[:]
}
