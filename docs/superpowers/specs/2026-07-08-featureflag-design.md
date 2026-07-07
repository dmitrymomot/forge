# `ops/featureflag` — design

**Status:** approved design, pre-implementation
**Date:** 2026-07-08
**Tier:** recommended (Wave 1 ops glue)

Standalone feature flags: bool/variant values, deterministic percentage
rollout, token-based allow/deny targeting, pluggable flag storage. No vendor
SDKs, no strategy objects, no rules DSL — a flag is a small serializable data
record, and all evaluation logic lives in this package so every storage
backend behaves identically.

Scales down to a config-only micro-SaaS (one option, zero infrastructure) and
up to a multi-tenant B2B platform with millions of users (Postgres provider +
cache decorator), without call sites changing shape.

## Decisions log (what was chosen and why)

| Decision | Choice | Rationale |
|---|---|---|
| Provider seam | **Store seam**: providers return flag *data*; the client owns all evaluation | Providers are app-side storage (config, memory, Postgres), not external engines. Uniform semantics everywhere; a custom provider is one method. |
| Strategy objects / AND/OR composites | **Rejected** | Strategies are code — they can never be stored in Postgres/YAML (true of the old `pkg/feature` too). Env-gating collapses into per-env YAML; cross-flag composition is `&&`/`\|\|` at the call site; custom predicates are a custom Provider. |
| Targeting | **Token-set allow/deny** (subsumes an earlier separate `Segments` field) | Match set = `{subjectID} ∪ WithIdentity(ctx)`. Namespacing (`role:staff`, `segment:vip`) is the app's convention; the package only intersects string sets. Deny gains segment power (`deny: [segment:self_excluded]`). |
| Value typing | Typed getters with inline defaults, string-typed storage, `core/typeconv` coercion | Call sites never handle errors; env/YAML/SQL all store strings naturally. |
| Subject flow | Context carrier (`WithSubject`) **plus** `For(id, tokens...)` bound evaluator | Carrier for request paths (sanctioned request-scoped read); `For` for jobs/CLIs. |
| Kill switch | Explicit `Enabled` bool, separate from `Rollout` | Admin toggle preserves the rollout %. Zero-value `Flag{}` is disabled → fail-safe. |
| Runtime mutation | Lives on the **memory provider** (`Set`/`Delete`), not the client | The client is immutable after `New`; Postgres providers are runtime-changeable by nature. |
| Caching | **Provider decorator** `Cached(p, ttl, ...)`, scope-aware, singleflight + serve-stale-on-error built in | Client-side per-key caching would leak flags across tenants. Cache cardinality is (scope × flags), never users. |
| Postgres support | **Recipe in doc.go/examples**, no `featureflag/pgstore` subpackage in v1 | Provider is one method (~20 lines); consumers own their schema. Ship a subpackage later on real demand. |
| Rollout granularity | Int percent 0–100 | 1% of 3.5M = 35k users — sane minimum blast radius. Designated extension point: migrate to basis points (×100) if sub-1% canaries are ever needed. |

## Package placement

`ops/featureflag`. Purpose sentence: "runtime feature toggling is an
operations concern" — one domain noun, ops. Product test: complete feature,
wire-and-go. No env-loadable `Config` of its own — flag data arrives through
the app's config struct (`ops/config`) or providers.

## Data model

```go
// Flag is a serializable flag definition. The zero value is disabled (fail-safe).
type Flag struct {
    Value   string   // canonical string form; typed getters coerce via typeconv
    Enabled bool     // kill switch; false → getters return the caller default
    Rollout int      // 0–100 percent of subjects; 100 = everyone
    Allow   []string // tokens: any match with the subject's token set → always on
    Deny    []string // tokens: any match → always off (wins over Allow)
}

type Flags map[string]Flag
```

### YAML shape (embeds in the app's `ops/config` struct)

```yaml
flags:
  dark_mode: true                                  # scalar shorthand
  banner_text: { value: "Summer sale" }
  new_checkout:
    value: true
    rollout: 25
    allow: [role:staff, cus_9f2k]
    deny: [segment:self_excluded]
```

- Scalar shorthand → `Flag{Value: <scalar as string>, Enabled: true, Rollout: 100}`.
- Object form: omitted `enabled` → true; omitted `rollout` → 100.
- **No yaml.v3 import.** `Flag` (and `Flags`) implement the legacy
  func-style unmarshaler `UnmarshalYAML(unmarshal func(any) error) error`,
  which yaml.v3 supports without the implementing package importing it.
  `ops/config` remains yaml.v3's sole consumer.
- Rollout outside 0–100 or an empty key → unmarshal error.

## Provider seam

```go
// Provider supplies flag definitions. Implementations must be safe for
// concurrent use. A miss is (Flag{}, false, nil); errors are treated as
// misses by the client (getter returns the default) and logged.
type Provider interface {
    Flag(ctx context.Context, key string) (Flag, bool, error)
}

// Lister is an optional Provider upgrade for debug/admin visibility.
type Lister interface {
    All(ctx context.Context) (Flags, error)
}
```

The ctx parameter is how multi-tenancy works: a Postgres provider reads the
tenant ID from request context and keys its lookup on (tenant, key). The core
package never learns about tenants.

### Built-in providers

- **static** (internal): built from `WithFlags`/`WithBool`/… options;
  immutable after `New`.
- **`Memory`** (exported): mutex-guarded mutable map for runtime toggles and
  tests. `NewMemory(initial Flags) *Memory` with `Set(key string, f Flag)`,
  `Delete(key string)`, and `All` (implements `Lister`). Powers the
  maintenance-503-gate recipe behind a guarded admin handler.

### `Cached` decorator

```go
func Cached(p Provider, ttl time.Duration, opts ...CacheOption) Provider

func CacheKey(scope func(ctx context.Context) string) CacheOption // default: "" (single-tenant)
```

- Cache entry key = (scope(ctx), flag key); cardinality is scopes × flags —
  independent of user count.
- **Singleflight refresh** (composes `resilience/singleflight`): one loader
  per entry on expiry; concurrent readers share the in-flight result.
- **Serve-stale-on-error**: entries are kept past TTL; if a refresh fails the
  stale value is served and the error logged. The failure mode is
  "yesterday's flags", not "everything off". Entries live for the process
  lifetime (no eviction in v1) — memory is bounded because cardinality is
  scopes × flags, not users.
- Uses `core/clock` for testability.

## Client API

```go
func New(opts ...Option) (*Client, error)

// sources & adjusters — providers are consulted in option order, first hit
// wins; the static set (WithFlags/typed options) occupies its position in
// that order like any other provider
func WithProvider(p Provider) Option
func WithFlags(f Flags) Option                        // merge into the static set
func WithBool(key string, v bool) Option              // static flag, Enabled, Rollout 100
func WithString(key, v string) Option
func WithInt(key string, v int) Option
func WithFloat64(key string, v float64) Option
func WithDuration(key string, v time.Duration) Option
func WithRollout(key string, percent int) Option      // adjust a static entry
func WithAllow(key string, tokens ...string) Option   // adjust a static entry
func WithDeny(key string, tokens ...string) Option    // adjust a static entry
func WithIdentity(fn func(ctx context.Context) []string) Option // subject's extra tokens
func WithLogger(l *slog.Logger) Option                // provider errors & coercion warnings

// typed getters — never error: missing key, disabled flag, provider error,
// coercion failure, or losing the rollout bucket all return def
func (c *Client) Bool(ctx context.Context, key string, def bool) bool
func (c *Client) String(ctx context.Context, key, def string) string
func (c *Client) Int(ctx context.Context, key string, def int) int
func (c *Client) Float64(ctx context.Context, key string, def float64) float64
func (c *Client) Duration(ctx context.Context, key string, def time.Duration) time.Duration

// subject
func WithSubject(ctx context.Context, id string) context.Context // ctxkey carrier
func (c *Client) For(id string, tokens ...string) Evaluator      // ctx-free bound evaluator

// visibility — merges All() across Lister providers + the static set,
// first-hit-wins per key (same precedence as evaluation)
func (c *Client) All(ctx context.Context) (Flags, error)
```

- `Evaluator` exposes the same five getters without `ctx` (jobs, CLIs).
  Explicit `tokens` substitute for the identity resolver.
- Option ordering: `New` applies all *source* options (`WithProvider`,
  `WithFlags`, typed `WithXxx`) in the order given, then all *adjusters*
  (`WithRollout`/`WithAllow`/`WithDeny`) — so interleaving them is harmless.
  Provider precedence is the order of provider-contributing options; the
  static set occupies the position of the **first** static option
  (`WithFlags` or a typed `WithXxx`).
- Adjusters apply to the static set and require the key to already exist
  there (typo guard): unknown key → `New` returns `ErrUnknownFlag`.
- `New` validation errors (single-line sentinels in `errors.go`):
  `ErrEmptyKey`, `ErrInvalidRollout`, `ErrUnknownFlag`.
- The `Client` is immutable after `New`; all internal state is read-only.
  Getters are safe for unlimited concurrency.

## Evaluation pipeline

For `getter(ctx, key, def)`:

1. **Lookup**: ask providers in order; first hit wins. Provider error → log
   WARN (`flag`, `error` attrs), treat as miss. No hit anywhere → `def`.
2. **Enabled**: `!Enabled` → `def`.
3. **Match set**: `{subjectID} ∪ identityResolver(ctx)` (resolver only if
   registered; subject ID only if present).
4. **Deny**: any `Deny` token in the match set → `def`.
5. **Allow**: any `Allow` token in the match set → value (skips rollout).
6. **Rollout**: `Rollout == 100` → value. Otherwise requires a subject ID; no
   ID → `def` (deterministic off-path). Bucket:
   `fnv64a(key + "\x00" + id) % 100 < Rollout`.
7. **Coerce** `Value` via `core/typeconv` to the getter's type; failure →
   `def` + WARN log (`flag`, `value`, `type` attrs).

Rollout properties (documented in doc.go): deterministic across restarts and
instances with zero coordination; raising the percent only ever adds
subjects, never flips existing ones off; the flag key in the hash decorrelates
buckets across flags. Subject IDs should be globally unique (forge `id.Prefix`
style) — bare per-tenant serials would correlate buckets across tenants.

## Contracts the doc.go states

- **Identity resolver is O(1) from context.** It must read pre-loaded request
  state (session/customer already attached by auth middleware), never hit a
  database. Precompute segment tokens at session create/refresh; membership
  changes propagate at session-refresh cadence.
- **Token discipline**: prefixed IDs (`cus_…`) and namespaced tokens
  (`segment:vip`) are naturally disjoint; the package does not police
  collisions.
- **Tokens for populations, overrides for individuals, arrays for handfuls**
  (see Postgres recipe).

## Postgres recipe (doc.go / examples — not a shipped subpackage)

```sql
CREATE TABLE feature_flags (
    tenant_id     text        NOT NULL,
    key           text        NOT NULL,
    value         text        NOT NULL DEFAULT 'true',
    enabled       boolean     NOT NULL DEFAULT true,
    rollout       smallint    NOT NULL DEFAULT 100 CHECK (rollout BETWEEN 0 AND 100),
    allow_tokens  text[]      NOT NULL DEFAULT '{}',
    deny_tokens   text[]      NOT NULL DEFAULT '{}',
    has_overrides boolean     NOT NULL DEFAULT false,
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, key)
);

-- per-user force on/off at scale (thousands+ of individual grants)
CREATE TABLE feature_flag_overrides (
    tenant_id  text    NOT NULL,
    key        text    NOT NULL,
    subject_id text    NOT NULL,
    allow      boolean NOT NULL,
    PRIMARY KEY (tenant_id, key, subject_id)
);
```

Provider sketch: PK lookup on `feature_flags` (rides `Cached`); only when
`has_overrides` (maintained by trigger or admin code) do a PK point-lookup on
`feature_flag_overrides` for the ctx subject and fold the result into the
returned `Flag` (`allow=false` → `Enabled: false`; `allow=true` → append
subject to `Allow`). Hot path for flags without overrides never touches the
overrides table. Single-tenant variant: drop `tenant_id`.

## Usage ladder (each stage adds an option; call sites never change)

```go
// micro-SaaS floor — config only, global on/off + rollout-less flags
flags, _ := featureflag.New(featureflag.WithFlags(cfg.Flags))

// + per-user rollout: auth middleware adds
ctx = featureflag.WithSubject(ctx, user.ID)

// + runtime kill switch, zero infrastructure
mem := featureflag.NewMemory(nil)
flags, _ = featureflag.New(featureflag.WithProvider(mem), featureflag.WithFlags(cfg.Flags))

// + dogfooding: allow: [role:staff] in YAML, plus
featureflag.WithIdentity(func(ctx context.Context) []string { return session.From(ctx).Tokens })

// B2B multi-tenant at millions of users
flags, _ = featureflag.New(
    featureflag.WithProvider(featureflag.Cached(pgFlags, 30*time.Second,
        featureflag.CacheKey(func(ctx context.Context) string { return tenant.From(ctx) }))),
    featureflag.WithFlags(cfg.Flags), // platform defaults
    featureflag.WithIdentity(identityTokens),
)
```

## Dependencies

`core/typeconv` (coercion), `core/ctxkey` (subject carrier), `core/clock`
(cache TTL), `resilience/singleflight` (cache stampede). Stdlib otherwise
(`hash/fnv`, `sync`, `log/slog`). No yaml.v3 (func-style unmarshaler), no pgx
(recipe only).

## Testing (black-box, `featureflag_test`)

- YAML matrix: scalar bool/int/string shorthand; object form; omitted
  enabled/rollout defaults; invalid rollout / empty key errors; embeds in a
  struct loaded by `ops/config` (integration-style fixture).
- Getters: coercion per type; default fallback on missing key, disabled flag,
  malformed value, provider error.
- Options: precedence (provider order, first hit wins; static set position);
  adjusters on unknown key → `ErrUnknownFlag`; invalid rollout → error.
- Pipeline: deny beats allow; allow skips rollout; token-set matching via
  identity resolver and via `For(id, tokens...)`; no-subject rollout → def.
- Rollout: determinism (same id+key stable across clients), monotonicity
  (25%→50% keeps the 25% cohort), rough distribution over ~2k IDs, per-flag
  decorrelation.
- Memory provider: Set/Delete visible to a live client; concurrent
  Set/getters under `-race`; `All`.
- Cached: TTL expiry refresh (mock clock); singleflight (concurrent misses →
  one provider call); serve-stale-on-error; scope isolation (two tenants,
  same key, distinct values).
- Client `All`: merge precedence across Lister providers + static set.

## Anti-scope

- **No `Strategy` interface, no AND/OR composites** — strategies are code and
  can't be stored; composition is Go at the call site; custom predicates are a
  custom Provider.
- **No group/attribute context carriers** — identity tokens cover it; group
  semantics belong to the app (or future `auth/rbac`).
- **No vendor SDK adapters** (explicit user decision — standalone).
- **No rules DSL / no per-flag expression language.**
- **No push invalidation** — TTL staleness bounds are the contract; a
  LISTEN/NOTIFY invalidator can ride `realtime/fanout`'s pgbus later if
  demanded.
- **No `featureflag/pgstore` subpackage in v1** — recipe; promote on demand.
- **No admin UI / management API** — `Memory.Set` + consumer SQL are the
  levers; flag-change auditing belongs to `ops/auditlog`.
- **No sub-1% rollout granularity** — designated extension: basis-point
  migration (×100).

## docs/packages.md edit (part of this work)

Replace the current entry:

> **`featureflag`** — recommended. Bool/variant flags via a `Provider` seam;
> static + env providers in core; vendor SDKs stay consumer-side.

with:

> **`featureflag`** — recommended. Standalone flags as serializable data
> records (`enabled → deny → allow → rollout` pipeline, token-set targeting,
> FNV subject bucketing): typed getters with defaults, config-fed/YAML +
> code-option + mutable memory sources behind a one-method store `Provider`
> seam (ctx-scoped for multi-tenancy), scope-aware `Cached` decorator
> (singleflight, serve-stale). Postgres provider is a doc.go recipe. No
> strategy objects, no vendor SDKs.

Also remove the "maintenance 503 gate" item from Recipes owed once the doc.go
ships it (it is this package's doc example).
