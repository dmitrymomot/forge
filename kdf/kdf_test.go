package kdf_test

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/kdf"
)

func TestHKDF_RFC5869Case1(t *testing.T) {
	ikm := make([]byte, 22)
	for i := range ikm {
		ikm[i] = 0x0b
	}
	salt := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	info := []byte{0xf0, 0xf1, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7, 0xf8, 0xf9}
	const want = "3cb25f25faacd57a90434f64d0362f2a" +
		"2d2d0a90cf1a5a4c5db02d56ecc4c5bf" +
		"34007208d5b887185865"

	got, err := kdf.HKDF(ikm, salt, info, 42)
	require.NoError(t, err)
	assert.Equal(t, want, hex.EncodeToString(got))
}

func TestHKDF_DomainSeparation(t *testing.T) {
	master := []byte("master-secret-material")
	salt := []byte("app-v1")
	a, err := kdf.HKDF(master, salt, []byte("cookie"), 32)
	require.NoError(t, err)
	b, err := kdf.HKDF(master, salt, []byte("token"), 32)
	require.NoError(t, err)
	assert.Len(t, a, 32)
	assert.NotEqual(t, a, b) // different info → unrelated keys
}

func TestDeriveKey_Deterministic(t *testing.T) {
	p := kdf.Params{Time: 1, Memory: 8 * 1024, Threads: 1, KeyLen: 32}
	salt := []byte("0123456789abcdef")
	a, err := kdf.DeriveKey([]byte("passphrase"), salt, p)
	require.NoError(t, err)
	b, err := kdf.DeriveKey([]byte("passphrase"), salt, p)
	require.NoError(t, err)
	assert.Equal(t, a, b)
	assert.Len(t, a, 32)

	c, err := kdf.DeriveKey([]byte("passphrase"), []byte("different-salt16"), p)
	require.NoError(t, err)
	assert.NotEqual(t, a, c)
}

func TestParams_Validate(t *testing.T) {
	require.NoError(t, kdf.DefaultParams().Validate())
	require.ErrorIs(t, (kdf.Params{}).Validate(), kdf.ErrInvalidParams)
	require.ErrorIs(t, kdf.Params{Time: 1, Memory: 0, Threads: 1, KeyLen: 32}.Validate(), kdf.ErrInvalidParams)
}

func TestDeriveKey_RejectsBadParams(t *testing.T) {
	_, err := kdf.DeriveKey([]byte("p"), []byte("s"), kdf.Params{})
	require.ErrorIs(t, err, kdf.ErrInvalidParams)
}
