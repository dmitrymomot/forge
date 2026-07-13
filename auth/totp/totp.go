// Package totp implements RFC 6238 TOTP / RFC 4226 HOTP two-factor
// authentication: secret generation, skew-window verification with replay
// rejection, otpauth:// provisioning URIs, one-time backup codes, and a
// Manager over a tenant-aware Store seam. See doc.go for recipes.
package totp

import (
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"hash"
	"time"
)

// Algorithm selects the HMAC hash for code derivation.
type Algorithm int

// Supported HMAC algorithms. SHA1 is the authenticator-app default.
const (
	SHA1 Algorithm = iota
	SHA256
	SHA512
)

// String returns the otpauth URI parameter form: "SHA1", "SHA256", "SHA512".
func (a Algorithm) String() string {
	switch a {
	case SHA1:
		return "SHA1"
	case SHA256:
		return "SHA256"
	case SHA512:
		return "SHA512"
	default:
		return "UNKNOWN"
	}
}

// hashFunc returns the hash constructor for HMAC.
func (a Algorithm) hashFunc() func() hash.Hash {
	switch a {
	case SHA256:
		return sha256.New
	case SHA512:
		return sha512.New
	default:
		return sha1.New
	}
}

// secretSize is the RFC 4226 §4 recommendation: secret length = hash length.
func (a Algorithm) secretSize() int {
	switch a {
	case SHA256:
		return 32
	case SHA512:
		return 64
	default:
		return 20
	}
}

func (a Algorithm) valid() bool { return a == SHA1 || a == SHA256 || a == SHA512 }

// TOTP holds validated code-generation parameters. One instance serves both
// enrollment (ProvisioningURI) and verification, so the parameters an
// authenticator app was enrolled with can never drift from the ones Verify
// checks. Safe for concurrent use.
type TOTP struct {
	cfg config
}

// New validates opts and builds a TOTP. Defaults: 6 digits, 30s period,
// SHA-1, skew ±1.
func New(opts ...Option) (*TOTP, error) {
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}
	if err := cfg.validateCore(); err != nil {
		return nil, err
	}
	return &TOTP{cfg: cfg}, nil
}

func (c *config) validateCore() error {
	if c.digits != 6 && c.digits != 8 {
		return fmt.Errorf("totp: digits must be 6 or 8, got %d", c.digits)
	}
	if c.period < time.Second || c.period%time.Second != 0 {
		return fmt.Errorf("totp: period must be whole seconds >= 1s, got %s", c.period)
	}
	if c.skew < 0 {
		return fmt.Errorf("totp: skew must be >= 0, got %d", c.skew)
	}
	if !c.algorithm.valid() {
		return fmt.Errorf("totp: unknown algorithm %d", int(c.algorithm))
	}
	return nil
}
