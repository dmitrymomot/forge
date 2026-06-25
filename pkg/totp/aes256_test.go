package totp_test

import (
	"crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/dmitrymomot/forge/pkg/totp"

	"github.com/stretchr/testify/require"
)

func TestEncryptDecryptSecret(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		plainText string
		key       []byte
		wantErr   error
	}{
		{
			name:      "Valid encryption and decryption",
			plainText: "MYSECRETKEY123",
			key:       mustKey(t),
			wantErr:   nil,
		},
		{
			name:      "Empty plaintext",
			plainText: "",
			key:       mustKey(t),
			wantErr:   nil,
		},
		{
			name:      "Invalid key size",
			plainText: "MYSECRETKEY123",
			key:       make([]byte, 16),
			wantErr:   totp.ErrInvalidEncryptionKeyLength,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Encrypt
			encrypted, err := totp.EncryptSecret(tt.plainText, tt.key)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.NotEmpty(t, encrypted)

			// Decrypt
			decrypted, err := totp.DecryptSecret(encrypted, tt.key)
			require.NoError(t, err)
			require.Equal(t, tt.plainText, decrypted)
		})
	}
}

// TestEncryptSecret_NonceRandomness verifies that encrypting the same plaintext
// twice yields distinct ciphertexts (fresh random nonce per call).
func TestEncryptSecret_NonceRandomness(t *testing.T) {
	t.Parallel()
	key := mustKey(t)

	first, err := totp.EncryptSecret("MYSECRETKEY123", key)
	require.NoError(t, err)
	second, err := totp.EncryptSecret("MYSECRETKEY123", key)
	require.NoError(t, err)

	require.NotEqual(t, first, second, "identical plaintext must produce distinct ciphertexts")

	// Both must still decrypt back to the same plaintext.
	for _, ct := range []string{first, second} {
		decrypted, err := totp.DecryptSecret(ct, key)
		require.NoError(t, err)
		require.Equal(t, "MYSECRETKEY123", decrypted)
	}
}

// TestDecryptSecret_WrongKey verifies GCM authentication rejects decryption with
// the wrong key rather than returning garbage plaintext.
func TestDecryptSecret_WrongKey(t *testing.T) {
	t.Parallel()
	encryptKey := mustKey(t)
	decryptKey := mustKey(t)
	require.NotEqual(t, encryptKey, decryptKey)

	encrypted, err := totp.EncryptSecret("MYSECRETKEY123", encryptKey)
	require.NoError(t, err)

	plain, err := totp.DecryptSecret(encrypted, decryptKey)
	require.ErrorIs(t, err, totp.ErrFailedToDecryptSecret)
	require.Empty(t, plain)
}

func TestDecryptSecret_Invalid(t *testing.T) {
	t.Parallel()
	key := mustKey(t)
	tests := []struct {
		name             string
		cipherTextBase64 string
		wantErr          error
	}{
		{
			name:             "Invalid base64",
			cipherTextBase64: "invalid-base64!@#$",
			wantErr:          totp.ErrFailedToDecryptSecret,
		},
		{
			name:             "Too short ciphertext",
			cipherTextBase64: base64.StdEncoding.EncodeToString([]byte("short")),
			wantErr:          totp.ErrInvalidCipherTooShort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := totp.DecryptSecret(tt.cipherTextBase64, key)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestGenerateEncryptionKey(t *testing.T) {
	t.Parallel()
	key, err := totp.GenerateEncryptionKey()
	require.NoError(t, err)
	require.Len(t, key, 32)
}

func TestGenerateEncodedEncryptionKey(t *testing.T) {
	t.Parallel()
	key, err := totp.GenerateEncodedEncryptionKey()
	require.NoError(t, err)
	require.NotEmpty(t, key)

	decoded, err := base64.StdEncoding.DecodeString(key)
	require.NoError(t, err)
	require.Len(t, decoded, 32)
}

func TestGetEncryptionKey(t *testing.T) {
	t.Parallel()

	t.Run("valid base64 32-byte key", func(t *testing.T) {
		t.Parallel()
		encoded, err := totp.GenerateEncodedEncryptionKey()
		require.NoError(t, err)
		key, err := totp.GetEncryptionKey(totp.Config{EncryptionKey: encoded})
		require.NoError(t, err)
		require.Len(t, key, 32)
	})

	t.Run("empty key", func(t *testing.T) {
		t.Parallel()
		_, err := totp.GetEncryptionKey(totp.Config{EncryptionKey: ""})
		require.ErrorIs(t, err, totp.ErrEncryptionKeyNotSet)
	})

	t.Run("invalid base64", func(t *testing.T) {
		t.Parallel()
		_, err := totp.GetEncryptionKey(totp.Config{EncryptionKey: "not-base64!@#"})
		require.ErrorIs(t, err, totp.ErrFailedToLoadEncryptionKey)
	})

	t.Run("wrong length", func(t *testing.T) {
		t.Parallel()
		short := base64.StdEncoding.EncodeToString(make([]byte, 16))
		_, err := totp.GetEncryptionKey(totp.Config{EncryptionKey: short})
		require.ErrorIs(t, err, totp.ErrInvalidEncryptionKeyLength)
	})
}

// mustKey returns a fresh random 32-byte AES key.
func mustKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	return key
}
