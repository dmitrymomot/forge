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

`docs/packages.md` entry for `auth/totp` is updated in the same PR: deps become
`core/random`, `core/clock`, `crypto/consttime`, `crypto/secret`
(+ `crypto/keyset` for the rotation constructor); pgstore adds `data/postgres`
+ `data/migration`. `core/qrcode` is *not* imported — we emit the otpauth URI,
the consumer renders it (`qrcode.DataURI(uri)`); the catalog note changes from
"QR image rendering lights up via core/qrcode" to "QR rendering composes with
core/qrcode".

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
- **Tenancy-agnostic via an opaque `subject` key.** Multi-tenant apps
  disagree about where 2FA lives: account-global (GitHub — one enrollment
  across all orgs; key = userID) vs per-tenant identity (Slack — separate
  enrollment per workspace; key = tenantID+userID). The Manager/Store key is
  therefore an opaque `subject string` — the consumer encodes whatever
  identifies an enrollment in their auth model. Single-tenant apps pass the
  user ID; per-tenant apps pass a composite like `tenantID + "/" + userID`
  (safe when IDs cannot contain the separator — forge `core/id` IDs never
  do). No tenant parameter, no ctx-derived tenant func (at login time there
  is no authenticated context to derive from). Documented as a doc.go recipe.
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
func (m *Manager) DisableAll(ctx context.Context, subjectPrefix string) (int, error)

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
- **DisableAll** — bulk-deletes every record whose subject starts with
  `subjectPrefix` (tenant offboarding: `DisableAll(ctx, tenantID+"/")`),
  returns the count. Requires the store to implement the optional
  `BulkDeleter` extension — `ErrUnsupported` otherwise. Deleting one user
  across all tenants is *not* prefix-shaped (user is the suffix); the doc.go
  recipe is to iterate the app's own membership list and `Disable` each.

### Options (Manager-only, on top of core options)

| Option | Default |
|---|---|
| `WithBackupCodeCount(int)` | 10 |
| `WithBackupCodeLength(int)` | 10 chars |

### Errors

```go
var (
    ErrInvalidCode     = errors.New("totp: invalid code")
    ErrReplayed        = errors.New("totp: code already used")
    ErrNotEnrolled     = errors.New("totp: not enrolled")
    ErrAlreadyEnrolled = errors.New("totp: already enrolled")
    ErrNotFound        = errors.New("totp: record not found") // store sentinel
    ErrUnsupported     = errors.New("totp: store does not support bulk delete")
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
    Get(ctx context.Context, subject string) (*Record, error) // ErrNotFound when absent
    Save(ctx context.Context, subject string, r *Record) error // upsert, full replace
    Delete(ctx context.Context, subject string) error          // no-op when absent
    // MarkUsed atomically sets LastUsedAt=usedAt iff the stored value is
    // earlier (or zero). false = a concurrent verify already claimed this
    // or a later step. This is the replay/race gate.
    MarkUsed(ctx context.Context, subject string, usedAt time.Time) (bool, error)
    // ConsumeBackup atomically removes hash if present. false = not present
    // (already spent or never existed). This is the single-use gate.
    ConsumeBackup(ctx context.Context, subject string, hash []byte) (bool, error)
}
```

The two atomic methods carry all concurrency correctness; `Get`/`Save`/`Delete`
are dumb CRUD. `NewMemoryStore()` ships in-package: `map[string]*Record` under
an `sync.RWMutex`, records deep-copied on the way in and out.

Optional extension (session `UserIndex` precedent — keeps the required seam
small while both built-in stores implement it):

```go
type BulkDeleter interface {
    DeleteByPrefix(ctx context.Context, subjectPrefix string) (int, error)
}
```

`Manager.DisableAll` type-asserts the store and returns `ErrUnsupported` for
custom stores that skip it. Memory store: `strings.HasPrefix` sweep under the
write lock.

## pgstore

```go
var Migrations fs.FS // embedded goose migration
func New(pool *pgxpool.Pool, opts ...Option) *Store
```

- Table `forge_totp`: `subject text PK`, `secret bytea NOT NULL`,
  `confirmed boolean NOT NULL DEFAULT false`,
  `last_used_at timestamptz`, `backup_hashes bytea[] NOT NULL DEFAULT '{}'`,
  `created_at` / `updated_at timestamptz NOT NULL DEFAULT now()`.
- Applied via `data/migration` under its own version table
  (`forge_totp_schema`), same as lock/pgstore.
- `MarkUsed`: `UPDATE forge_totp SET last_used_at=$2, updated_at=now()
  WHERE subject=$1 AND (last_used_at IS NULL OR last_used_at < $2)` —
  rows-affected = the boolean.
- `ConsumeBackup`: `UPDATE forge_totp SET backup_hashes=array_remove(backup_hashes,$2),
  updated_at=now() WHERE subject=$1 AND $2 = ANY(backup_hashes)` —
  rows-affected = the boolean.
- `Get` maps no-rows to `totp.ErrNotFound`. `Save` is an upsert
  (`INSERT … ON CONFLICT (subject) DO UPDATE`).
- `DeleteByPrefix`: `DELETE FROM forge_totp WHERE subject LIKE $1 || '%'`
  with `%`, `_`, `\` escaped in the prefix; rows-affected = the count.
  Prefix deletes are rare admin operations — a seq scan is acceptable, no
  extra index.

## doc.go recipes

1. **Full flow** — enroll (QR via `qrcode.DataURI`), confirm, login verify —
   the Manager version, a dozen lines.
2. **Grace period, user-scoped** — `mgr.LastVerified` + `time.Since(...) < N`,
   with the caveat: verifying on one device silences prompts on all devices
   for N.
3. **Remember-this-device, device-scoped (recommended)** — signed cookie via
   `crypto/token` (`WithPurpose("2fa-device")`, TTL = trust window), checked
   before prompting. Three lines each side.
4. **Multi-tenancy** — subject-key encoding for both models: account-global
   2FA (`subject = userID`) vs per-tenant enrollment
   (`subject = tenantID + "/" + userID`), with the separator-safety note and
   guidance on picking a model (does an enrollment belong to the account or
   to the membership?). Cleanup patterns: tenant offboarding =
   `DisableAll(ctx, tenantID+"/")`; account deletion across tenants =
   iterate the app's membership list, `Disable` each.
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
- **DeleteByPrefix**: deletes exactly the prefix-matched subjects (memory +
  pg); LIKE metacharacters (`%`, `_`, `\`) in a prefix must not over-match.
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
