package totp_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/totp"
)

func TestGenerateBackupCodes(t *testing.T) {
	t.Parallel()
	codes, hashes, err := totp.GenerateBackupCodes(10, 10)
	require.NoError(t, err)
	require.Len(t, codes, 10)
	require.Len(t, hashes, 10)

	seen := map[string]bool{}
	for _, c := range codes {
		assert.Equal(t, "xxxxx-xxxxx", strings.Map(func(r rune) rune {
			if r == '-' {
				return '-'
			}
			return 'x'
		}, c), "display format grouped by 5: %q", c)
		assert.False(t, seen[c], "duplicate code %q", c)
		seen[c] = true
		for _, r := range strings.ReplaceAll(c, "-", "") {
			assert.NotContains(t, "01ilo", string(r), "ambiguous char in %q", c)
		}
	}
	for _, h := range hashes {
		assert.Len(t, h, 32, "SHA-256 hash")
	}
}

func TestGenerateBackupCodes_Validation(t *testing.T) {
	t.Parallel()
	_, _, err := totp.GenerateBackupCodes(0, 10)
	assert.Error(t, err)
	_, _, err = totp.GenerateBackupCodes(10, 7)
	assert.Error(t, err, "length floor is 8 — the high-entropy claim needs it")
}

func TestVerifyBackupCode(t *testing.T) {
	t.Parallel()
	codes, hashes, err := totp.GenerateBackupCodes(5, 10)
	require.NoError(t, err)

	idx, ok := totp.VerifyBackupCode(codes[3], hashes)
	require.True(t, ok)
	assert.Equal(t, 3, idx)

	_, ok = totp.VerifyBackupCode("aaaaa-aaaaa", hashes)
	assert.False(t, ok)
	_, ok = totp.VerifyBackupCode("", hashes)
	assert.False(t, ok)
	_, ok = totp.VerifyBackupCode(codes[0], nil)
	assert.False(t, ok)
}

func TestVerifyBackupCode_NormalizationEquivalence(t *testing.T) {
	t.Parallel()
	codes, hashes, err := totp.GenerateBackupCodes(1, 10)
	require.NoError(t, err)
	variants := []string{
		strings.ToUpper(codes[0]),              // ABCDE-FGHIJ
		strings.ReplaceAll(codes[0], "-", ""),  // abcdefghij
		strings.ReplaceAll(codes[0], "-", " "), // abcde fghij
		" " + strings.ToUpper(strings.ReplaceAll(codes[0], "-", "")) + " ",
	}
	for _, v := range variants {
		idx, ok := totp.VerifyBackupCode(v, hashes)
		assert.True(t, ok, "variant %q must verify", v)
		assert.Equal(t, 0, idx)
	}
}
