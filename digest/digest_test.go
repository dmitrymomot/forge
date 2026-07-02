package digest_test

import (
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/digest"
)

// SHA-256("abc") — FIPS 180-2 example digest.
const sha256abcHex = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"

func TestSHA256_KnownAnswer(t *testing.T) {
	assert.Equal(t, sha256abcHex, digest.SHA256Hex([]byte("abc")))
	assert.Len(t, digest.SHA256([]byte("abc")), 32)
}

func TestSHA256Base64(t *testing.T) {
	// Derive the expected value from the known digest rather than hardcoding a
	// base64 literal (avoids transcription errors).
	raw, err := hex.DecodeString(sha256abcHex)
	require.NoError(t, err)
	want := base64.RawStdEncoding.EncodeToString(raw)
	assert.Equal(t, want, digest.SHA256Base64([]byte("abc")))
}

// SHA-512("abc") — FIPS 180-4 known-answer vector.
const sha512abcHex = "ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a2192992a274fc1a836ba3c23a3feebbd454d4423643ce80e2a9ac94fa54ca49f"

func TestSHA512(t *testing.T) {
	assert.Len(t, digest.SHA512([]byte("abc")), 64)
	assert.Len(t, digest.SHA512Hex([]byte("abc")), 128)
	assert.Equal(t, sha512abcHex, digest.SHA512Hex([]byte("abc")))
}

func TestSHA512Base64(t *testing.T) {
	// Derive the expected value from the known digest rather than hardcoding a
	// base64 literal (avoids transcription errors).
	raw, err := hex.DecodeString(sha512abcHex)
	require.NoError(t, err)
	want := base64.RawStdEncoding.EncodeToString(raw)
	assert.Equal(t, want, digest.SHA512Base64([]byte("abc")))
}

func TestHMACSHA256_KnownAnswer(t *testing.T) {
	// RFC 4231 test case 2
	const want = "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843"
	got := digest.HMACSHA256Hex([]byte("Jefe"), []byte("what do ya want for nothing?"))
	assert.Equal(t, want, got)
	assert.Len(t, digest.HMACSHA256([]byte("k"), []byte("m")), 32)
}

func TestFileSHA256(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	require.NoError(t, os.WriteFile(p, []byte("abc"), 0o600))
	got, err := digest.FileSHA256(p)
	require.NoError(t, err)
	assert.Equal(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", got)
}

func TestFileSHA256_Missing(t *testing.T) {
	_, err := digest.FileSHA256(filepath.Join(t.TempDir(), "nope"))
	require.Error(t, err)
	assert.ErrorContains(t, err, "digest: open ")
}
