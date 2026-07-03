package sign

import (
	"crypto/hmac"
	"encoding/base64"
	"errors"
	"hash"
	"strconv"
	"strings"

	"github.com/dmitrymomot/forge/crypto/consttime"
	"github.com/dmitrymomot/forge/crypto/keyset"
)

// Signer produces and verifies HMAC tags. Sign/Verify operate on a single key; the
// String forms carry the key version so a keyset-backed signer can verify material
// produced under retired keys (transparent rotation).
type Signer struct {
	ks   *keyset.Keyset // nil for a raw single-key signer
	hash func() hash.Hash
	key  []byte
	ver  int
}

// New builds a single-key signer. An empty key returns ErrInvalidKey.
func New(key []byte, opts ...Option) (*Signer, error) {
	if len(key) == 0 {
		return nil, ErrInvalidKey
	}
	c := newConfig(opts...)
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	return &Signer{key: key, hash: c.hash}, nil
}

// FromKeyset builds a rotation-aware signer: it signs with the keyset's primary and
// verifies String material against any version (including retired keys).
func FromKeyset(ks *keyset.Keyset, opts ...Option) (*Signer, error) {
	if ks == nil {
		return nil, ErrInvalidKey
	}
	c := newConfig(opts...)
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	ver, key := ks.Primary()
	return &Signer{ks: ks, key: key, ver: ver, hash: c.hash}, nil
}

func (s *Signer) mac(key, msg []byte) []byte {
	m := hmac.New(s.hash, key)
	m.Write(msg)
	return m.Sum(nil)
}

// Sign returns the raw MAC of msg under the primary key.
func (s *Signer) Sign(msg []byte) []byte { return s.mac(s.key, msg) }

// Verify reports whether mac is a valid MAC for msg, in constant time.
func (s *Signer) Verify(msg, mac []byte) bool {
	return consttime.BytesEqual(s.mac(s.key, msg), mac)
}

// SignString returns "<version>.<base64url-mac>" for msg.
func (s *Signer) SignString(msg string) string {
	mac := s.mac(s.key, []byte(msg))
	return strconv.Itoa(s.ver) + "." + base64.RawURLEncoding.EncodeToString(mac)
}

// VerifyString verifies a "<version>.<base64url-mac>" string against msg, resolving the
// key by version when this signer is keyset-backed. Returns false on any parse failure.
func (s *Signer) VerifyString(msg, signed string) bool {
	verStr, macStr, ok := strings.Cut(signed, ".")
	if !ok {
		return false
	}
	ver, err := strconv.Atoi(verStr)
	if err != nil {
		return false
	}
	mac, err := base64.RawURLEncoding.DecodeString(macStr)
	if err != nil {
		return false
	}
	key := s.key
	switch {
	case s.ks != nil:
		k, found := s.ks.ByVersion(ver)
		if !found {
			return false
		}
		key = k
	case ver != s.ver:
		return false
	}
	return consttime.BytesEqual(s.mac(key, []byte(msg)), mac)
}
