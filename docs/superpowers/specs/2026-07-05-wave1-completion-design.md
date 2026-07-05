# Wave 1 Completion Bundle — Design Spec

> Status: approved 2026-07-05. Completes the build-order **Wave 1 (Unblockers)**
> in `docs/packages.md`. The shipped-package API additions portion of Wave 1
> landed in PR #31; this bundle adds the four *new* packages that finish the
> wave: `web/httpclient`, `resilience/ratelimit` (+`ratelimit/redisstore`),
> `ops/envconfig`, `ops/health`, plus one shared unblocker
> (`structfields.SetString`).

## Goal

Ship the four Wave 1 packages as one bundled PR via `subagent-driven-dev`. Each
is `core` tier, independently buildable on already-shipped dependencies, and
unblocks large parts of Wave 2+:

- `httpclient` — the outbound transport under `captcha`, `oauthclient`,
  `comms/*`, `llm`.
- `ratelimit` — establishes the **counter `Store` seam** shared later by
  `quota` and `lockout`.
- `envconfig` — env→Config loading that every shipped `Config` (`logger`,
  `httpserver`, …) is already tagged for.
- `health` — liveness/readiness + drain gating, needed by `appmain` and any
  real deployment.

All four follow the design DNA in `docs/packages.md`: `New(...Option)` (never
builders), env-loadable `Config`+`DefaultConfig`+`Validate` where stateful,
`errors.Is`-matchable single-line sentinels, `doc.go` with a runnable example,
real deps isolated in driver subpackages, black-box tests only.

## Scope

**In:** the four packages above, their driver subpackages
(`ratelimit/redisstore`, `health/checks`), and one shared shipped-package
addition (`core/structfields.SetString`).

**Out (explicit non-goals):**

- Config *watching*/hot-reload — that is icebox `ops/configwatch`.
- Token-bucket / GCRA / fixed-window rate-limit algorithms — sliding-window
  counter is the only algorithm shipped now (the seam allows adding others
  later without a breaking change).
- Tracing propagation internals — `httpclient` propagates whatever headers the
  caller's extractor returns; the OTel `traceparent` *source* is icebox
  `ops/tracing`.
- Changing `httpclient.Do` to return a typed error — it returns the stdlib
  `*http.Client`; problem+json surfacing is a documented companion call.

---

## 0. Shared unblocker — `core/structfields.SetString`

`envconfig` must turn an env *string* into a typed struct field. `typeconv` is
documented "converts strings to Go scalars **without reflection**" (generic
`Parse[T]`), so it cannot host a `Kind`-dispatched parser. Per the design DNA
("no reflection except the one sanctioned helper, `structfields`"), the dispatch
lives in `structfields`, where it is also reusable by `view/form`, `ops/cli`,
and `web/request` later.

```go
// SetString parses raw into f's field according to its kind and assigns it,
// returning an ErrNotSettable/parse error on failure. Supported kinds: string,
// bool, all int/uint widths, float32/64, time.Duration, time.Time (RFC3339),
// and []T of the above (comma-separated). Unsupported kinds return ErrUnsupportedKind.
func SetString(f Field, raw string) error
```

- Implemented as a `switch f.Value.Kind()` (plus special-casing `time.Duration`
  and `time.Time` by concrete type) that calls the matching `typeconv.ParseX`
  and then `f.Set(parsed)`. No new reflection machinery beyond what `Walk`
  already exposes.
- New sentinel `ErrUnsupportedKind` in `structfields/errors.go`.
- Slices use `typeconv.ParseSlice[T]` with `","` as the separator.
- TDD'd and merged conceptually first; the other three packages depend only on
  it (via `envconfig`).

**Test expectations (black-box):** round-trips each supported kind; pointer
fields are allocated and set; type mismatch and unsupported kind return the
right sentinel; value-struct (non-pointer) `Field.Set` still returns
`ErrNotSettable`; empty `raw` leaves defaults for optional fields (caller
decides — `SetString` on empty string parses "" per kind, so `envconfig` skips
calling it when the env var is absent).

---

## 1. `ops/envconfig`

Populate `env`-tagged Config structs from the environment plus an optional
`.env` file. Boot-time only.

### Public API

```go
func Load[T any](opts ...Option) (T, error)   // parse and return a fresh T
func Populate(dst any, opts ...Option) error  // fill an existing non-nil *struct

type Option func(*config)
func WithPrefix(prefix string) Option                       // e.g. "APP_"
func WithDotenv(paths ...string) Option                     // load these .env files first
func WithLookup(fn func(key string) (string, bool)) Option  // override os.LookupEnv (test seam)

// Deployment profile.
type Profile string
const (
    ProfileDev     Profile = "dev"
    ProfileTest    Profile = "test"
    ProfileStaging Profile = "staging"
    ProfileProd    Profile = "prod"
)
func (p Profile) IsDev() bool
func (p Profile) IsTest() bool
func (p Profile) IsStaging() bool
func (p Profile) IsProd() bool
func ResolveProfile(opts ...Option) Profile  // reads APP_ENV then ENV; unknown/empty -> ProfileDev
```

### Semantics

- **Field walk:** `structfields.Walk(dst, "env", ...)`; each field's env key is
  `prefix + tag.Name`. Missing `env` tag → field skipped.
- **Value resolution order per key:** real environment (via the `WithLookup`
  func, default `os.LookupEnv`) wins; `.env` values are loaded into a fallback
  map consulted only when the env var is absent. `.env` is parsed by an internal
  `KEY=VALUE` reader (comments `#`, blank lines, optional `export ` prefix,
  single/double-quoted values with escape handling) — no godotenv dependency.
- **Assignment:** present values go through `structfields.SetString`; absent
  values fall back to the `default:"..."` struct tag (also via `SetString`);
  fields tagged `required` (an option in the `env` tag, `env:"PORT,required"`)
  with no value and no default cause `Load`/`Populate` to fail.
- **Nested structs:** recurse; a nested field may carry
  `envconfig:"prefix=DB_"` to compose an additional key prefix. Anonymous
  embedded structs recurse with the parent prefix.
- **Validation:** if `dst` (or `*T`) implements `interface{ Validate() error }`,
  `envconfig` calls it after populating and joins any error. This is how it
  cooperates with every shipped `Config.Validate`.

### Errors

`ErrNotStruct` (wrap of the structfields sentinel is fine), `ErrRequiredMissing`
(carries the key name), `ErrParse` (carries key + kind), `ErrDotenv` (file read
failure). All single-line, `errors.Is`-matchable. Multiple field errors are
`errors.Join`-ed so one call reports every problem.

### Config / DNA note

`envconfig` is itself optionless-stateful (it *is* the loader), so it uses the
free-func + `Option` idiom, not a `New`/`Config` struct.

### Tests (black-box)

Populate a multi-field struct from a `WithLookup` map (no real env); `.env`
fallback and precedence; prefix and nested prefix composition; `required`
missing → `ErrRequiredMissing`; bad value → `ErrParse`; `Validate()` hook
invoked and its error surfaced; `ResolveProfile` mapping and predicates;
round-trip a real shipped Config shape (e.g. a struct mirroring
`logger.Config`) to prove tag compatibility.

---

## 2. `ops/health`

Liveness + readiness in one package, with per-check timeout/TTL, drain gating,
and heartbeat kickers.

### Public API

```go
func New(opts ...Option) *Health

func (h *Health) Liveness(name string, c Check, opts ...CheckOption)
func (h *Health) Readiness(name string, c Check, opts ...CheckOption)

func (h *Health) LivenessHandler() http.Handler   // GET /livez
func (h *Health) ReadinessHandler() http.Handler  // GET /readyz

func (h *Health) SetReady(ready bool)
func (h *Health) Heartbeat(name string, maxAge time.Duration) (kick func())
func (h *Health) DrainService(grace time.Duration) supervisor.Service

type Check func(ctx context.Context) error

type Option func(*config)
func WithDefaultTimeout(d time.Duration) Option   // default per-check ctx deadline
func WithDefaultCacheTTL(d time.Duration) Option  // default result cache window
func WithLogger(l *slog.Logger) Option

type CheckOption func(*checkConfig)
func WithCritical() CheckOption                  // failure flips readiness to 503
func WithTimeout(d time.Duration) CheckOption    // per-check override of default timeout
func WithCacheTTL(d time.Duration) CheckOption   // per-check override of default cache TTL
```

### Semantics

- **Two registries.** `/livez` reflects only liveness checks + heartbeat
  freshness; a hung background loop that stops calling its `kick()` makes
  `/livez` fail after `maxAge`. `/readyz` reflects readiness checks AND the
  `SetReady` flag.
- **Result shape.** Handlers return `200`/`503` with a JSON body:
  `{"status":"ok|degraded|unavailable","checks":{"name":{"ok":bool,"error":"…","critical":bool}}}`.
  Non-critical readiness failure → `status:"degraded"` but still `200`; any
  critical failure or `SetReady(false)` → `503`.
- **Per-check execution.** Each check runs under its timeout; results are cached
  for the TTL so a scrape storm can't hammer the database. Checks run
  concurrently on scrape.
- **Drain gating.** `DrainService(grace)` returns a `supervisor.Service`
  (`Name()="health-drain"`, blocking `Run`). On ctx cancel it calls
  `SetReady(false)`, sleeps `grace` (so the load balancer observes `/readyz`
  going 503 and deregisters), then returns nil. Registered **before**
  `httpserver` in `supervisor.Run` so readiness flips before the server drains.
  Documented ordering in `doc.go`.

### `health/checks` subpackage

Driver-free adapters so concrete checks don't pull deps into core `health`:

```go
func Ping(p interface{ Ping(ctx context.Context) error }) health.Check
func HTTPGet(client *http.Client, url string) health.Check   // 2xx = healthy
```

`data/postgres` and `data/redis` clients satisfy the `Ping` shape; `HTTPGet`
composes the new `httpclient`. No imports of `data/*` from `health`.

### Errors

`health` checks return caller errors verbatim in the JSON; the package needs no
exported error sentinels beyond what handlers encode.

### Tests (black-box)

`/livez` 200 with healthy checks, 503 when a liveness check errors; heartbeat
staleness flips `/livez`; `/readyz` 503 on `SetReady(false)`; critical vs
non-critical readiness distinction (503 vs degraded-200); per-check timeout
surfaces as failure not hang; result caching (a slow check invoked once within
TTL — assert call count); `DrainService` flips readiness on ctx cancel and
returns after `grace` (use `clock.Mock` or a tiny real grace); `checks.Ping`
maps a stub pinger's error.

---

## 3. `resilience/ratelimit` (+ `ratelimit/redisstore`)

Keyed sliding-window-counter limiter over the **counter `Store` seam** — the
second framework store seam (byte-KV is the first, owned by `cache`).

### The counter Store seam (the important artifact)

```go
// Store is the windowed atomic-counter seam. ratelimit owns it; quota and
// lockout consume the same contract. Distinct from cache.Store (byte-KV):
// counters need atomic increment-with-TTL, which Get/Set cannot express
// race-free.
type Store interface {
    // Incr atomically adds delta to key's counter and returns the new value.
    // If the key is created by this call, its TTL is set to ttl; Incr never
    // extends the TTL of an existing key (fixed expiry per window).
    Incr(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error)
    // Get returns the current counter value, or 0 if the key is absent/expired.
    Get(ctx context.Context, key string) (int64, error)
    // Reset deletes the counter for key.
    Reset(ctx context.Context, key string) error
    Close() error
}

func NewMemoryStore(opts ...MemoryOption) Store   // sweeps expired keys; caller Closes
```

### Limiter

```go
func New(store Store, opts ...Option) *Limiter

func (l *Limiter) Allow(ctx context.Context, key string) (Result, error)
func (l *Limiter) Middleware(key KeyFunc, opts ...MiddlewareOption) middleware.Middleware

type Result struct {
    Allowed    bool
    Limit      int64
    Remaining  int64
    Reset      time.Time     // when the window rolls
    RetryAfter time.Duration // 0 when Allowed
}

type KeyFunc func(*http.Request) string   // e.g. clientip-based

type Option func(*config)
func WithLimit(n int64, per time.Duration) Option   // required; n requests per window
func WithClock(c clock.Clock) Option                 // testability

var ErrLimited = errors.New("ratelimit: limit exceeded")
```

### Algorithm — sliding-window counter

For window size `per` and current time `t`:

- `curr = floor(t / per)`, `prev = curr - 1`.
- On `Allow`: `c = Incr(key:curr, 1, ttl=2*per)`; `p = Get(key:prev)`.
- Weighted estimate `est = c + p * (1 - (t mod per)/per)`.
- Allowed iff `est <= n`. On rejection the counter is still incremented but the
  request is denied (standard sliding-window behavior); `RetryAfter` is the time
  until `est` would drop below `n`, bounded by the window roll.
- `Reset` = start of next window.

The two-key composition means the `Store` primitive stays a plain
`Incr`/`Get` — `quota` (calendar/rolling windows) and `lockout` (failure counts
with delay) reuse it unchanged.

### HTTP middleware

Emits IETF draft headers on every response: `RateLimit-Limit`,
`RateLimit-Remaining`, `RateLimit-Reset` (delta-seconds). On limit: `429` +
`Retry-After` + `RateLimit-*`, body via a configurable responder (default
plain 429; a `problem`-based responder is a documented option so API callers get
problem+json). `MiddlewareOption`: `WithResponder`, `WithHeadersDisabled`.

### `ratelimit/redisstore`

Implements `Store` over the caller's `data/redis` client. `Incr` = a Lua script
doing `INCRBY` then `PEXPIRE … NX`-equivalent (set TTL only when the key was
just created — check `INCRBY` result == delta, or use `PTTL < 0` guard inside
the script) to guarantee fixed per-window expiry. `Get` = `GET` (nil→0),
`Reset` = `DEL`, `Close` = no-op (client lifecycle is the caller's).

### Tests (black-box)

Memory store: `Incr` atomicity under concurrency (race test), TTL expiry, `Get`
absent→0, `Reset`. Limiter: allows up to `n` then `ErrLimited`; sliding weight
prevents 2× edge burst (drive `clock.Mock` across a boundary and assert the
denied request mid-window); `Result` fields correct; `Remaining` never
negative. Middleware: header values, 429 + `Retry-After`, custom responder,
`KeyFunc` isolation between keys. `redisstore` is tested behind the same
black-box Store contract test (shared test against a real/miniredis-free
`data/redis` — if no Redis in CI, gate with the existing `data/redis` test
pattern).

---

## 4. `web/httpclient`

Resilient outbound `*http.Client` (the stdlib type) via a RoundTripper stack
composing the shipped `retry` + `circuitbreaker` + `backoff`.

### Public API

```go
func New(opts ...Option) *http.Client

type Option func(*config)
func WithTimeout(d time.Duration) Option            // overall client timeout
func WithPerAttemptTimeout(d time.Duration) Option  // per try, via ctx
func WithRetry(opts ...retry.Option) Option         // tune attempts/backoff
func WithRetryMethods(methods ...string) Option     // default {GET,HEAD,PUT,DELETE,OPTIONS}
func WithBreakerGroup(opts ...circuitbreaker.GroupOption) Option  // per-host breaker; OFF unless set
func WithBaseTransport(rt http.RoundTripper) Option // default http.DefaultTransport clone
func WithBefore(fn func(*http.Request)) Option
func WithAfter(fn func(*http.Request, *http.Response)) Option
func WithContextHeaders(fn func(context.Context) http.Header) Option  // propagation extractor
func WithHeader(key, value string) Option
func WithUserAgent(ua string) Option
```

### Transport stack (outer → inner)

1. **hooks + static headers + propagation** — `WithBefore`, `WithHeader`,
   `WithUserAgent`, and `WithContextHeaders(fn)` (copies e.g. `X-Request-ID`,
   `traceparent` from ctx onto the outbound request); `WithAfter` runs on the
   response.
2. **retry** — `retry.Do` around the attempt. Default classifier retries on
   network errors, `429`, and `5xx`, **only** for idempotent methods (the
   `WithRetryMethods` set; POST excluded by default to avoid double-submit).
   Default `retry.WithMaxAttempts(3)` with jittered exponential backoff. A
   `Retry-After` header on `429`/`503` is surfaced as a `RetryAfterError` so the
   shipped `retry` raises the delay floor automatically.
3. **per-attempt timeout** — `WithPerAttemptTimeout` via `context.WithTimeout`
   per try.
4. **circuit breaker (opt-in)** — when `WithBreakerGroup` is set, each attempt
   runs inside `circuitbreaker.Group.Do(ctx, req.URL.Host, …)`. The breaker's
   open error already implements `RetryAfter()`, so retry and breaker cooperate.
   Off by default (Fork C).
5. **base transport** — `WithBaseTransport` or a cloned `http.DefaultTransport`.

Defaults with no options: retry on (3 attempts, idempotent methods), timeouts
unset (caller's ctx governs), breaker off.

### Propagation & problem surfacing

- `httpclient` takes **no** hard dependency on `web/requestid`. `doc.go` shows
  the one-liner extractor: `WithContextHeaders(func(ctx) http.Header { h :=
  http.Header{}; if id, ok := requestid.From(ctx); ok { h.Set("X-Request-ID",
  id) }; return h })`.
- Problem+json surfacing stays a companion call — `doc.go` shows
  `problem.Decode(resp)` on non-2xx — because `New` returns the stdlib
  `*http.Client` and we will not change `Do`'s signature.

### Errors

No new sentinels: transient failures surface as the stdlib transport/`retry`
errors; `retry.Permanent` short-circuits non-retryable ones. The breaker's
`circuitbreaker.ErrOpen` surfaces through when tripped.

### Tests (black-box)

Use `testkit`-style `httptest.Server` stubs (webtest not yet shipped — use
stdlib `net/http/httptest`): retries a flaky 503-then-200 and succeeds; does
**not** retry POST by default; honors `Retry-After` (assert elapsed floor with a
mock/short clock); per-attempt timeout aborts a slow attempt and retries;
`WithContextHeaders` propagates `X-Request-ID`; `WithBefore/After` invoked;
breaker opt-in trips after N host failures and fast-fails with `ErrOpen`, and
does NOT engage when `WithBreakerGroup` is absent; `problem.Decode` companion
maps a 422 problem+json body.

---

## Cross-cutting

- **Idioms:** `envconfig` = free-func + Option; `health`/`ratelimit`/
  `httpclient` = `New(...Option)`. No builders anywhere.
- **Anatomy per package:** `doc.go` (runnable example), `errors.go`
  (single-line sentinels), `options.go` (`type Option func(*config)`), impl,
  black-box `_test.go` in `package X_test`.
- **Dependencies added:** none new — all four compose stdlib + already-isolated
  deps (`data/redis` for `redisstore`, the shipped resilience trio for
  `httpclient`). `x/crypto` etc. untouched.
- **Test doubles live with the seam owner:** `ratelimit.NewMemoryStore` is the
  in-memory counter double; there is no central fakes package.
- **`just fmt ./pkg/...` + `just lint`** run clean before the PR (use the
  package-path form of `just fmt`, not single-file, per the known betteralign
  quirk); `modernize` run before done; race tests green.

## Build order (within the bundle)

1. `core/structfields.SetString` (unblocker leaf).
2. In parallel: `ops/envconfig` (needs #1), `ops/health` (+`health/checks`),
   `resilience/ratelimit` (+`ratelimit/redisstore`), `web/httpclient`.
3. Update `docs/packages.md`: move the four from *planned* to *shipped*, bump
   the shipped-count line, drop the Wave 1 rows from build order.

One bundled PR via `subagent-driven-dev`.

## Risks / notes

- **redisstore fixed-TTL-per-window** must be atomic (Lua) or a concurrent
  first-incr race re-arms the TTL and stretches a window. Called out in the plan
  as a must-test.
- **Sliding-window `RetryAfter`** math is the subtle part of `ratelimit`; the
  boundary-crossing test with a mock clock is the guard.
- **POST-retry default off** is a deliberate safety choice (no silent
  double-submit); `WithRetryMethods` is the documented escape hatch.
- `health/checks.HTTPGet` depends on `httpclient`, so it is authored after
  `httpclient` even though both land in the same PR.
