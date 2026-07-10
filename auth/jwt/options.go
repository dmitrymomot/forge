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

// jwksConfig holds JWKS fetch settings configured via JWKSOption.
type jwksConfig struct {
	client       *http.Client
	refresh      time.Duration
	cooldown     time.Duration
	fetchTimeout time.Duration
}

// JWKSOption configures JWKS fetching for WithJWKSURL.
type JWKSOption func(*jwksConfig)

// WithJWKSURL adds a remote RFC 7517 JWK set as a verification key source.
// Fetching is lazy: the first Verify triggers it. Keys are cached in
// memory, refreshed when the refresh interval elapses, and refetched on an
// unknown kid no more often than the cooldown.
func WithJWKSURL(url string, opts ...JWKSOption) VerifierOption {
	return func(c *verifierConfig) {
		c.jwksURL = url
		for _, o := range opts {
			o(&c.jwksCfg)
		}
	}
}

// WithHTTPClient sets the HTTP client used to fetch the JWK set.
// Default: httpclient.New().
func WithHTTPClient(client *http.Client) JWKSOption {
	return func(c *jwksConfig) { c.client = client }
}

// WithRefreshInterval sets how long a fetched JWK set stays fresh.
// Default 1h.
func WithRefreshInterval(d time.Duration) JWKSOption {
	return func(c *jwksConfig) { c.refresh = d }
}

// WithRefreshCooldown sets the minimum gap between unknown-kid refetches.
// Default 1m.
func WithRefreshCooldown(d time.Duration) JWKSOption {
	return func(c *jwksConfig) { c.cooldown = d }
}

// WithFetchTimeout bounds each JWKS HTTP fetch. Because fetches are
// deduplicated (the caller's context deadline does not propagate to the
// shared fetch), this is the ceiling on how long a Verify can block on a
// stalled JWKS endpoint. Default 30s.
func WithFetchTimeout(d time.Duration) JWKSOption {
	return func(c *jwksConfig) { c.fetchTimeout = d }
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

// WithIssuer requires the iss claim to equal iss exactly.
func WithIssuer(iss string) VerifierOption {
	return func(c *verifierConfig) { c.iss = iss }
}

// WithAudience requires the aud claim to contain aud.
func WithAudience(aud string) VerifierOption {
	return func(c *verifierConfig) { c.aud = aud }
}

// WithLeeway sets the clock-skew tolerance applied to exp and nbf.
// Default 30s.
func WithLeeway(d time.Duration) VerifierOption {
	return func(c *verifierConfig) { c.leeway = d }
}

// WithoutExpiry accepts tokens with no exp claim. By default a missing
// exp is rejected as ErrExpired.
func WithoutExpiry() VerifierOption {
	return func(c *verifierConfig) { c.requireExp = false }
}
