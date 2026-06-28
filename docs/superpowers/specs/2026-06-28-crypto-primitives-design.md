# Design: P1 crypto primitives (+ `clock`/`randx` foundation)

- **Date:** 2026-06-28
- **Status:** Draft for review
- **Scope:** The **P1 crypto layer** from `docs/maximal-package-set.md` — nine flat
  top-level packages (`subtlex`, `hashx`, `redact`, `keyset`, `sign`, `secret`, `kdf`,
  `password`, `token`) — plus the two **P0 foundation seams they require** that do not yet
  exist in the repo (`clock`, `randx`). Eleven packages total, built in five dependency
  waves. Each is a small, single-responsibility package of stdlib-first cryptographic
  building blocks following forge DNA. The only external dependency is
  `golang.org/x/crypto` (argon2/bcrypt/scrypt/chacha), already vendored as indirect and
  promoted to direct, confined to the three packages that need it (`password`, `kdf`,
  `secret`-option). No package imports `supervisor`. This spec stops at the primitives — it
  does **not** build `cookie`, `csrf`, `session`, `apikey`, `secretsource`, or any auth
  package; those are downstream consumers in later phases.

## Overview

These packages are the security building blocks the rest of forge composes on: constant-time
comparison, HMAC signing, authenticated encryption, key derivation, password hashing, opaque
expiring tokens, versioned key management, and log/JSON redaction. They are intentionally
**low-level and unopinionated** — each does one cryptographic job with safe defaults, returns
plain values and `errors.Is`-matchable sentinels, and gets out of the way. Higher layers
(`cookie`, `session`, `token`-driven auth flows, `secretsource`) assemble them.

Two of the nine crypto packages depend on P0 primitives that are mandated framework-wide but
not yet built: `token` needs `randx` (secure entropy) and `clock` (testable time). Rather
than inline those seams or defer `token`, this effort builds **minimal** `clock` and `randx`
first — they are tiny, mandated, and needed by nearly everything downstream (id, csrf,
session, apikey, scheduler). Their scope here is the minimum the crypto layer needs; they can
grow later (e.g. `clock` timers when the scheduler lands) without breaking callers.

A representative end-to-end usage, deriving per-purpose keys from one master secret and
issuing a signed, expiring, purpose-bound reset token:

```go
import (
	"time"

	"github.com/dmitrymomot/forge/kdf"
	"github.com/dmitrymomot/forge/secret"
	"github.com/dmitrymomot/forge/token"
)

// One master secret in the environment; derive independent keys per purpose.
master := []byte(cfg.MasterKey.Expose()) // cfg.MasterKey is a redact.Secret[string]
salt    := []byte("myapp-v1")

tokenKey, _ := kdf.HKDF(master, salt, []byte("reset-token"), 32)
fieldKey, _ := kdf.HKDF(master, salt, []byte("pii-field-encryption"), 32)

box, _ := secret.New(fieldKey) // AES-256-GCM, for encrypting PII columns

type Reset struct {
	UserID string `json:"uid"`
}

codec, _ := token.New[Reset](tokenKey,
	token.WithTTL(15*time.Minute),
	token.WithPurpose("pwreset"))

tok, _  := codec.Issue(Reset{UserID: "u_123"}) // opaque url-safe string for the email link
got, err := codec.Parse(tok)                    // got.UserID == "u_123"
// err is one of: token.ErrExpired / ErrBadSignature / ErrMalformed / ErrWrongPurpose
```

## Design DNA (applies to every package)

Each package follows the framework conventions already established by the shipped packages:

- **No magic:** no reflection, no service containers, values via params not context.
- **One of two idioms here:** stateless **free-funcs** (`subtlex`, `hashx`, `redact`
  helpers, `kdf`, `randx`) · or `New(...Option)` / `New[T](...Option)` returning a small
  stateful value (`sign.Signer`, `secret.Box`, `token.Codec[T]`, `keyset.Keyset`).
  **Options are `type Option func(*config)` — never builders.** None of these need an
  env-loadable `Config`; key material and params are passed explicitly by the consumer.
- **Anatomy:** `doc.go` (package doc + runnable example) · `errors.go`
  (`errors.Is`-matchable single-line sentinels, no stacks/blobs) · `options.go` where
  options exist · impl files · black-box `_test.go` in `pkg_test`.
- **Public methods never return unexported types.**
- **Constant-time everywhere it matters:** every secret comparison routes through
  `subtlex`; no `==`/`bytes.Equal` on MACs, tags, or hashes.
- Flat, single-responsibility packages; black-box tests only; all in-process (no
  integration env gating needed).

## Resolved decisions

1. **Layout: flat top-level** (`clock/`, `randx/`, `subtlex/`, …), matching all 12 shipped
   packages and CLAUDE.md "keep flat". The roadmap's `pkg/*` label and the README's
   `pkg/id/` mention are treated as stale; not honored here.
2. **`randx.Bytes(n) []byte` panics** on the effectively-impossible `crypto/rand` failure
   (an unrecoverable broken-OS-RNG condition), keeping every ID/token/nonce call site clean.
   `randx.Read(p) error` is the error-returning escape hatch for callers who want it.
3. **`clock` ships `Now()`-only** (interface + system clock + mock). Timers/tickers are
   deferred until the scheduler needs them; adding them later is additive.
4. **Key wiring = "raw key primary, keyset opt-in" (Approach A).** `sign`/`secret`/`token`
   take a raw `key []byte` for the single-key case via `New`, and a `*keyset.Keyset` for
   rotation via `FromKeyset`. The key version rides in the encoded output; decrypt/verify
   resolves the version → key (including retired keys) inside the package. `keyset` stays
   "optional" tier.
5. **`token` is generic-primary:** `token.New[T]` / `Codec[T]` with internal JSON encoding
   of the payload. `T` may be a struct or `[]byte`. Deliberately not JWT.

---

## Package designs

### Wave 0 — foundation

#### `clock` — testable time seam

The mandated seam for "what time is it" so time-dependent code (starting with `token`
expiry) is deterministic in tests.

```go
package clock

// Clock reports the current time. Production code depends on this interface, never time.Now directly.
type Clock interface {
	Now() time.Time
}

// System returns the production clock backed by time.Now().
func System() Clock

// Mock is a controllable Clock for tests.
type Mock struct{ /* unexported */ }

func NewMock(t time.Time) *Mock
func (m *Mock) Now() time.Time
func (m *Mock) Set(t time.Time)
func (m *Mock) Advance(d time.Duration)
```

- `System()` returns a zero-state value implementing `Clock`; safe to call repeatedly.
- `Mock` is goroutine-safe (mutex-guarded) so concurrent code under test can read it.
- Consumers inject a `Clock` (e.g. `token.WithClock`), defaulting to `System()`. No
  package-level global clock — values via params.
- **Out of scope (deferred):** `After`, `NewTimer`, `NewTicker`, `Sleep`. Add when the
  scheduler/async layer needs them.

#### `randx` — secure entropy

Thin, safe wrappers over `crypto/rand`.

```go
package randx

// Bytes returns n cryptographically secure random bytes.
// Panics only if crypto/rand fails, which indicates a broken OS RNG (unrecoverable).
func Bytes(n int) []byte

// Read fills p with secure random bytes; the error escape hatch for callers who want it.
func Read(p []byte) error

func Hex(n int) string     // hex encoding of n random bytes (2n chars)
func URLSafe(n int) string // base64.RawURLEncoding of n random bytes
func Int(max int) int      // unbiased integer in [0, max); panics if max <= 0
```

- `Bytes`/`Hex`/`URLSafe` are the entropy sources for token nonces, salts, opaque IDs.
- `Int` uses rejection sampling for an unbiased result (no modulo bias).
- stdlib-only: `crypto/rand`, `encoding/hex`, `encoding/base64`.

### Wave 1 — leaves (no internal deps)

#### `subtlex` — constant-time, length-safe comparison

```go
package subtlex

// BytesEqual reports whether a and b are equal in constant time, without leaking
// length via early return: both inputs are first reduced to a fixed-length digest
// (HMAC/SHA-256 over each side), then compared with crypto/subtle.
func BytesEqual(a, b []byte) bool
func StringEqual(a, b string) bool
```

- The #1 timing-attack footgun removed: callers never reach for `==` or `bytes.Equal` on
  secrets. Internal dependency of `sign`, `password`, `keyset`, `secret`, `token`.
- Length-safe by construction (the digest equalizes length before the subtle compare), so
  comparing values of different lengths does not reveal that fact through timing.
- stdlib-only: `crypto/subtle`, `crypto/hmac`, `crypto/sha256`.

#### `hashx` — digest convenience

```go
package hashx

func SHA256(b []byte) []byte
func SHA256Hex(b []byte) string
func SHA256Base64(b []byte) string // base64.RawStdEncoding
func SHA512(b []byte) []byte
func SHA512Hex(b []byte) string

func HMACSHA256(key, msg []byte) []byte
func HMACSHA256Hex(key, msg []byte) string

func FileSHA256(path string) (string, error) // streaming; hex digest
```

- Removes per-call boilerplate for ETags, cache keys, dedup, content addressing.
- **Excludes MD5/SHA1** by design (no insecure digests).
- stdlib-only: `crypto/sha256`, `crypto/sha512`, `crypto/hmac`, `encoding/hex`,
  `encoding/base64`, `io`, `os`.

#### `redact` — keep secrets out of logs & JSON

```go
package redact

// Secret wraps a value so it renders as "REDACTED" through fmt, encoding/json, and log/slog,
// and is revealed only via Expose().
type Secret[T any] struct{ /* unexported */ }

func New[T any](v T) Secret[T]
func (s Secret[T]) Expose() T
func (s Secret[T]) String() string                // "REDACTED"  (fmt.Stringer)
func (s Secret[T]) GoString() string              // "REDACTED"  (%#v)
func (s Secret[T]) MarshalJSON() ([]byte, error)  // "REDACTED"  (json.Marshaler)
func (s Secret[T]) LogValue() slog.Value          // REDACTED    (slog.LogValuer)

// Free-func helpers for data you do not control.
func String(s string) string                                   // partial mask, e.g. "sk_l***f8a2"
func Map(m map[string]any, keys ...string) map[string]any       // copy with named keys scrubbed
```

- The seam with `logger`: makes "log the whole config/request" safe by default — a
  careless `slog.Info("...", "cfg", cfg)` cannot leak a wrapped field.
- `Map` returns a shallow copy with the named keys replaced by `"REDACTED"`; it does not
  mutate the input.
- Does **not** fetch from a vault; purely a wrapper + scrub helpers.
- stdlib-only: `fmt`, `encoding/json`, `log/slog`, `strings`.

#### `kdf` — key derivation

(no forge-internal dependencies — a leaf alongside the others in this wave)

```go
package kdf

type Params struct {
	Time    uint32 // argon2 iterations
	Memory  uint32 // argon2 memory in KiB
	Threads uint8
	KeyLen  uint32
}

func DefaultParams() Params
func (p Params) Validate() error

// HKDF derives length bytes from a high-entropy secret (stdlib crypto/hkdf, Go 1.24+).
func HKDF(secret, salt, info []byte, length int) ([]byte, error)

// DeriveKey turns a low-entropy passphrase into key bytes via Argon2id.
func DeriveKey(passphrase, salt []byte, p Params) ([]byte, error)
```

- Two distinct jobs: **HKDF** for "one master secret → many purpose-scoped, independent
  keys" (the `info` argument domain-separates outputs); **DeriveKey** (Argon2id) for
  "user passphrase → encryption key bytes".
- Distinct from `password` (which *verifies* a login and never exposes the key) and from
  `keyset` (which *stores/rotates* keys). Centralizes safe Argon2 parameters via `Params`,
  reused by `password`.
- Deps: `crypto/hkdf` (stdlib, Go 1.24+), `golang.org/x/crypto/argon2`.
- **Scrypt:** out of scope for v1 (Argon2id is the recommended memory-hard KDF). Add a
  `Scrypt` func later if a consumer needs it; the package shape already accommodates it.

### Wave 2 — keyed (depend on `subtlex`)

#### `keyset` — in-memory versioned keyring

```go
package keyset

type Keyset struct{ /* unexported */ }

func New(opts ...Option) (*Keyset, error)

func WithPrimary(version int, key []byte) Option
func WithRetired(version int, key []byte) Option
func WithBase64Keys(s string) Option // "2:<b64>,1:<b64>" — highest version is primary

func (k *Keyset) Primary() (version int, key []byte)
func (k *Keyset) ByVersion(v int) ([]byte, bool)
func (k *Keyset) All() iter.Seq2[int, []byte]

// Sentinels
var (
	ErrNoPrimary     = errors.New("keyset: no primary key configured")
	ErrBadKeyMaterial = errors.New("keyset: invalid key material")
)
```

- Backs `secret`/`sign`/`token` rotation instead of each reinventing a keyring.
- `WithBase64Keys` parses comma-separated `version:base64` pairs (typically from one env
  var); the highest version becomes primary, the rest retired. Duplicate versions or
  undecodable material → `ErrBadKeyMaterial`. No primary → `ErrNoPrimary`.
- Key bytes are compared with `subtlex` where comparison is needed.
- **Not** a cloud KMS client. stdlib + `subtlex`: `encoding/base64`, `iter`, `errors`,
  `sort`/`strings`.

#### `sign` — HMAC sign/verify

```go
package sign

type Signer struct{ /* unexported */ }

func New(key []byte, opts ...Option) (*Signer, error)
func FromKeyset(ks *keyset.Keyset, opts ...Option) (*Signer, error)

func WithHash(h func() hash.Hash) Option // default sha256.New

func (s *Signer) Sign(msg []byte) []byte                  // raw MAC (single key)
func (s *Signer) Verify(msg, mac []byte) bool             // constant-time (subtlex)
func (s *Signer) SignString(msg string) string            // "<version>.<base64url-mac>"
func (s *Signer) VerifyString(msg, signed string) bool    // parses version, resolves key

var (
	ErrInvalidKey   = errors.New("sign: invalid key")
	ErrBadSignature = errors.New("sign: signature mismatch")
)
```

- `Sign`/`Verify` are the raw single-key HMAC primitive (no version tag). `SignString`
  /`VerifyString` carry the key version in the encoded form so a `FromKeyset` signer can
  verify values produced under retired keys, and so rotation is transparent.
- `New` with an empty/short key → `ErrInvalidKey`. Verification is always constant-time via
  `subtlex`. (`ErrBadSignature` is exported for higher layers that surface a typed error;
  the bool methods return false rather than err.)
- stdlib + `subtlex`/`keyset`: `crypto/hmac`, `crypto/sha256`, `encoding/base64`, `hash`.

#### `password` — Argon2id hashing with bcrypt fallback

```go
package password

type Algorithm int
const (
	Argon2id Algorithm = iota // default
	Bcrypt
)

func Hash(password string, opts ...Option) (string, error)               // PHC self-describing string
func Verify(password, encoded string) (ok bool, needsRehash bool, err error)

func WithArgon2Params(p kdf.Params) Option
func WithBcryptCost(cost int) Option
func WithAlgorithm(a Algorithm) Option

var (
	ErrMismatch   = errors.New("password: hash mismatch")
	ErrInvalidHash = errors.New("password: malformed encoded hash")
)
```

- Default Argon2id with PHC encoding (`$argon2id$v=19$m=...,t=...,p=...$salt$hash`); bcrypt
  produces its own self-describing `$2a$...` string.
- `Verify` detects the algorithm from the encoded prefix, compares in constant time, and
  returns `needsRehash=true` when stored params drift from current defaults (transparent
  upgrade on next successful login). A wrong password → `(false, _, nil)`; a malformed
  `encoded` → `ErrInvalidHash`.
- Reuses `kdf.Params` for Argon2 parameters so the two packages share one parameter type.
- Deps: `golang.org/x/crypto/argon2`, `golang.org/x/crypto/bcrypt`, `subtlex`, `randx`
  (salt), `encoding/base64`, `crypto/subtle`.

### Wave 3 — AEAD

#### `secret` — authenticated symmetric encryption

```go
package secret

type Box struct{ /* unexported */ }

func New(key []byte, opts ...Option) (*Box, error)          // AES-256-GCM default
func FromKeyset(ks *keyset.Keyset, opts ...Option) (*Box, error)

func WithAAD(aad []byte) Option // additional authenticated data bound to every op
func WithChaCha() Option         // XChaCha20-Poly1305 (x/crypto), 24-byte nonce

func (b *Box) Encrypt(plaintext []byte) ([]byte, error) // out = version || nonce || ciphertext
func (b *Box) Decrypt(ciphertext []byte) ([]byte, error)
func (b *Box) EncryptString(s string) (string, error)   // base64url of Encrypt output
func (b *Box) DecryptString(s string) (string, error)

var (
	ErrInvalidKeySize = errors.New("secret: invalid key size")
	ErrDecryptFailed  = errors.New("secret: decryption failed")
)
```

- AES-256-GCM by default (stdlib `crypto/aes` + `crypto/cipher`), requiring a 32-byte key.
  `WithChaCha()` switches to XChaCha20-Poly1305 (`golang.org/x/crypto/chacha20poly1305`),
  whose 24-byte random nonce is safe for high-volume random-nonce use.
- Output is `version-byte || nonce || ciphertext+tag`; `Decrypt` reads the version, resolves
  the key (single key, or via `keyset` including retired keys), and authenticates. Any
  failure (bad key, tampered data, unknown version) → `ErrDecryptFailed` (no oracle detail).
- Nonces come from `randx`. Underpins `cookie`/`session`/`token`-encrypt and field crypto.
- Deps: `crypto/aes`, `crypto/cipher`, `encoding/base64`, `keyset`, `randx`,
  `golang.org/x/crypto/chacha20poly1305` (option only).

### Wave 4 — tokens

#### `token` — opaque, signed, expiring, purpose-bound tokens

```go
package token

type Codec[T any] struct{ /* unexported */ }

func New[T any](key []byte, opts ...Option) (*Codec[T], error)
func FromKeyset[T any](ks *keyset.Keyset, opts ...Option) (*Codec[T], error)

func WithTTL(d time.Duration) Option
func WithPurpose(p string) Option
func WithEncrypt(box *secret.Box) Option // encrypt the payload, not just sign it
func WithClock(c clock.Clock) Option     // default clock.System()

func (c *Codec[T]) Issue(payload T) (string, error)
func (c *Codec[T]) Parse(token string) (T, error)

var (
	ErrExpired      = errors.New("token: expired")
	ErrBadSignature = errors.New("token: signature mismatch")
	ErrMalformed    = errors.New("token: malformed")
	ErrWrongPurpose = errors.New("token: wrong purpose")
)
```

- For email-verify / password-reset / magic-link / invite flows. **Deliberately not JWT** —
  opaque, app-internal, no header algorithm negotiation.
- Wire format: envelope `{v, exp, purpose, nonce, payload-JSON}` → (optional `secret`
  encrypt) → `sign` HMAC → base64url. `nonce` comes from `randx` so identical payloads
  produce distinct tokens.
- `Issue` JSON-marshals `T`, stamps `exp = clock.Now() + ttl` and `purpose`. `Parse`
  verifies the signature first (constant-time), then checks expiry against the injected
  `Clock`, then purpose, then unmarshals `T`. Each failure maps to its sentinel.
- Composes `sign`, `secret` (optional), `randx`, `clock`. No JWT library.

---

## Build order (five waves)

A clean DAG; each wave depends only on earlier waves.

| Wave | Packages | Depends on |
|---|---|---|
| 0 | `clock`, `randx` | stdlib only |
| 1 | `subtlex`, `hashx`, `redact`, `kdf` | stdlib only (+ `x/crypto` for `kdf`) |
| 2 | `keyset`, `sign`, `password` | `subtlex` (+ `randx`, `kdf` for `password`) |
| 3 | `secret` | `keyset`, `randx` |
| 4 | `token` | `sign`, `secret`, `randx`, `clock` |

Within a wave the packages are independent and parallelizable. `kdf` has no forge-internal
dependencies (stdlib `crypto/hkdf` + `x/crypto/argon2`), so it sits in wave 1; `password`
(wave 2) reuses its `Params` type.

## Testing strategy

- **Black-box only** (`pkg_test` packages), per the project rule and the
  `black-box-tests-external-package` convention.
- **Deterministic time** via `clock.Mock` for all `token` expiry tests.
- **Coverage targets per package:** round-trip success; tamper detection (flipped byte →
  `ErrBadSignature`/`ErrDecryptFailed`); expiry (`clock.Mock.Advance` past TTL →
  `ErrExpired`); wrong purpose; key rotation (encrypt/sign under primary, decrypt/verify
  under retired); malformed input; wrong key size.
- **Known-answer tests** for `subtlex` (equal/unequal, differing lengths), `hashx` (digests
  against published vectors), `sign` (stable MAC for fixed key+msg), `kdf` (HKDF RFC 5869
  vectors).
- **Redaction tests** assert `fmt`/`%#v`/`json.Marshal`/`slog` all emit `REDACTED` and
  `Expose()` round-trips; `Map` does not mutate its input.
- All in-process — no integration env gating, no `FORGE_TEST_*` flags.

## Dependencies

- `golang.org/x/crypto` promoted from indirect → direct in `go.mod`, used only by
  `password` (argon2, bcrypt), `kdf` (argon2), and `secret` (chacha20poly1305, option only).
- Everything else is stdlib (`crypto/*`, `encoding/*`, `log/slog`, `iter`, `hash`, `time`).
- No package imports `supervisor`, `logger`, or any other forge package outside this layer
  (intra-layer edges only, per the DAG above). `redact` is the seam logger will consume, but
  `redact` itself imports nothing from forge.

## Anti-scope (explicitly out)

- **JWT / JOSE** — `token` is opaque by design.
- **Cloud KMS / vault clients** — `keyset` is in-memory only; fetching secrets is
  `secretsource` (P7).
- **Asymmetric crypto** (Ed25519 signing, X25519, RSA) — not needed by the consumers in
  scope; add a dedicated package later if a flow requires it.
- **Cookie/CSRF/session/apikey/auth** — downstream consumers in P4/P5, not this spec.
- **`clock` timers/tickers**, **`kdf` scrypt**, **MD5/SHA1 digests** — deferred or excluded
  as noted per package.

## Open questions

None outstanding — the three flagged calls (flat layout, `randx.Bytes` panic semantics,
`clock` `Now()`-only) and the two architectural choices (Approach A key wiring,
generic-primary `token`) are resolved above.
