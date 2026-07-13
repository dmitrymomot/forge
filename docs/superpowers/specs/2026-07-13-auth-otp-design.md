# auth/otp — Design

Date: 2026-07-13
Status: approved for planning

## Purpose

Short numeric codes for email/SMS verification and passwordless login:
attempt-limited, TTL'd, hashed at rest, single-use. Delivery is the
caller's channel; HTTP behavior (anti-enumeration responses, throttling)
stays in the consumer. A complete product per the product-or-brick test:
wire a store and a secret and it works.

Catalog correction: the packages.md entry lists deps `core/random` +
`crypto/digest` only, but design.md's TTL-KV seam rule explicitly names
`otp` a consumer of `resilience/cache.Store` ("no package defines a
private byte-KV store"). The seam rule wins; `resilience/cache` is a dep.

## API surface

```go
func New(secret []byte, store cache.Store, opts ...Option) (*OTP, error)

func (o *OTP) Generate(ctx context.Context, identifier string) (string, error)
func (o *OTP) Verify(ctx context.Context, identifier, code string) error
func (o *OTP) Revoke(ctx context.Context, identifier string) error
```

- `secret` — HMAC pepper, required, min 16 bytes, validated in `New`
  (`ErrInvalidConfig`). Consumers load it from env themselves
  (`OTP_SECRET`); no Config struct — positional-required + options follows
  the `token.New(key, ...)` / `cookie.New(ks, ...)` precedent.
- `Generate` returns the plaintext code for the caller to deliver.
  Generating again for the same (purpose, scope, identifier) overwrites
  the previous record — "resend" semantics, exactly one active code per
  key.
- `Verify` is single-use: the record is deleted on success.
- `Revoke` deletes any outstanding code. Included because consumers
  cannot compute the storage key themselves; use cases: cancel a pending
  flow, invalidate on account-state change.
- `identifier` is an opaque, caller-canonicalized string (lowercased
  email, E.164 phone). The package never interprets it. doc.go warns:
  `Generate` and `Verify` must receive the identical canonical form.

### Options

| Option | Default | Notes |
|---|---|---|
| `WithPurpose(string)` | `"default"` | one instance per flow (login, email-verify, …); purpose isolates keys |
| `WithTTL(time.Duration)` | 10 min | code lifetime; must be > 0 |
| `WithLength(int)` | 6 | digits; valid range 4–10 |
| `WithMaxAttempts(int)` | 5 | per-code verify attempts; must be > 0 |
| `WithScope(func(ctx) (string, error))` | nil | tenant hook, see below |
| `WithClock(clock.Clock)` | `clock.System()` | expiry math in tests |

## Tenancy — dual-mode (repo-wide rule)

Every forge package works single- AND multi-tenant (CLAUDE.md rule,
2026-07-13). For otp:

- **Single-tenant:** omit `WithScope`; no scope part in keys; zero
  ceremony.
- **Multi-tenant:** `WithScope(hook)` where the hook reads TenantID from
  context (the planned `data/tenant` middleware puts it there; until it
  ships the hook is a plain func — no dependency). Scope is resolved
  inside every `Generate`/`Verify`/`Revoke`, so no call site can forget
  the tenant.
- **Fail closed:** if a hook is configured and errors or returns empty,
  the operation returns `ErrScope` — never a silent fall-through to an
  unscoped key (a tenant-A code must never verify in a global bucket).
- Consumers never compose key strings; the package builds keys with
  unambiguous domain separation (below), so there are no delimiter
  collisions and no per-call-site conventions.

## Storage

Rides `resilience/cache.Store` (memory store for dev/tests, cache/redis
in prod; the LRU memory store is documented as unsuitable for production
OTP just as for sessions).

**Key:** `otp:<purpose>:<hex(SHA256(lenPrefix(scope) || lenPrefix(identifier)))>`

- Deterministic — `Verify`/`Revoke` recompute it; no server-minted ID to
  round-trip.
- Hashed — no email/phone PII in store keys.
- Length-prefixed parts — `("a:b","c")` and `("a","b:c")` cannot collide.

**Value:** fixed manual binary encoding (no reflection), 42 bytes:

```
version(1) | attempts(1) | expiresAt unix seconds int64 BE (8) | HMAC-SHA256(secret, code) (32)
```

- Written with `Set` + `WithTTL` (plain overwrite → replace-on-generate;
  SetNX not needed).
- `expiresAt` inside the record serves two jobs: attempt-increment
  rewrites preserve the *remaining* TTL (never extend a code's life), and
  defense-in-depth if a store ignores TTL — `Verify` checks it
  explicitly.
- Remaining-TTL guard: `cache` treats TTL <= 0 as never-expire, so a
  record that expired between Get and the rewrite is deleted and treated
  as `ErrNotFound`, never written back immortal.
- Unknown version byte → treated as `ErrNotFound` (code effectively
  revoked; forward-compat).

**Why keyed HMAC, not plain SHA256:** a 6-digit code has 10^6 values;
plain (even salted) hashes of that keyspace are reversible in
milliseconds from a store dump. `HMAC-SHA256(secret, code)` makes a
store-only compromise worthless without the app's env secret. Precedent:
web/fingerprint's keyed digests.

## Verify semantics

1. Resolve scope (fail closed), recompute key, `Get`.
2. Miss, or record `expiresAt` passed → `ErrNotFound`.
3. `attempts >= maxAttempts` → delete record, `ErrTooManyAttempts`.
4. Constant-time compare via stdlib `hmac.Equal` (no `crypto/consttime`
   dep needed).
5. Mismatch → `attempts+1`; if the limit is now consumed, delete and
   return `ErrTooManyAttempts`, else write back with remaining TTL and
   return `ErrCodeMismatch`.
6. Match → delete (single-use), return nil.

Documented caveat: the attempt counter is read-modify-write, so N
*concurrent* wrong guesses can overshoot the limit by the in-flight
count. Immaterial against a 10^6 keyspace; sustained abuse is
`auth/lockout`'s layer (per-identifier lockout over the counter seam),
and request throttling is `ratelimit` middleware in the consumer.

## Errors (`errors.go`, single-line, `errors.Is`-matchable)

| Sentinel | Meaning |
|---|---|
| `ErrNotFound` | no active code (never issued, expired, or revoked) |
| `ErrCodeMismatch` | wrong code; attempts remain |
| `ErrTooManyAttempts` | attempt limit consumed; code invalidated |
| `ErrScope` | scope hook errored or returned empty |
| `ErrStore` | wraps underlying store failures (unwrap for driver error) |
| `ErrInvalidConfig` | `New` validation (secret too short, bad option values) |

doc.go guidance: consumers map `ErrNotFound` and `ErrCodeMismatch` to one
user-facing message ("invalid or expired code") — no oracle.

## Dependencies

`core/random` (DigitCode), `crypto/digest` (SHA256 + HMAC-SHA256),
`resilience/cache` (Store seam), `core/clock`. All existing; no new
external deps.

## Package anatomy

`auth/otp/`: `doc.go` (runnable passwordless-login example +
normalization warning + multi-tenant wiring + lockout/ratelimit
composition notes) · `options.go` · `errors.go` · `otp.go` (incl. record
encode/decode) · `otp_test.go` · `bench_test.go`. Estimated ~300 LOC
non-test — within the single-responsibility band.

## Testing

Black-box `otp_test` using cache's memory store + `clock.Mock`:

- happy path generate → verify → second verify is `ErrNotFound`
  (single-use)
- wrong code until `ErrTooManyAttempts`; correct code afterwards fails
- expiry via mock clock → `ErrNotFound`; attempt rewrite preserves
  remaining TTL (record dies at original deadline)
- replace-on-generate kills the old code
- purpose isolation (two instances, same identifier)
- scope isolation (two tenants, same identifier) + fail-closed missing
  scope + no-scope-hook single-tenant mode
- `Revoke` → `ErrNotFound`
- concurrent wrong guesses: limit overshoot bounded by in-flight count
  (race detector clean)
- fuzz `Verify` with arbitrary code strings (non-digit, wrong length,
  huge input)

Benchmarks per repo rule: `Generate` and `Verify` (memory store),
`b.ReportAllocs()`; post-benchmark optimization pass with before/after
numbers in the PR. Expected inherent costs: HMAC + SHA256 and store
round-trips; do not "optimize" crypto away.

## Out of scope

Delivery (caller's channel), rate limiting / lockout (`ratelimit`,
`auth/lockout`), TOTP/HOTP (`auth/totp`), alphanumeric codes, HTTP
handlers/middleware, challenge-ID round-trips (expressible by using a
random ID as the identifier — documented, not a feature).

## Consumer example (doc.go sketch)

```go
secret := []byte(os.Getenv("OTP_SECRET"))
store := redis.NewStore(client) // resilience/cache/redis; memory store for dev/tests

loginOTP, err := otp.New(secret, store,
    otp.WithPurpose("login"),
    // multi-tenant apps add:
    // otp.WithScope(func(ctx context.Context) (string, error) {
    //     return tenant.FromContext(ctx)
    // }),
)

// request a code
code, err := loginOTP.Generate(ctx, canonicalEmail)
// deliver via your channel; respond 202 whether or not the account exists

// verify
err = loginOTP.Verify(ctx, canonicalEmail, submitted)
switch {
case err == nil:                              // authenticated
case errors.Is(err, otp.ErrTooManyAttempts):  // "request a new code"
default:                                      // one message, no oracle
}
```
