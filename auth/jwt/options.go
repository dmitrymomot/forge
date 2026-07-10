package jwt

import (
	"crypto"
	"net/http"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
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

// VerifierOption configures NewVerifier.
type VerifierOption func(*verifierConfig)

type verifierConfig struct {
	clk        clock.Clock
	jwksURL    string
	iss        string
	aud        string
	keys       []Key
	keysets    []*keyset.Keyset // asymmetric PKCS#8 material
	hsKeysets  []*keyset.Keyset // raw HS256 secrets
	jwksCfg    jwksConfig
	leeway     time.Duration
	requireExp bool
}

// jwksConfig is a placeholder for JWKS fetch settings, wired up in Task 6.
type jwksConfig struct {
	client   *http.Client
	refresh  time.Duration
	cooldown time.Duration
}

// WithKeys adds explicit verification keys (e.g. from Signer.PublicKeys).
func WithKeys(keys ...Key) VerifierOption {
	return func(c *verifierConfig) { c.keys = append(c.keys, keys...) }
}

// WithVerifyKeyset derives verification keys from the public halves of an
// asymmetric keyset (PKCS#8 DER private-key material). All versions —
// primary and retired — verify.
func WithVerifyKeyset(ks *keyset.Keyset) VerifierOption {
	return func(c *verifierConfig) { c.keysets = append(c.keysets, ks) }
}

// WithVerifyHS256Keyset adds every version of an HS256 secret keyset as a
// verification key.
func WithVerifyHS256Keyset(ks *keyset.Keyset) VerifierOption {
	return func(c *verifierConfig) { c.hsKeysets = append(c.hsKeysets, ks) }
}

// WithClock overrides the time source used for exp/nbf checks (tests).
func WithClock(clk clock.Clock) VerifierOption {
	return func(c *verifierConfig) { c.clk = clk }
}
