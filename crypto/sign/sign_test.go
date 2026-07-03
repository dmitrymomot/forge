package sign_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/crypto/sign"
)

func TestSignVerify_RawKey(t *testing.T) {
	s, err := sign.New([]byte("0123456789abcdef"))
	require.NoError(t, err)

	mac := s.Sign([]byte("hello"))
	assert.NotEmpty(t, mac)
	assert.True(t, s.Verify([]byte("hello"), mac))
	assert.False(t, s.Verify([]byte("hellp"), mac)) // tampered message
	assert.False(t, s.Verify([]byte("hello"), append(mac, 0x00)))
}

func TestNew_EmptyKey(t *testing.T) {
	_, err := sign.New(nil)
	require.ErrorIs(t, err, sign.ErrInvalidKey)
}

func TestSignVerifyString(t *testing.T) {
	s, err := sign.New([]byte("0123456789abcdef"))
	require.NoError(t, err)

	signed := s.SignString("payload")
	assert.Contains(t, signed, ".") // "version.mac"
	assert.True(t, s.VerifyString("payload", signed))
	assert.False(t, s.VerifyString("payload2", signed))
	assert.False(t, s.VerifyString("payload", "garbage"))
	assert.False(t, s.VerifyString("payload", "0.bad$$base64"))
}

func TestVerifyString_Rotation(t *testing.T) {
	// Sign under a keyset whose primary is version 1.
	ksOld, err := keyset.New(keyset.WithPrimary(1, []byte("key-v1-secret-bytes")))
	require.NoError(t, err)
	signerOld, err := sign.FromKeyset(ksOld)
	require.NoError(t, err)
	signed := signerOld.SignString("invite-payload")

	// Rotate: new primary v2, v1 retired. The new signer still verifies v1 material.
	ksNew, err := keyset.New(
		keyset.WithPrimary(2, []byte("key-v2-secret-bytes")),
		keyset.WithRetired(1, []byte("key-v1-secret-bytes")),
	)
	require.NoError(t, err)
	signerNew, err := sign.FromKeyset(ksNew)
	require.NoError(t, err)

	assert.True(t, signerNew.VerifyString("invite-payload", signed))
	assert.False(t, signerNew.VerifyString("tampered", signed))
}

func TestFromKeyset_Nil(t *testing.T) {
	_, err := sign.FromKeyset(nil)
	require.ErrorIs(t, err, sign.ErrInvalidKey)
}

func TestNew_NilHash(t *testing.T) {
	_, err := sign.New([]byte("0123456789abcdef"), sign.WithHash(nil))
	require.ErrorIs(t, err, sign.ErrInvalidKey)
}

func TestVerifyString_SingleKeyVersionMismatch(t *testing.T) {
	s, err := sign.New([]byte("0123456789abcdef"))
	require.NoError(t, err)
	signed := s.SignString("payload") // "0.<base64url-mac>"
	_, mac, ok := strings.Cut(signed, ".")
	require.True(t, ok)
	// Same MAC bytes but a different version prefix: a single-key signer must reject it.
	assert.False(t, s.VerifyString("payload", "1."+mac))
}
