package hashx_test

import (
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/hashx"
)

// SHA-256("abc") — FIPS 180-2 example digest.
const sha256abcHex = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"

func TestSHA256_KnownAnswer(t *testing.T) {
	assert.Equal(t, sha256abcHex, hashx.SHA256Hex([]byte("abc")))
	assert.Len(t, hashx.SHA256([]byte("abc")), 32)
}

func TestSHA256Base64(t *testing.T) {
	// Derive the expected value from the known digest rather than hardcoding a
	// base64 literal (avoids transcription errors).
	digest, err := hex.DecodeString(sha256abcHex)
	require.NoError(t, err)
	want := base64.RawStdEncoding.EncodeToString(digest)
	assert.Equal(t, want, hashx.SHA256Base64([]byte("abc")))
}

func TestSHA512(t *testing.T) {
	assert.Len(t, hashx.SHA512([]byte("abc")), 64)
	assert.Len(t, hashx.SHA512Hex([]byte("abc")), 128)
}

func TestHMACSHA256_KnownAnswer(t *testing.T) {
	// RFC 4231 test case 2
	const want = "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843"
	got := hashx.HMACSHA256Hex([]byte("Jefe"), []byte("what do ya want for nothing?"))
	assert.Equal(t, want, got)
	assert.Len(t, hashx.HMACSHA256([]byte("k"), []byte("m")), 32)
}

func TestFileSHA256(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	require.NoError(t, os.WriteFile(p, []byte("abc"), 0o600))
	got, err := hashx.FileSHA256(p)
	require.NoError(t, err)
	assert.Equal(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", got)
}

func TestFileSHA256_Missing(t *testing.T) {
	_, err := hashx.FileSHA256(filepath.Join(t.TempDir(), "nope"))
	require.Error(t, err)
}
