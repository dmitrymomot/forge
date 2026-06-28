package kdf

import (
	"crypto/hkdf"
	"crypto/sha256"
	"fmt"

	"golang.org/x/crypto/argon2"
)

// Params holds Argon2id cost parameters, shared with the password package.
type Params struct {
	Time    uint32 // iterations
	Memory  uint32 // memory in KiB
	KeyLen  uint32 // output length in bytes
	Threads uint8  // parallelism
}

// DefaultParams returns sane Argon2id parameters (t=3, m=64MiB, p=4, 32-byte key).
func DefaultParams() Params {
	return Params{Time: 3, Memory: 64 * 1024, KeyLen: 32, Threads: 4}
}

// Validate reports whether every Params field is non-zero.
func (p Params) Validate() error {
	if p.Time == 0 || p.Memory == 0 || p.Threads == 0 || p.KeyLen == 0 {
		return fmt.Errorf("%w: all fields must be > 0", ErrInvalidParams)
	}
	return nil
}

// DeriveKey turns a low-entropy passphrase into KeyLen key bytes via Argon2id.
func DeriveKey(passphrase, salt []byte, p Params) ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return argon2.IDKey(passphrase, salt, p.Time, p.Memory, p.Threads, p.KeyLen), nil
}

// HKDF derives length bytes from a high-entropy secret using HKDF-SHA256. The info
// argument domain-separates outputs, so one master secret yields cryptographically
// unrelated keys per purpose.
func HKDF(secret, salt, info []byte, length int) ([]byte, error) {
	key, err := hkdf.Key(sha256.New, secret, salt, string(info), length)
	if err != nil {
		return nil, fmt.Errorf("kdf: hkdf: %w", err)
	}
	return key, nil
}
