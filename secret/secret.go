package secret

import (
	"encoding/base64"
	"errors"

	"github.com/dmitrymomot/forge/keyset"
	"github.com/dmitrymomot/forge/randx"
)

// Box performs authenticated symmetric encryption. Output is
// version-byte || nonce || ciphertext+tag; Decrypt reads the version and resolves the
// key (single key, or via keyset including retired keys) before authenticating.
type Box struct {
	ks      *keyset.Keyset // nil for single-key
	newAEAD aeadFor
	key     []byte
	aad     []byte
	ver     int
}

// New builds a single-key Box (AES-256-GCM by default, requiring a 32-byte key).
func New(key []byte, opts ...Option) (*Box, error) {
	c := newConfig(opts...)
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	if _, err := c.newAEAD(key); err != nil {
		return nil, err
	}
	return &Box{key: key, aad: c.aad, newAEAD: c.newAEAD}, nil
}

// FromKeyset builds a rotation-aware Box: it encrypts under the keyset's primary and
// decrypts material produced under any version (including retired keys).
func FromKeyset(ks *keyset.Keyset, opts ...Option) (*Box, error) {
	c := newConfig(opts...)
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	if ks == nil {
		return nil, ErrInvalidKeySize
	}
	ver, key := ks.Primary()
	if _, err := c.newAEAD(key); err != nil {
		return nil, err
	}
	return &Box{ks: ks, key: key, ver: ver, aad: c.aad, newAEAD: c.newAEAD}, nil
}

// Encrypt seals plaintext, returning version-byte || nonce || ciphertext+tag.
func (b *Box) Encrypt(plaintext []byte) ([]byte, error) {
	aead, err := b.newAEAD(b.key)
	if err != nil {
		return nil, err
	}
	nonce := randx.Bytes(aead.NonceSize())
	out := make([]byte, 0, 1+len(nonce)+len(plaintext)+aead.Overhead())
	out = append(out, byte(b.ver))
	out = append(out, nonce...)
	return aead.Seal(out, nonce, plaintext, b.aad), nil
}

// Decrypt opens a ciphertext produced by Encrypt.
func (b *Box) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < 1 {
		return nil, ErrDecryptFailed
	}
	ver := int(ciphertext[0])
	key := b.key
	switch {
	case b.ks != nil:
		k, ok := b.ks.ByVersion(ver)
		if !ok {
			return nil, ErrDecryptFailed
		}
		key = k
	case ver != b.ver:
		return nil, ErrDecryptFailed
	}
	aead, err := b.newAEAD(key)
	if err != nil {
		return nil, ErrDecryptFailed
	}
	ns := aead.NonceSize()
	if len(ciphertext) < 1+ns {
		return nil, ErrDecryptFailed
	}
	nonce := ciphertext[1 : 1+ns]
	pt, err := aead.Open(nil, nonce, ciphertext[1+ns:], b.aad)
	if err != nil {
		return nil, ErrDecryptFailed
	}
	return pt, nil
}

// EncryptString is Encrypt with unpadded base64url in/out.
func (b *Box) EncryptString(s string) (string, error) {
	ct, err := b.Encrypt([]byte(s))
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(ct), nil
}

// DecryptString is Decrypt with unpadded base64url in/out.
func (b *Box) DecryptString(s string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return "", ErrDecryptFailed
	}
	pt, err := b.Decrypt(raw)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}
