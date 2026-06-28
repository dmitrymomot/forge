package secret

import "errors"

// ErrInvalidKeySize is returned when a key is not the size the cipher requires (32 bytes).
var ErrInvalidKeySize = errors.New("secret: invalid key size")

// ErrDecryptFailed is returned for any decryption failure — wrong key, unknown version,
// tampered or truncated ciphertext, bad AAD — without revealing which.
var ErrDecryptFailed = errors.New("secret: decryption failed")
