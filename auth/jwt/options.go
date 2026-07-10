package jwt

import (
	"crypto"
	"time"

	"github.com/dmitrymomot/forge/crypto/keyset"
)

// SignerOption configures NewSigner.
type SignerOption func(*signerConfig)

type signerConfig struct {
	ks     *keyset.Keyset
	hs     *keyset.Keyset
	direct *directKey
}

type directKey struct {
	key crypto.Signer
	kid string
	alg Alg
}

// WithKeyset loads asymmetric signing keys from ks. Key material must be
// PKCS#8 DER private keys; the alg is inferred per key: RSA (>=2048 bits)
// -> RS256, ECDSA P-256 -> ES256, Ed25519 -> EdDSA. The keyset primary
// signs; retired versions stay published via PublicKeys and JWKS.
func WithKeyset(ks *keyset.Keyset) SignerOption {
	return func(c *signerConfig) { c.ks = ks }
}

// WithHS256Keyset loads HMAC secrets from ks. Each version's material is
// the raw secret and must be at least 32 bytes.
func WithHS256Keyset(ks *keyset.Keyset) SignerOption {
	return func(c *signerConfig) { c.hs = ks }
}

// WithSignerKey supplies a single caller-owned signing key (HSM/KMS-backed
// crypto.Signer). HS256 is not accepted here — use WithHS256Keyset.
func WithSignerKey(kid string, alg Alg, key crypto.Signer) SignerOption {
	return func(c *signerConfig) { c.direct = &directKey{kid: kid, alg: alg, key: key} }
}

// ServeOption configures the Signer.JWKS handler.
type ServeOption func(*serveConfig)

type serveConfig struct {
	maxAge time.Duration
}

// WithCacheControl sets a "Cache-Control: public, max-age=..." header on
// JWKS responses. Without it no cache header is written.
func WithCacheControl(maxAge time.Duration) ServeOption {
	return func(c *serveConfig) { c.maxAge = maxAge }
}
