package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

const keySize = 32

// aeadFor builds an AEAD from a key.
type aeadFor func(key []byte) (cipher.AEAD, error)

type config struct {
	newAEAD aeadFor
	aad     []byte
	errs    []error
}

// Option configures New/FromKeyset. Invalid values accumulate and are returned.
type Option func(*config)

func newConfig(opts ...Option) *config {
	c := &config{newAEAD: newGCM}
	for _, o := range opts {
		o(c)
	}
	return c
}

// WithAAD binds additional authenticated data to every Encrypt/Decrypt. The same AAD
// must be supplied to decrypt; a mismatch fails authentication.
func WithAAD(aad []byte) Option { return func(c *config) { c.aad = aad } }

// WithChaCha switches from the default AES-256-GCM to XChaCha20-Poly1305 (24-byte nonce).
func WithChaCha() Option { return func(c *config) { c.newAEAD = newChaCha } }

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != keySize {
		return nil, ErrInvalidKeySize
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKeySize, err)
	}
	return cipher.NewGCM(block)
}

func newChaCha(key []byte) (cipher.AEAD, error) {
	if len(key) != chacha20poly1305.KeySize {
		return nil, ErrInvalidKeySize
	}
	return chacha20poly1305.NewX(key)
}
