package secret_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/crypto/secret"
)

func key32(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }

func TestEncryptDecrypt_GCM(t *testing.T) {
	box, err := secret.New(key32(1))
	require.NoError(t, err)

	ct, err := box.Encrypt([]byte("4111 1111 1111 1111"))
	require.NoError(t, err)
	assert.NotContains(t, string(ct), "4111") // ciphertext hides the plaintext

	pt, err := box.Decrypt(ct)
	require.NoError(t, err)
	assert.Equal(t, "4111 1111 1111 1111", string(pt))
}

func TestEncryptDecryptString(t *testing.T) {
	box, err := secret.New(key32(7))
	require.NoError(t, err)
	enc, err := box.EncryptString("hello")
	require.NoError(t, err)
	dec, err := box.DecryptString(enc)
	require.NoError(t, err)
	assert.Equal(t, "hello", dec)
}

func TestNew_BadKeySize(t *testing.T) {
	_, err := secret.New([]byte("too-short"))
	require.ErrorIs(t, err, secret.ErrInvalidKeySize)
}

func TestDecrypt_Tampered(t *testing.T) {
	box, err := secret.New(key32(2))
	require.NoError(t, err)
	ct, err := box.Encrypt([]byte("data"))
	require.NoError(t, err)
	ct[len(ct)-1] ^= 0xff // flip a tag byte
	_, err = box.Decrypt(ct)
	require.ErrorIs(t, err, secret.ErrDecryptFailed)
}

func TestDecryptString_BadBase64(t *testing.T) {
	box, err := secret.New(key32(3))
	require.NoError(t, err)
	_, err = box.DecryptString("not*base64")
	require.ErrorIs(t, err, secret.ErrDecryptFailed)
}

func TestChaCha_RoundTrip(t *testing.T) {
	box, err := secret.New(key32(4), secret.WithChaCha())
	require.NoError(t, err)
	ct, err := box.Encrypt([]byte("xchacha"))
	require.NoError(t, err)
	pt, err := box.Decrypt(ct)
	require.NoError(t, err)
	assert.Equal(t, "xchacha", string(pt))
}

func TestAAD_MustMatch(t *testing.T) {
	enc, err := secret.New(key32(5), secret.WithAAD([]byte("ctx-A")))
	require.NoError(t, err)
	ct, err := enc.Encrypt([]byte("secret"))
	require.NoError(t, err)

	dec, err := secret.New(key32(5), secret.WithAAD([]byte("ctx-B")))
	require.NoError(t, err)
	_, err = dec.Decrypt(ct)
	require.ErrorIs(t, err, secret.ErrDecryptFailed) // different AAD fails authentication
}

func TestRotation_ViaKeyset(t *testing.T) {
	ksOld, err := keyset.New(keyset.WithPrimary(1, key32(1)))
	require.NoError(t, err)
	boxOld, err := secret.FromKeyset(ksOld)
	require.NoError(t, err)
	ct, err := boxOld.Encrypt([]byte("legacy"))
	require.NoError(t, err)

	ksNew, err := keyset.New(
		keyset.WithPrimary(2, key32(2)),
		keyset.WithRetired(1, key32(1)),
	)
	require.NoError(t, err)
	boxNew, err := secret.FromKeyset(ksNew)
	require.NoError(t, err)

	pt, err := boxNew.Decrypt(ct) // old ciphertext still decrypts under retired key
	require.NoError(t, err)
	assert.Equal(t, "legacy", string(pt))

	ct2, err := boxNew.Encrypt([]byte("fresh")) // new writes use primary v2
	require.NoError(t, err)
	pt2, err := boxNew.Decrypt(ct2)
	require.NoError(t, err)
	assert.Equal(t, "fresh", string(pt2))
}
