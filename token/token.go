package token

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/clock"
	"github.com/dmitrymomot/forge/keyset"
	"github.com/dmitrymomot/forge/randx"
	"github.com/dmitrymomot/forge/secret"
	"github.com/dmitrymomot/forge/sign"
)

// envelope is the JSON-serialized token body.
type envelope[T any] struct {
	Payload T      `json:"pld"`
	Purpose string `json:"prp,omitempty"`
	Nonce   string `json:"nce"`
	Exp     int64  `json:"exp,omitempty"`
}

// Codec issues and parses opaque, signed, optionally-encrypted, expiring tokens carrying
// a payload of type T. It is deliberately not JWT.
type Codec[T any] struct {
	signer  *sign.Signer
	box     *secret.Box
	clk     clock.Clock
	purpose string
	ttl     time.Duration
}

// New builds a single-key Codec.
func New[T any](key []byte, opts ...Option) (*Codec[T], error) {
	signer, err := sign.New(key)
	if err != nil {
		return nil, err
	}
	c, err := newConfig(opts...)
	if err != nil {
		return nil, err
	}
	return &Codec[T]{signer: signer, box: c.box, clk: c.clk, purpose: c.purpose, ttl: c.ttl}, nil
}

// FromKeyset builds a rotation-aware Codec (signs under primary, verifies any version).
func FromKeyset[T any](ks *keyset.Keyset, opts ...Option) (*Codec[T], error) {
	signer, err := sign.FromKeyset(ks)
	if err != nil {
		return nil, err
	}
	c, err := newConfig(opts...)
	if err != nil {
		return nil, err
	}
	return &Codec[T]{signer: signer, box: c.box, clk: c.clk, purpose: c.purpose, ttl: c.ttl}, nil
}

// Issue marshals payload into a signed (optionally encrypted) url-safe token string.
func (c *Codec[T]) Issue(payload T) (string, error) {
	env := envelope[T]{Purpose: c.purpose, Nonce: randx.URLSafe(8), Payload: payload}
	if c.ttl > 0 {
		env.Exp = c.clk.Now().Add(c.ttl).Unix()
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if c.box != nil {
		enc, err := c.box.Encrypt(raw)
		if err != nil {
			return "", err
		}
		raw = enc
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	return body + "." + c.signer.SignString(body), nil // SignString returns "ver.mac"
}

// Parse verifies, decrypts (if applicable), and decodes a token back into a payload.
func (c *Codec[T]) Parse(token string) (T, error) {
	var zero T
	body, sig, ok := strings.Cut(token, ".")
	if !ok || body == "" || sig == "" {
		return zero, ErrMalformed
	}
	if !c.signer.VerifyString(body, sig) {
		return zero, ErrBadSignature
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return zero, ErrMalformed
	}
	if c.box != nil {
		dec, err := c.box.Decrypt(raw)
		if err != nil {
			return zero, ErrBadSignature // authenticated-encryption failure ~ tamper
		}
		raw = dec
	}
	var env envelope[T]
	if err := json.Unmarshal(raw, &env); err != nil {
		return zero, ErrMalformed
	}
	if env.Exp != 0 && c.clk.Now().Unix() > env.Exp {
		return zero, ErrExpired
	}
	if env.Purpose != c.purpose {
		return zero, ErrWrongPurpose
	}
	return env.Payload, nil
}
