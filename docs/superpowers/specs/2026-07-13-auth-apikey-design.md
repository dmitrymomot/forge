# auth/apikey — design

Date: 2026-07-13
Status: approved for planning

## Purpose

The full API-key product for tenant- or user-owned keys: Stripe-style
prefixed keys with a checksum for cheap rejection, hash stored, plaintext
shown once — plus management (create/list/revoke/rotate, per-key scopes,
optional expiry, last-used-at tracking) behind a storage-agnostic `Store`,
and request verification as a `guard.Verifier`.

Works for both ownership shapes with one model: **personal keys** (`Subject`
= user id, `Tenant` = the user's org) and **tenant-wide keys** (`Subject` =
whatever principal represents the org acting as itself — tenant id or a
service-account id, caller's choice). The package never invents principal
semantics; it requires a non-empty `Subject` and carries it.

Primary consumers: any service exposing a partner/programmatic API.
`auth/scim` lists apikey-or-oauthserver as its authentication.

## Placement

`auth/apikey` core package plus one driver subpackage
`auth/apikey/pgstore` (pgx). Core imports: `auth/guard`, `core/id`,
`core/random`, `crypto/consttime` (+ stdlib `crypto/sha256`,
`hash/crc32`) — core never imports a driver client. Standard anatomy:
`doc.go` (runnable example), `apikey.go` (Key/Filter/CreateParams),
`keygen.go`, `manager.go`, `verify.go`, `store.go` (seam), `memory.go`
(in-memory store), `options.go`, `errors.go`, black-box tests +
`bench_test.go`.

When this ships: delete the `auth/apikey` entry from docs/packages.md and
un-tag the planned-dep references (`auth/scim`); the entry's dep line gains
`core/id` (UUIDv7 record ids), decided here.

## Decisions (resolved during brainstorming)

1. **Hash-lookup verification.** Store `SHA-256(full plaintext key)` hex;
   verify = hash the presented credential, exact-match `GetByHash`,
   `consttime.StringEqual` on the returned hash as defense-in-depth.
   Unsalted SHA-256 is safe at 256-bit key entropy (no brute-forceable
   preimage); no bcrypt/argon2 on the hot path. A split key-id+secret
   format was rejected — a stored `Preview` field gives the same dashboard
   UX without parsing complexity.
2. **Key anatomy** — `<prefix>_<payload><checksum>`:
   - `prefix`: construction-time (`WithPrefix("sk_live")`, default
     `"key"`), validated `[a-z0-9_]+`; environments are just differently
     configured issuers, the package invents no env segment.
   - `payload`: 43 random base62 chars via `random.String` (~256 bits).
   - `checksum`: CRC32 (IEEE) of the payload bytes, base62-encoded, fixed
     6 chars (GitHub's scheme) — malformed/truncated keys are rejected
     before any store hit, and secret scanners can validate leaks offline.
   - `Preview` = first 12 plaintext chars, stored for dashboards.
   - `ID` = `id.NewUUID()` (UUIDv7, time-ordered, SQL-native) — record
     identity is never derived from the secret. All management ops key on
     `id.UUID`.
3. **Plaintext is returned exactly once** — from `Create` and `Rotate`,
   alongside the stored record. The record's exported `Hash` field is not
   secret (preimage-resistant digest, useless to authenticate); one shared
   record type between Manager and Store beats a split public/store type.
4. **last-used-at = throttled best-effort touch.** `Verify` calls
   `store.Touch` only when the loaded record's `LastUsedAt` is staler than
   `WithTouchInterval` (default 60s; `0` = every request, negative =
   disabled). Touch errors never fail verification (documented
   best-effort).
5. **Rotation = graceful roll.** `Rotate(ctx, id, grace)` mints a new key
   inheriting Subject/Tenant/Scopes/Name/Meta and sets the old key's
   `ExpiresAt = now+grace` (`0` = immediate cutover). Both keys verify
   during the grace window. Revoked/expired keys cannot be rotated.
6. **Scopes are carried, not enforced.** `Create` stores them; `Verify`
   copies them into `guard.Identity.Scopes`. No `RequireScope` middleware —
   enforcement belongs to the future shared authz seam (`auth/rbac`,
   guard's 401-vs-403 split).
7. **Multi-tenancy via optional `WithScope(func(ctx) (string, error))`**
   (repo-wide rule), governing management ops only — `Verify` resolves
   tenant from the key record itself. When set: `Create` stamps the derived
   tenant (params must leave `Tenant` empty or matching, else
   `ErrTenantMismatch`); `List` forces `Filter.Tenant`; `Get`/`Revoke`/
   `Rotate` return `ErrNotFound` on tenant mismatch so cross-tenant probing
   cannot distinguish "exists elsewhere" from "doesn't exist". Hook error
   or empty tenant fails the operation (fail-closed). Single-tenant apps
   skip the option — zero ceremony.
8. **Subject-level ownership stays with the caller.** `Filter.Subject`
   makes "list this user's keys" one call; whether a given user may revoke
   a given key is app authz, decided with the record in hand.

## Types

```go
// Key is the stored record — never contains the plaintext secret.
type Key struct {
    ID         id.UUID           // UUIDv7 record id
    Hash       string            // hex SHA-256 of the full plaintext key
    Preview    string            // first 12 plaintext chars, for dashboards
    Name       string            // human label
    Subject    string            // principal the key acts as — never empty
    Tenant     string            // optional tenant
    Scopes     []string
    Meta       map[string]string
    CreatedAt  time.Time
    ExpiresAt  time.Time         // zero = never expires
    LastUsedAt time.Time         // zero = never used
    RevokedAt  time.Time         // zero = active
}

type CreateParams struct {
    Name      string
    Subject   string            // required — ErrSubjectRequired
    Tenant    string            // optional; constrained by the scope hook
    Scopes    []string
    Meta      map[string]string
    ExpiresAt time.Time         // zero = never
}

type Filter struct {
    Subject string // optional: personal keys of one principal
    Tenant  string // optional; forced by the scope hook when configured
}
```

## Store seam

```go
type Store interface {
    Create(ctx context.Context, k Key) error                    // unique by ID and Hash
    Get(ctx context.Context, id id.UUID) (Key, error)           // ErrNotFound
    GetByHash(ctx context.Context, hash string) (Key, error)    // verify path; ErrNotFound
    List(ctx context.Context, f Filter) ([]Key, error)          // newest first
    Revoke(ctx context.Context, id id.UUID, at time.Time) error // sets RevokedAt
    Expire(ctx context.Context, id id.UUID, at time.Time) error // sets ExpiresAt (rotation grace)
    Touch(ctx context.Context, id id.UUID, at time.Time) error  // sets LastUsedAt (best-effort)
}
```

Targeted mutators instead of a generic `Update`: each is one statement in a
SQL driver and cannot clobber concurrent writes. `NewMemoryStore()`
(mutex-guarded map by ID + hash index) ships in-package as the seam-owner
test double; the postgres driver ships in this PR (next section).

## Postgres driver (`auth/apikey/pgstore`)

Follows the established pgstore idiom (`resilience/lock/pgstore`):
`New(pool *pgxpool.Pool) *Store`, embedded goose migration exported as
`Migrations` (rooted via `fs.Sub` so the .sql files sit at fsys root),
applied by the consumer through `data/migration` under its own version
table (`forge_apikey_schema` — never shared, per the migration.Group
collision rule).

```sql
CREATE TABLE forge_api_keys (
    id           uuid PRIMARY KEY,
    hash         text NOT NULL UNIQUE,
    preview      text NOT NULL,
    name         text NOT NULL DEFAULT '',
    subject      text NOT NULL,
    tenant       text NOT NULL DEFAULT '',
    scopes       text[] NOT NULL DEFAULT '{}',
    meta         jsonb NOT NULL DEFAULT '{}',
    created_at   timestamptz NOT NULL,
    expires_at   timestamptz,
    last_used_at timestamptz,
    revoked_at   timestamptz
);
CREATE INDEX forge_api_keys_list_idx ON forge_api_keys (tenant, subject, id DESC);
```

Mapping rules: zero `time.Time` ⇔ SQL `NULL` on the three nullable
columns; `List` orders by `id DESC` (UUIDv7 is time-ordered, so this is
newest-first straight off the index); `GetByHash` hits the unique hash
index — the verify hot path is one indexed point lookup. Filter clauses
are appended only for non-empty `Subject`/`Tenant`.

## Manager API

```go
func New(store Store, opts ...Option) *Manager
// New panics on nil store or invalid prefix — wiring bugs, like guard.New.
// Options: WithPrefix(string), WithScope(func(ctx) (string, error)),
//          WithTouchInterval(time.Duration)

func (m *Manager) Create(ctx context.Context, p CreateParams) (Key, string, error) // record + plaintext
func (m *Manager) Get(ctx context.Context, keyID id.UUID) (Key, error)
func (m *Manager) List(ctx context.Context, f Filter) ([]Key, error)
func (m *Manager) Revoke(ctx context.Context, keyID id.UUID) error
func (m *Manager) Rotate(ctx context.Context, keyID id.UUID, grace time.Duration) (Key, string, error)
func (m *Manager) Verify(ctx context.Context, credential string) (guard.Identity, error)
```

`Manager` implements `guard.Verifier` directly — no adapter type.

## Verify flow (hot path)

1. **Cheap reject** — prefix mismatch, wrong length, or checksum failure ⇒
   `ErrMalformedKey`; no store hit, so credential-stuffing garbage never
   reaches the DB.
2. `store.GetByHash(ctx, sha256hex(credential))` — miss ⇒ `ErrKeyNotFound`.
3. `consttime.StringEqual` on the returned hash (defense-in-depth against a
   buggy store).
4. `RevokedAt` set ⇒ `ErrKeyRevoked`; `ExpiresAt` passed ⇒ `ErrKeyExpired`.
5. Throttled best-effort `Touch` (decision 4).
6. Return `guard.Identity{Subject, Tenant, Scopes, Method: guard.MethodAPIKey,
   Meta: key.Meta + "key_id"/"key_name"}` — key id in Meta so handlers and
   audit logs can tell which key authenticated.

Under `guard.New` every verify error collapses to an opaque 401; the
sentinels exist for metrics and direct callers.

Wiring example (doc.go material):

```go
mgr := apikey.New(store, apikey.WithPrefix("sk_live"))
r.Use(guard.New(mgr, guard.WithExtractors(guard.BearerHeader(), guard.Header("X-API-Key"))))
```

## Errors

`errors.go`, single-line `errors.Is`-matchable sentinels: `ErrNotFound`
(store), `ErrMalformedKey`, `ErrKeyNotFound`, `ErrKeyRevoked`,
`ErrKeyExpired`, `ErrSubjectRequired`, `ErrTenantMismatch`. Nil store or
invalid prefix panics at construction.

## Testing

Black-box (`apikey_test`): key-anatomy round-trip; checksum tamper and
truncation rejection; verify matrix (ok / malformed / unknown / revoked /
expired / empty-subject record); touch throttling via injected stale
`LastUsedAt` (0 / positive / negative intervals); rotation grace overlap
(old+new both verify, old dies after grace); scope-hook enforcement
including cross-tenant `ErrNotFound` and fail-closed hook errors; guard
integration end-to-end (401/200 through `guard.New(mgr)`); memory-store
contract tests. The same store contract suite runs against pgstore as
integration tests, gated on `FORGE_TEST_POSTGRES_DSN` (skip when unset;
run live against ephemeral docker pg16 during development, as in the
resilience-bundle flow) — plus pg-specific cases: NULL⇔zero-time
round-trip, hash-uniqueness violation, `List` ordering/filtering, and
migration apply. Fuzz: `FuzzVerify` over the parse/checksum path.
Benchmarks (repo requirement): keygen, `Verify` hit, malformed reject —
target ~zero allocs on the reject path.

## Anti-scope

No HTTP management handlers; no scope-enforcement middleware; no drivers
beyond pgstore (redis/sqlite not planned); no encryption-at-rest beyond
hashing; no per-key rate limiting; no idle-based expiry.
