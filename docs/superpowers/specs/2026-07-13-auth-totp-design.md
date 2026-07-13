# auth/totp — design

Date: 2026-07-13
Status: approved for planning

## Purpose

The complete 2FA package: RFC 6238 TOTP / RFC 4226 HOTP secret generation,
skew-window verification with replay rejection, otpauth:// provisioning URIs,
and one-time backup codes — plus a `Manager` over a `Store` seam that owns the
whole enrollment/verify lifecycle so consumers never handle plaintext secrets,
pending-enrollment state, hashing, or replay races themselves. Everything that
touches the store is already encrypted (TOTP secret) or hashed (backup codes).

## Placement

```
auth/totp/            core primitives + Manager + Store seam + memory store
auth/totp/pgstore/    Postgres driver (pgx), embedded goose migrations
```

Two layers, both public:

1. **Primitives** — stateless RFC math (`New`, `Verify`, `GenerateBackupCodes`,
   …). Escape hatch for consumers with their own storage discipline.
2. **Manager** — the flow orchestrator over `Store`. This is the documented
   front door.

`docs/packages.md` entry for `auth/totp` is deleted in the same PR (design.md
roadmap rule: the catalog lists only unbuilt packages; doc.go becomes the
reference). Actual deps: `core/random`, `core/clock`, `crypto/consttime`,
`crypto/secret` (+ `crypto/keyset` for the rotation constructor); pgstore adds
`data/postgres` + `data/migration`. `core/qrcode` is *not* imported — we emit
the otpauth URI, the consumer renders it (`qrcode.DataURI(uri)`).

## Decisions (resolved during brainstorming)

- **Replay protection is the default path.** `Verify` takes the last-used
  value and returns the matched one; codes at or before the stored step are
  rejected. Not optional, no boolean-only variant.
- **`time.Time`, not raw counter, in the public API.** `Verify(secret, code,
  lastUsed time.Time) (time.Time, error)` returns the *step-start* time
  (`counter × period`), so the round-trip through a `timestamptz` column is
  lossless (`t.Unix()/period` recovers the exact counter) and the stored value
  survives a period reconfiguration. Zero `time.Time{}` = never verified.
- **Grace period ("don't prompt again for N") is a recipe, not API.** It is
  session policy: user-scoped grace is `time.Since(lastVerified) < N` off
  `Manager.LastVerified`; device-scoped ("remember this browser") is a signed
  cookie via `crypto/token`. Both documented in doc.go with the security
  caveat that user-scoped grace silences 2FA on *all* devices.
- **Manager + Store seam over consumer-managed columns.** Earlier drafts made
  the consumer store pending secrets, hashes, and write conditional SQL; too
  much boilerplate and too easy to store plaintext. The Manager owns the
  state machine; the store sees only opaque bytes.
- **Encryption at rest is mandatory.** `NewManager` requires key material;
  there is no plaintext-storage configuration.
- **Tenancy via the canonical `WithScope` hook** (repo-wide rule; follows
  `auth/apikey`/`auth/otp` shipped precedent). The Manager takes an optional
  construction-time `WithScope(func(ctx) (string, error))` hook that derives
  the tenant from the request context on every operation, so no call site
  can forget isolation. Fail-closed: a hook error or empty scope aborts with
  `ErrScope`. Tenant is a first-class dimension of the store key —
  `(tenant, subject)` — never encoded into the subject string. Single-tenant
  apps omit the option (tenant `""`), zero ceremony. Account-global 2FA in a
  multi-tenant app (GitHub model) also omits it; per-tenant enrollment
  (Slack model) wires the hook to the tenant middleware. Bulk cleanup is
  exact-match by tenant — no prefix/LIKE matching anywhere.
- **pgstore ships in the same PR** (precedent: `resilience/lock/pgstore`).

## API surface — core primitives

```go
func New(opts ...Option) (*TOTP, error)

func (t *TOTP) GenerateSecret() (string, error)            // base32 (no padding), size per algorithm
func (t *TOTP) ProvisioningURI(secret, account string) string
func (t *TOTP) Code(secret string, at time.Time) (string, error)
func (t *TOTP) Verify(secret, code string, lastUsed time.Time) (time.Time, error)

// HOTP (RFC 4226); shares digits/algorithm config. Counter is caller state.
func (t *TOTP) HOTPCode(secret string, counter uint64) (string, error)
func (t *TOTP) VerifyHOTP(secret, code string, counter uint64, lookahead int) (uint64, error)
```

- `GenerateSecret`: random bytes via `core/random`, length matches the hash
  (SHA-1 → 20, SHA-256 → 32, SHA-512 → 64) per RFC 4226 §4, encoded
  uppercase base32 without padding.
- `ProvisioningURI`: `otpauth://totp/<Issuer>:<account>?secret=…&issuer=…
  &algorithm=…&digits=…&period=…` with label and params URL-escaped. Issuer
  appears in both label and query param (Key Uri Format convention).
- `Code`: computes the code for an explicit time — client side, CLIs, tests.
- `Verify`: checks the presented code against every step in
  `[now−skew, now+skew]` — evaluating **all** windows without early exit and
  comparing via `crypto/consttime` — then rejects any matched step ≤ the step
  derived from `lastUsed`. Success returns the matched step-start time for
  the caller (or Manager) to persist.
- `VerifyHOTP`: checks `counter … counter+lookahead`, returns the *next*
  counter value to store on success (matched + 1).

Backup codes are package-level functions (independent of TOTP params):

```go
func GenerateBackupCodes(count, length int) (codes []string, hashes [][]byte, err error)
func VerifyBackupCode(code string, hashes [][]byte) (idx int, ok bool)
```

- Codes: `length` chars (Manager default 10) from an unambiguous alphanumeric
  alphabet via `core/random`, displayed dash-grouped (`xxxxx-xxxxx`).
- Normalization (lowercase, strip `-` and spaces) applied before hashing and
  before verification, so user typing quirks don't matter.
- Hashes: SHA-256 of the normalized code. Codes are high-entropy random —
  a KDF (bcrypt/argon2) is unnecessary and would slow the constant-time scan.
- `VerifyBackupCode` compares against **every** hash with
  `consttime.BytesEqual` (no early exit), returns the matched index.

### Options (core)

| Option | Default | Validation |
|---|---|---|
| `WithIssuer(string)` | `""` | empty issuer → URI label is just the account, `issuer` param omitted (valid per Key Uri Format) |
| `WithDigits(int)` | 6 | 6 or 8 |
| `WithPeriod(time.Duration)` | 30s | ≥ 1s, whole seconds |
| `WithAlgorithm(Algorithm)` | SHA1 | SHA1 \| SHA256 \| SHA512 |
| `WithSkew(int)` | 1 | ≥ 0 |
| `WithClock(clock.Clock)` | wall clock | — |

`Algorithm` is a small enum type in this package (`totp.SHA1`, `totp.SHA256`,
`totp.SHA512`).

## API surface — Manager

```go
func NewManager(store Store, key []byte, opts ...Option) (*Manager, error)
func ManagerFromKeyset(store Store, ks *keyset.Keyset, opts ...Option) (*Manager, error)

func (m *Manager) BeginEnroll(ctx context.Context, subject, account string) (*Enrollment, error)
func (m *Manager) ConfirmEnroll(ctx context.Context, subject, code string) (backupCodes []string, err error)
func (m *Manager) Verify(ctx context.Context, subject, code string) (VerifyResult, error)
func (m *Manager) Enabled(ctx context.Context, subject string) (bool, error)
func (m *Manager) LastVerified(ctx context.Context, subject string) (time.Time, error)
func (m *Manager) RegenerateBackupCodes(ctx context.Context, subject string) ([]string, error)
func (m *Manager) Disable(ctx context.Context, subject string) error
func (m *Manager) DisableTenant(ctx context.Context) (int, error)

type Enrollment struct {
    Secret string // show for manual entry
    URI    string // render as QR (core/qrcode composes)
}

type VerifyResult struct {
    UsedBackupCode  bool
    BackupRemaining int // valid when UsedBackupCode
}
```

Key material builds a `crypto/secret.Box` (AES-256-GCM, versioned ciphertext);
`ManagerFromKeyset` mirrors `token.FromKeyset` and enables rotation.

Semantics:

- **BeginEnroll** — generates a secret, seals it, saves an *unconfirmed*
  record (overwriting any earlier unconfirmed one — re-showing the QR is
  idempotent). Fails with `ErrAlreadyEnrolled` if a confirmed record exists;
  re-enrollment requires `Disable` first.
- **ConfirmEnroll** — verifies the first code against the pending record
  (lastUsed = zero), flips `Confirmed`, generates backup codes, stores hashes,
  persists the matched step, returns plaintext codes exactly once.
  `ErrNotEnrolled` without a pending record; `ErrInvalidCode` on mismatch
  (record stays pending).
- **Verify** — refuses unconfirmed records (`ErrNotEnrolled`). Tries TOTP
  first: on code match, calls `Store.MarkUsed`; a `false` result means a
  concurrent request or replay already consumed that step → `ErrReplayed`.
  On TOTP mismatch, normalizes and hashes the input and scans backup hashes;
  on match calls `Store.ConsumeBackup` (a `false` result → `ErrInvalidCode`,
  the code was already spent). Neither path matching → `ErrInvalidCode`.
- **RegenerateBackupCodes** — replaces all hashes, returns fresh plaintext
  codes; `ErrNotEnrolled` unless confirmed.
- **Enabled** — `true` only for a confirmed record; absent or pending →
  `false, nil` (not an error — it's a policy query).
- **LastVerified** — `ErrNotEnrolled` unless a confirmed record exists;
  zero time when confirmed but never re-verified.
- **Disable** — deletes the record entirely (secret, hashes, state).
- **DisableTenant** — bulk-deletes every record in the scope-resolved tenant
  (offboarding, GDPR), returns the count. Requires `WithScope`: on an
  unscoped Manager it returns `ErrScope` (an unscoped app has no tenant to
  delete; platform-level jobs run it with a ctx bound to the target tenant —
  the "tenant switching" pattern from the `auth/otp` docs). Deleting one
  user across all tenants is the consumer's membership loop: per membership,
  a tenant-bound ctx + `Disable` (doc.go recipe).

Every Manager operation resolves the tenant via the scope hook first
(`nil` hook → tenant `""`; hook error or empty scope → `ErrScope`) and passes
it to the store alongside the subject. A record enrolled under one tenant is
invisible to every other tenant — cross-tenant `Verify`/`Enabled`/`Disable`
behave exactly as if the subject were not enrolled.

### Options (Manager-only, on top of core options)

| Option | Default |
|---|---|
| `WithBackupCodeCount(int)` | 10 |
| `WithBackupCodeLength(int)` | 10 chars |
| `WithScope(func(context.Context) (string, error))` | nil — unscoped, tenant `""` |

### Errors

```go
var (
    ErrInvalidCode     = errors.New("totp: invalid code")
    ErrReplayed        = errors.New("totp: code already used")
    ErrNotEnrolled     = errors.New("totp: not enrolled")
    ErrAlreadyEnrolled = errors.New("totp: already enrolled")
    ErrNotFound        = errors.New("totp: record not found") // store sentinel
    ErrScope           = errors.New("totp: scope resolution failed")
)
```

Config validation errors surface from `New`/`NewManager` as plain wrapped
errors. Decryption failure of a stored secret surfaces `secret.ErrDecryptFailed`
wrapped — it means key material is wrong, not that the user typed a bad code.

## Store contract

```go
type Record struct {
    Secret       []byte    // AEAD ciphertext — the store never sees plaintext
    Confirmed    bool
    LastUsedAt   time.Time // zero = never verified
    BackupHashes [][]byte  // SHA-256 of normalized codes
}

type Store interface {
    Get(ctx context.Context, tenant, subject string) (*Record, error) // ErrNotFound when absent
    Save(ctx context.Context, tenant, subject string, r *Record) error // upsert, full replace
    Delete(ctx context.Context, tenant, subject string) error          // no-op when absent
    // MarkUsed atomically sets LastUsedAt=usedAt iff the stored value is
    // earlier (or zero). false = a concurrent verify already claimed this
    // or a later step. This is the replay/race gate.
    MarkUsed(ctx context.Context, tenant, subject string, usedAt time.Time) (bool, error)
    // ConsumeBackup atomically removes hash if present. false = not present
    // (already spent or never existed). This is the single-use gate.
    ConsumeBackup(ctx context.Context, tenant, subject string, hash []byte) (bool, error)
    // DeleteTenant removes every record in the tenant, returning the count.
    DeleteTenant(ctx context.Context, tenant string) (int, error)
}
```

Tenant is an explicit key dimension on every method (`""` = unscoped) — the
Manager resolves it from the scope hook; stores never interpret it beyond
equality. The two atomic methods carry all concurrency correctness; the rest
is dumb CRUD. `NewMemoryStore()` ships in-package: map keyed on a
`struct{ tenant, subject string }` under a `sync.RWMutex`, records
deep-copied on the way in and out.

## pgstore

```go
var Migrations fs.FS // embedded goose migration
func New(pool *pgxpool.Pool) *Store // no options needed (apikey/pgstore precedent)
```

- Table `forge_totp`: `tenant text NOT NULL DEFAULT ''`, `subject text`,
  PK `(tenant, subject)`, `secret bytea NOT NULL`,
  `confirmed boolean NOT NULL DEFAULT false`,
  `last_used_at timestamptz`, `backup_hashes bytea[] NOT NULL DEFAULT '{}'`,
  `created_at` / `updated_at timestamptz NOT NULL DEFAULT now()`
  (tenant column precedent: `auth/apikey` pgstore).
- Applied via `data/migration` under its own version table
  (`forge_totp_schema`), same as lock/pgstore.
- `MarkUsed`: `UPDATE forge_totp SET last_used_at=$3, updated_at=now()
  WHERE tenant=$1 AND subject=$2 AND (last_used_at IS NULL OR last_used_at < $3)`
  — rows-affected = the boolean.
- `ConsumeBackup`: `UPDATE forge_totp SET backup_hashes=array_remove(backup_hashes,$3),
  updated_at=now() WHERE tenant=$1 AND subject=$2 AND $3 = ANY(backup_hashes)`
  — rows-affected = the boolean.
- `Get` maps no-rows to `totp.ErrNotFound`. `Save` is an upsert
  (`INSERT … ON CONFLICT (tenant, subject) DO UPDATE`).
- `DeleteTenant`: `DELETE FROM forge_totp WHERE tenant=$1` — exact match on
  the leading PK column; rows-affected = the count.

## doc.go recipes

1. **Full flow** — enroll (QR via `qrcode.DataURI`), confirm, login verify —
   the Manager version, a dozen lines.
2. **Grace period, user-scoped** — `mgr.LastVerified` + `time.Since(...) < N`,
   with the caveat: verifying on one device silences prompts on all devices
   for N.
3. **Remember-this-device, device-scoped (recommended)** — signed cookie via
   `crypto/token` (`WithPurpose("2fa-device")`, TTL = trust window), checked
   before prompting. Three lines each side.
4. **Multi-tenancy** — the `WithScope` hook wired to tenant middleware
   (mirrors the `auth/otp` doc.go treatment): single-tenant omits the
   option; account-global 2FA in a multi-tenant app (GitHub model) also
   omits it; per-tenant enrollment (Slack model) returns the request's
   tenant; platform-level code uses a reserved non-empty sentinel. Guidance
   on picking a model (does an enrollment belong to the account or to the
   membership?). Cleanup patterns: tenant offboarding = `DisableTenant`
   under a tenant-bound ctx; account deletion across tenants = iterate the
   app's membership list, `Disable` each.
5. **Custom Store contract** — the exact atomicity requirements of
   `MarkUsed`/`ConsumeBackup`, with the reference SQL.
6. **Brute-force pointer** — code guessing is rate-limited by `auth/lockout`
   (planned), not by this package.

## File anatomy

```
auth/totp/
    doc.go            package docs + recipes
    totp.go           TOTP struct, New, Code, Verify, HOTP
    secret.go         GenerateSecret, ProvisioningURI
    backup.go         GenerateBackupCodes, VerifyBackupCode, normalization
    manager.go        Manager
    store.go          Store, Record, memory store
    options.go        options for both constructors
    errors.go         sentinels
    *_test.go         black-box (package totp_test)
    bench_test.go
auth/totp/pgstore/
    doc.go
    pgstore.go
    pgstore_test.go   integration, skips without docker DSN
    migrations/00001_totp.sql
```

## Testing

- **RFC vectors**: RFC 6238 Appendix B (all three algorithms, 8 digits) and
  RFC 4226 Appendix D (10 HOTP codes) verbatim.
- **Verify semantics**: skew acceptance at ±1, rejection at ±2; replay
  rejection at the exact matched step and at earlier steps; `time.Time`
  round-trip equals counter round-trip.
- **Backup codes**: normalization equivalence (`ABCDE-FGHIJ` ≡ `abcdefghij`),
  consume-once via store, constant scan over full list.
- **Manager lifecycle**: begin → confirm → verify → regenerate → disable;
  `ErrAlreadyEnrolled` / `ErrNotEnrolled` edges; unconfirmed record refuses
  Verify; ConfirmEnroll failure leaves record pending.
- **Concurrency**: parallel `Verify` with the same code against memory store —
  exactly one success (`-race`).
- **pgstore integration**: same lifecycle + MarkUsed/ConsumeBackup atomicity
  against ephemeral docker pg16; skipped without DSN.
- **Tenancy** (mirrors `auth/otp` tenancy_test.go precedent): same subject
  enrolled under two tenants = independent records (separate secrets,
  counters, backup codes); cross-tenant Verify/Enabled/Disable see nothing;
  fail-closed — hook error or empty scope → `ErrScope` on every method;
  `DisableTenant` removes exactly one tenant's records (memory + pg) and
  errors with `ErrScope` on an unscoped Manager; unscoped Manager and
  scoped-with-sentinel Manager never collide.
- **Fuzz**: `Verify` (secret/code inputs — malformed base32 must error, never
  panic) and backup-code normalization.
- **Bench** (repo rule): `Verify` and `Code` with alloc counts; memory-store
  `MarkUsed` under contention.

## Non-goals (anti-scope)

- No rate limiting / attempt counting — `auth/lockout` composes.
- No HTTP handlers, middleware, or cookie management — recipes only.
- No QR image rendering — emit URI, `core/qrcode` composes.
- No plaintext-at-rest mode, no `WithoutEncryption`.
- No `WithSecretSize` (derived from algorithm), no backup-code format options,
  no otpauth `image` extension — icebox until a real consumer asks.
- No SMS/email OTP — that's `auth/otp` (separate catalog entry).
