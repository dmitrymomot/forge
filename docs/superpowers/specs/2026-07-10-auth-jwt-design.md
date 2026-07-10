# auth/jwt — Design

Date: 2026-07-10
Status: approved

## Purpose

Full JWT for forge: sign and verify with a pinned algorithm allowlist
(HS256/RS256/ES256/EdDSA — never negotiated), registered-claim checks
(exp/nbf/iss/aud), JWKS serve + fetch with kid cache and rotation, key
rotation via `crypto/keyset`. No JWE, ever.

Consumers: `auth/oauthserver` (issuing), `auth/oauthclient` (id_token
verify against provider JWKS), `auth/guard` (bearer verify), inter-service
auth, and small API apps that want plain HS256 with one secret.

## Decisions (locked during brainstorming)

1. **Key material**: both paths — `FromKeyset`-style options over
   `crypto/keyset` (env-loadable, versioned) AND a `crypto.Signer` escape
   hatch for HSM/KMS-backed keys later.
2. **Scope**: full catalog spec — asymmetric + JWKS both directions.
3. **HS256 included** for simple shared-secret apps, alongside
   RS256/ES256/EdDSA. Confusion attacks are closed structurally (see
   Verification rules).
4. **Claims**: typed generics. Package exports `Claims` (registered
   claims); consumers embed it and get their struct back from
   `Verify[T]`.
5. **Architecture**: split `Signer` / `Verifier` types (Approach 1) —
   each side carries only its own config.

## Public API

```go
package jwt // auth/jwt

type Alg string // "HS256", "RS256", "ES256", "EdDSA" — closed set

// Claims holds the RFC 7519 registered claims. Consumers embed it:
//
//	type AccessClaims struct {
//	    jwt.Claims
//	    TenantID string `json:"tid"`
//	}
type Claims struct {
    Issuer    string       `json:"iss,omitempty"`
    Subject   string       `json:"sub,omitempty"`
    Audience  Audience     `json:"aud,omitempty"`
    ExpiresAt *NumericDate `json:"exp,omitempty"`
    NotBefore *NumericDate `json:"nbf,omitempty"`
    IssuedAt  *NumericDate `json:"iat,omitempty"`
    ID        string       `json:"jti,omitempty"`
}

type Audience []string    // JSON: unmarshals string OR []string; marshals string when len==1
type NumericDate struct{} // wraps time.Time; JSON unix seconds (accepts fractional, truncates)

type Key struct { // a verification key bound to exactly one alg
    KID string
    Alg Alg
    Key crypto.PublicKey // or []byte secret for HS256
}

// --- Signing side ---

func NewSigner(opts ...SignerOption) (*Signer, error)

// SignerOptions (at least one key source required):
func WithKeyset(ks *keyset.Keyset) SignerOption      // material = PKCS#8 DER private keys;
                                                     // alg inferred: RSA→RS256 (key ≥2048 bits),
                                                     // ECDSA P-256→ES256, Ed25519→EdDSA;
                                                     // other key types → error at construction
func WithHS256Keyset(ks *keyset.Keyset) SignerOption // material = raw secrets, ≥32 bytes enforced
func WithSignerKey(kid string, alg Alg, key crypto.Signer) SignerOption // escape hatch (HSM/KMS)

// A Signer takes EXACTLY ONE key-source option; two or more → construction
// error. (Verifier sources, by contrast, are combinable.)

func (s *Signer) Sign(claims any) (string, error) // primary key; header {"alg","kid","typ":"JWT"};
                                                  // signs exactly what it is given — no auto iat
func (s *Signer) JWKS() http.Handler              // RFC 7517 {"keys":[...]}; public keys only;
                                                  // HS256 keys never included (HS256-only → empty set)
func (s *Signer) PublicKeys() []Key               // direct in-process wiring to NewVerifier(WithKeys(...))

// --- Verifying side ---

func NewVerifier(opts ...VerifierOption) (*Verifier, error)

// Key sources (at least one required; combinable):
func WithKeys(keys ...Key) VerifierOption
func WithVerifyKeyset(ks *keyset.Keyset) VerifierOption      // PKCS#8 DER; public halves derived; all versions usable
func WithVerifyHS256Keyset(ks *keyset.Keyset) VerifierOption // raw secrets; all versions usable
func WithJWKSURL(url string, opts ...JWKSOption) VerifierOption

// Policy:
func WithIssuer(iss string) VerifierOption      // exact match required when set
func WithAudience(aud string) VerifierOption    // token aud must contain it when set
func WithLeeway(d time.Duration) VerifierOption // default 30s; applies to exp and nbf
func WithoutExpiry() VerifierOption             // opt out of the exp-required default
func WithClock(c clock.Clock) VerifierOption    // test seam; default clock.System

// JWKS fetch options:
func WithHTTPClient(c *http.Client) JWKSOption        // default httpclient.New()
func WithRefreshInterval(d time.Duration) JWKSOption  // default 1h
func WithRefreshCooldown(d time.Duration) JWKSOption  // default 1m; floor for unknown-kid refetch

func Verify[T any](ctx context.Context, v *Verifier, token string) (*T, error)
```

`Verify` is package-level because Go methods cannot take type parameters.
`T` is expected to embed `jwt.Claims`; registered checks are enforced by
parsing the payload into `Claims` first, then unmarshaling into `T`.

kid convention: keyset-sourced keys use the keyset version formatted as a
decimal string ("2"); JWKS-sourced and `WithSignerKey` kids are arbitrary
strings.

## Verification rules

Order: parse → key resolution → signature → registered claims → typed
unmarshal. First failure wins; nothing later runs.

1. **Strict parsing**: exactly 3 segments; base64url without padding only;
   header must decode to a JSON object; `typ` accepted iff absent or
   `"JWT"` (case-insensitive per RFC 7515 §4.1.9).
2. **Key resolution**: by `kid` header. If the token has no kid and the
   verifier holds exactly one key, that key is used; no kid + multiple
   keys → `ErrUnknownKey`. There is NO try-all-keys fallback.
3. **Alg binding**: the token's `alg` header must equal the resolved
   key's declared alg. This single rule kills `alg:none`, alg-swap, and
   HMAC-with-public-key confusion — the attacker controls the header but
   never the key's binding.
4. **Signature**: per alg; HMAC comparison via `hmac.Equal`
   (constant-time). ES256 accepts raw R||S (64 bytes) only — not DER.
5. **Registered claims** (all against `WithClock`, with leeway):
   - `exp`: required unless `WithoutExpiry()`; `now < exp + leeway`.
   - `nbf`: if present, `now >= nbf - leeway`.
   - `iat`: not validated (informational).
   - `iss`: exact match if `WithIssuer` set.
   - `aud`: must contain the `WithAudience` value if set.
6. **Size guard**: tokens larger than 64 KiB are rejected as
   `ErrMalformed` before any decoding (DoS bound, mirrors the codec
   bounds precedent from idempotency).

## JWKS behavior

**Fetch** (`WithJWKSURL`): lazy first fetch on first Verify, deduplicated
via `resilience/singleflight`; keys cached in-memory. Refresh triggers:
TTL elapsed (`WithRefreshInterval`, default 1h) or unknown kid seen —
gated by `WithRefreshCooldown` (default 1m) so rotated issuers are picked
up promptly but a flood of bogus kids cannot hammer the endpoint.
Verify's `ctx` bounds the HTTP call. Stale-if-error: a failed refresh
logs nothing, returns the cached set; only a cold cache surfaces the
fetch error to Verify. Only keys with supported algs/key types are
loaded; unknown entries are skipped, not fatal. `use`/`key_ops` fields,
when present, must permit verification.

**Serve** (`Signer.JWKS()`): `{"keys":[...]}` as `application/json`,
RSA (`kty:RSA`), EC P-256 (`kty:EC`), Ed25519 (`kty:OKP`) public JWKs
with `kid` and `alg` set. HS256 keys never appear; an HS256-only signer
serves `{"keys":[]}` (documented in doc.go). Optional
`WithCacheControl(maxAge)` on the handler.

## Errors

`errors.go`, single-line, `errors.Is`-matchable sentinels:

- `ErrMalformed` — structure/encoding/size failures
- `ErrSignature` — signature mismatch
- `ErrExpired`, `ErrNotYetValid`
- `ErrIssuerMismatch`, `ErrAudienceMismatch`
- `ErrUnknownKey` — kid not resolvable (after any permitted refetch)
- `ErrNoKeys` — key resolution found an empty set at use (e.g. JWKS
  endpoint returned zero usable keys); a missing key-source option is a
  construction error, not this sentinel
- `ErrBadKey` — construction-time key material problems

`guard` will map all Verify errors to 401.

## Package anatomy

`auth/jwt`, single package (stdlib crypto only — nothing to isolate):

- `doc.go` — runnable HS256 quick-start + asymmetric JWKS example
- `claims.go` — Claims, Audience, NumericDate
- `signer.go`, `verifier.go`, `jwks.go` (serve + fetch), `options.go`, `errors.go`

Dependencies: stdlib + `crypto/keyset`, `resilience/singleflight`,
`web/httpclient` (default JWKS client), `core/clock`. Estimated ~800 LOC —
top of the band; catalog scopes it as "Full JWT".

## Testing (black-box, package `jwt_test`)

- Golden vector: RFC 7515 A.1 (HS256) verbatim. RS256/ES256/EdDSA are
  covered by stdlib-crypto cross-checks in both directions instead —
  tokens our Signer produces must verify under stdlib `rsa`/`ecdsa`/
  `ed25519`, and stdlib-signed tokens must pass our Verify. (RFC 8037
  A.4's payload is not a JSON claims object, so it cannot pass full
  Verify; long RS256/ES256 vector constants are transcription hazards.)
- Adversarial: `alg:none`, alg-swap, HS256-with-public-key confusion,
  tampered payload/signature, padding in base64url, >64 KiB token,
  wrong/missing kid, garbage input, `typ` mismatch.
- Claim boundaries: exp/nbf at and around leeway edges via `clock.Mock`;
  aud string-vs-array forms; missing exp with and without
  `WithoutExpiry`.
- JWKS: `httptest` server — rotation pickup on unknown kid, cooldown
  suppression, TTL refresh, stale-if-error, cold-cache error surface,
  concurrent Verify under `-race`.
- Round-trip property tests per alg (Sign → Verify, all keyset versions).
- Benchmarks: HS256 and EdDSA sign/verify with allocation counts.

## Non-goals

- JWE (encryption) — anti-scope, permanent.
- Alg negotiation or extensible alg registry — the allowlist is closed.
- JWKS persistence — cache is in-memory only.
- OAuth2/OIDC flows — those are `auth/oauthclient`/`auth/oauthserver`.
- Token storage/revocation lists — consumer concern (or future guard/session work).
