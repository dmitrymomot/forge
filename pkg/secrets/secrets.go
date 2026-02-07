package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"

	"golang.org/x/crypto/hkdf"
)

const (
	// KeySize is the required size in bytes for both app and workspace keys (AES-256).
	KeySize = 32
)

// hkdfInfo is the context string for HKDF key derivation.
var hkdfInfo = []byte("forge-secrets-v1")

// GenerateKey generates a cryptographically secure random key
// suitable for use as an app key or workspace key.
// The returned key is exactly KeySize (32) bytes.
func GenerateKey() ([]byte, error) {
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, errors.Join(ErrKeyDerivationFailed, err)
	}
	return key, nil
}

// ValidateKeys checks that both appKey and workspaceKey are exactly KeySize bytes.
func ValidateKeys(appKey, workspaceKey []byte) error {
	if len(appKey) != KeySize {
		return ErrInvalidAppKey
	}
	if len(workspaceKey) != KeySize {
		return ErrInvalidWorkspaceKey
	}
	return nil
}

// EncryptString encrypts plaintext using AES-256-GCM with a key derived from
// appKey and workspaceKey via HKDF. Returns a base64-encoded ciphertext string.
func EncryptString(plaintext string, appKey, workspaceKey []byte) (string, error) {
	if err := ValidateKeys(appKey, workspaceKey); err != nil {
		return "", errors.Join(ErrEncryptionFailed, err)
	}

	derived, err := deriveKey(appKey, workspaceKey)
	if err != nil {
		return "", errors.Join(ErrEncryptionFailed, err)
	}
	defer clearBytes(derived)

	sealed, err := encrypt(derived, []byte(plaintext))
	if err != nil {
		return "", errors.Join(ErrEncryptionFailed, err)
	}

	return base64.StdEncoding.EncodeToString(sealed), nil
}

// DecryptString decrypts a base64-encoded ciphertext using AES-256-GCM with a key
// derived from appKey and workspaceKey via HKDF.
func DecryptString(ciphertext string, appKey, workspaceKey []byte) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", errors.Join(ErrDecryptionFailed, ErrInvalidCiphertext)
	}

	plain, err := DecryptBytes(data, appKey, workspaceKey)
	if err != nil {
		return "", err
	}

	return string(plain), nil
}

// EncryptBytes encrypts data using AES-256-GCM with a key derived from appKey and
// workspaceKey via HKDF. Returns the nonce prepended to the ciphertext.
func EncryptBytes(data, appKey, workspaceKey []byte) ([]byte, error) {
	if err := ValidateKeys(appKey, workspaceKey); err != nil {
		return nil, errors.Join(ErrEncryptionFailed, err)
	}

	derived, err := deriveKey(appKey, workspaceKey)
	if err != nil {
		return nil, errors.Join(ErrEncryptionFailed, err)
	}
	defer clearBytes(derived)

	sealed, err := encrypt(derived, data)
	if err != nil {
		return nil, errors.Join(ErrEncryptionFailed, err)
	}

	return sealed, nil
}

// DecryptBytes decrypts data using AES-256-GCM with a key derived from appKey and
// workspaceKey via HKDF. Expects the nonce prepended to the ciphertext
// (as produced by EncryptBytes).
func DecryptBytes(data, appKey, workspaceKey []byte) ([]byte, error) {
	if err := ValidateKeys(appKey, workspaceKey); err != nil {
		return nil, errors.Join(ErrDecryptionFailed, err)
	}

	derived, err := deriveKey(appKey, workspaceKey)
	if err != nil {
		return nil, errors.Join(ErrDecryptionFailed, err)
	}
	defer clearBytes(derived)

	block, err := aes.NewCipher(derived)
	if err != nil {
		return nil, errors.Join(ErrDecryptionFailed, err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.Join(ErrDecryptionFailed, err)
	}

	nonceSize := aesGCM.NonceSize()
	if len(data) < nonceSize {
		return nil, errors.Join(ErrDecryptionFailed, ErrInvalidCiphertext)
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]

	plain, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.Join(ErrDecryptionFailed, err)
	}

	return plain, nil
}

// deriveKey uses HKDF-SHA256 to derive a 32-byte encryption key.
// appKey is used as input key material, workspaceKey as salt.
func deriveKey(appKey, workspaceKey []byte) ([]byte, error) {
	reader := hkdf.New(sha256.New, appKey, workspaceKey, hkdfInfo)

	derived := make([]byte, KeySize)
	if _, err := io.ReadFull(reader, derived); err != nil {
		return nil, errors.Join(ErrKeyDerivationFailed, err)
	}

	return derived, nil
}

// encrypt performs AES-256-GCM encryption with a random nonce.
// Returns nonce prepended to ciphertext.
func encrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	return aesGCM.Seal(nonce, nonce, plaintext, nil), nil
}

// clearBytes zeroes a byte slice to remove sensitive key material from memory.
func clearBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
