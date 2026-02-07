package secrets

import "errors"

var (
	ErrInvalidAppKey       = errors.New("secrets: invalid app key, must be 32 bytes")
	ErrInvalidWorkspaceKey = errors.New("secrets: invalid workspace key, must be 32 bytes")
	ErrEncryptionFailed    = errors.New("secrets: encryption failed")
	ErrDecryptionFailed    = errors.New("secrets: decryption failed")
	ErrInvalidCiphertext   = errors.New("secrets: invalid ciphertext")
	ErrKeyDerivationFailed = errors.New("secrets: key derivation failed")
)
