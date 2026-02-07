package secrets_test

import (
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/pkg/secrets"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateKey(t *testing.T) {
	t.Parallel()

	t.Run("returns 32-byte key", func(t *testing.T) {
		t.Parallel()
		key, err := secrets.GenerateKey()
		require.NoError(t, err)
		require.Len(t, key, secrets.KeySize)
	})

	t.Run("generates unique keys", func(t *testing.T) {
		t.Parallel()
		key1, err := secrets.GenerateKey()
		require.NoError(t, err)
		key2, err := secrets.GenerateKey()
		require.NoError(t, err)
		assert.NotEqual(t, key1, key2)
	})
}

func TestValidateKeys(t *testing.T) {
	t.Parallel()

	validKey := make([]byte, 32)

	tests := []struct {
		name         string
		appKey       []byte
		workspaceKey []byte
		wantErr      error
	}{
		{
			name:         "both valid",
			appKey:       validKey,
			workspaceKey: validKey,
			wantErr:      nil,
		},
		{
			name:         "app key too short",
			appKey:       make([]byte, 16),
			workspaceKey: validKey,
			wantErr:      secrets.ErrInvalidAppKey,
		},
		{
			name:         "app key nil",
			appKey:       nil,
			workspaceKey: validKey,
			wantErr:      secrets.ErrInvalidAppKey,
		},
		{
			name:         "workspace key too short",
			appKey:       validKey,
			workspaceKey: make([]byte, 16),
			wantErr:      secrets.ErrInvalidWorkspaceKey,
		},
		{
			name:         "workspace key nil",
			appKey:       validKey,
			workspaceKey: nil,
			wantErr:      secrets.ErrInvalidWorkspaceKey,
		},
		{
			name:         "both invalid returns app key error first",
			appKey:       make([]byte, 16),
			workspaceKey: make([]byte, 16),
			wantErr:      secrets.ErrInvalidAppKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := secrets.ValidateKeys(tt.appKey, tt.workspaceKey)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestEncryptDecryptString(t *testing.T) {
	t.Parallel()

	appKey, err := secrets.GenerateKey()
	require.NoError(t, err)
	wsKey, err := secrets.GenerateKey()
	require.NoError(t, err)

	tests := []struct {
		name      string
		plaintext string
	}{
		{name: "normal text", plaintext: "hello world"},
		{name: "empty string", plaintext: ""},
		{name: "unicode", plaintext: "こんにちは世界 🌍"},
		{name: "long string", plaintext: strings.Repeat("a", 1<<16)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			encrypted, err := secrets.EncryptString(tt.plaintext, appKey, wsKey)
			require.NoError(t, err)
			require.NotEmpty(t, encrypted)

			decrypted, err := secrets.DecryptString(encrypted, appKey, wsKey)
			require.NoError(t, err)
			require.Equal(t, tt.plaintext, decrypted)
		})
	}
}

func TestEncryptDecryptBytes(t *testing.T) {
	t.Parallel()

	appKey, err := secrets.GenerateKey()
	require.NoError(t, err)
	wsKey, err := secrets.GenerateKey()
	require.NoError(t, err)

	t.Run("normal bytes", func(t *testing.T) {
		t.Parallel()
		data := []byte{0xDE, 0xAD, 0xBE, 0xEF}
		encrypted, err := secrets.EncryptBytes(data, appKey, wsKey)
		require.NoError(t, err)

		decrypted, err := secrets.DecryptBytes(encrypted, appKey, wsKey)
		require.NoError(t, err)
		require.Equal(t, data, decrypted)
	})

	t.Run("empty bytes", func(t *testing.T) {
		t.Parallel()
		encrypted, err := secrets.EncryptBytes([]byte{}, appKey, wsKey)
		require.NoError(t, err)

		decrypted, err := secrets.DecryptBytes(encrypted, appKey, wsKey)
		require.NoError(t, err)
		require.Empty(t, decrypted)
	})

	t.Run("all byte values", func(t *testing.T) {
		t.Parallel()
		data := make([]byte, 256)
		for i := range data {
			data[i] = byte(i)
		}
		encrypted, err := secrets.EncryptBytes(data, appKey, wsKey)
		require.NoError(t, err)

		decrypted, err := secrets.DecryptBytes(encrypted, appKey, wsKey)
		require.NoError(t, err)
		require.Equal(t, data, decrypted)
	})
}

func TestEncryptString_InvalidKeys(t *testing.T) {
	t.Parallel()

	validKey := make([]byte, 32)
	shortKey := make([]byte, 16)

	t.Run("invalid app key", func(t *testing.T) {
		t.Parallel()
		_, err := secrets.EncryptString("data", shortKey, validKey)
		require.ErrorIs(t, err, secrets.ErrEncryptionFailed)
		require.ErrorIs(t, err, secrets.ErrInvalidAppKey)
	})

	t.Run("invalid workspace key", func(t *testing.T) {
		t.Parallel()
		_, err := secrets.EncryptString("data", validKey, shortKey)
		require.ErrorIs(t, err, secrets.ErrEncryptionFailed)
		require.ErrorIs(t, err, secrets.ErrInvalidWorkspaceKey)
	})
}

func TestDecryptString_InvalidInputs(t *testing.T) {
	t.Parallel()

	appKey, err := secrets.GenerateKey()
	require.NoError(t, err)
	wsKey, err := secrets.GenerateKey()
	require.NoError(t, err)

	t.Run("invalid base64", func(t *testing.T) {
		t.Parallel()
		_, err := secrets.DecryptString("not-valid-base64!@#$", appKey, wsKey)
		require.ErrorIs(t, err, secrets.ErrDecryptionFailed)
		require.ErrorIs(t, err, secrets.ErrInvalidCiphertext)
	})

	t.Run("too short ciphertext", func(t *testing.T) {
		t.Parallel()
		_, err := secrets.DecryptString("c2hvcnQ=", appKey, wsKey) // "short"
		require.ErrorIs(t, err, secrets.ErrDecryptionFailed)
		require.ErrorIs(t, err, secrets.ErrInvalidCiphertext)
	})

	t.Run("tampered ciphertext", func(t *testing.T) {
		t.Parallel()
		encrypted, err := secrets.EncryptString("hello", appKey, wsKey)
		require.NoError(t, err)

		// Flip a byte in the middle of the ciphertext
		tampered := []byte(encrypted)
		tampered[len(tampered)/2] ^= 0xFF
		_, err = secrets.DecryptString(string(tampered), appKey, wsKey)
		require.ErrorIs(t, err, secrets.ErrDecryptionFailed)
	})

	t.Run("wrong app key", func(t *testing.T) {
		t.Parallel()
		encrypted, err := secrets.EncryptString("hello", appKey, wsKey)
		require.NoError(t, err)

		wrongKey, err := secrets.GenerateKey()
		require.NoError(t, err)
		_, err = secrets.DecryptString(encrypted, wrongKey, wsKey)
		require.ErrorIs(t, err, secrets.ErrDecryptionFailed)
	})

	t.Run("wrong workspace key", func(t *testing.T) {
		t.Parallel()
		encrypted, err := secrets.EncryptString("hello", appKey, wsKey)
		require.NoError(t, err)

		wrongKey, err := secrets.GenerateKey()
		require.NoError(t, err)
		_, err = secrets.DecryptString(encrypted, appKey, wrongKey)
		require.ErrorIs(t, err, secrets.ErrDecryptionFailed)
	})
}

func TestDecryptBytes_InvalidInputs(t *testing.T) {
	t.Parallel()

	validKey := make([]byte, 32)

	t.Run("nil data", func(t *testing.T) {
		t.Parallel()
		_, err := secrets.DecryptBytes(nil, validKey, validKey)
		require.ErrorIs(t, err, secrets.ErrDecryptionFailed)
		require.ErrorIs(t, err, secrets.ErrInvalidCiphertext)
	})

	t.Run("data shorter than nonce", func(t *testing.T) {
		t.Parallel()
		_, err := secrets.DecryptBytes([]byte("short"), validKey, validKey)
		require.ErrorIs(t, err, secrets.ErrDecryptionFailed)
		require.ErrorIs(t, err, secrets.ErrInvalidCiphertext)
	})

	t.Run("invalid keys", func(t *testing.T) {
		t.Parallel()
		_, err := secrets.DecryptBytes(make([]byte, 100), make([]byte, 16), validKey)
		require.ErrorIs(t, err, secrets.ErrDecryptionFailed)
		require.ErrorIs(t, err, secrets.ErrInvalidAppKey)
	})
}

func TestTenantIsolation(t *testing.T) {
	t.Parallel()

	appKey, err := secrets.GenerateKey()
	require.NoError(t, err)
	wsKey1, err := secrets.GenerateKey()
	require.NoError(t, err)
	wsKey2, err := secrets.GenerateKey()
	require.NoError(t, err)

	t.Run("different workspace keys produce different ciphertext", func(t *testing.T) {
		t.Parallel()
		enc1, err := secrets.EncryptString("same plaintext", appKey, wsKey1)
		require.NoError(t, err)
		enc2, err := secrets.EncryptString("same plaintext", appKey, wsKey2)
		require.NoError(t, err)
		assert.NotEqual(t, enc1, enc2)
	})

	t.Run("cross-tenant decrypt fails", func(t *testing.T) {
		t.Parallel()
		encrypted, err := secrets.EncryptString("secret data", appKey, wsKey1)
		require.NoError(t, err)

		_, err = secrets.DecryptString(encrypted, appKey, wsKey2)
		require.ErrorIs(t, err, secrets.ErrDecryptionFailed)
	})

	t.Run("cross-app decrypt fails", func(t *testing.T) {
		t.Parallel()
		appKey2, err := secrets.GenerateKey()
		require.NoError(t, err)

		encrypted, err := secrets.EncryptString("secret data", appKey, wsKey1)
		require.NoError(t, err)

		_, err = secrets.DecryptString(encrypted, appKey2, wsKey1)
		require.ErrorIs(t, err, secrets.ErrDecryptionFailed)
	})
}

func TestEncryptString_Uniqueness(t *testing.T) {
	t.Parallel()

	appKey, err := secrets.GenerateKey()
	require.NoError(t, err)
	wsKey, err := secrets.GenerateKey()
	require.NoError(t, err)

	enc1, err := secrets.EncryptString("same plaintext", appKey, wsKey)
	require.NoError(t, err)
	enc2, err := secrets.EncryptString("same plaintext", appKey, wsKey)
	require.NoError(t, err)

	assert.NotEqual(t, enc1, enc2, "same plaintext should produce different ciphertext due to random nonce")
}
