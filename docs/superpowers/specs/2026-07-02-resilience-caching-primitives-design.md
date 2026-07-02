# Resilience & Caching Primitives — Design

**Date:** 2026-07-02
**Status:** Approved (design), pending implementation plan
**Bundle:** `backoff`, `retry`, `singleflight`, `parallel`, `circuitbreaker`, `cache` (+ `cache/redis`)

## Context & motivation

These packages are the resilience/async building blocks that the rest of the recommended
tier sits on — the substrate under the not-yet-built `httpclient`, `jobqueue`, `lock`,
`ratelimit`, `otp`, `idempotency`, and `sessionstore` packages.

Five of them (`backoff`, `retry`, `singleflight`, `parallel`, `circuitbreaker`) are
near-zero-dependency leaves: stdlib, or stdlib + the shipped `clock`. The sixth, `cache`,
is a **provider-backed package** — a generic `Cache[V]` interface with built-in **memory**
and **Redis** adapters — whose core adds only `clock` + `singleflight`, and whose Redis
adapter is isolated in a `cache/redis` subpackage. **No new `go.mod` dependencies:**
go-redis is already present via the shipped `redis` package.

Two of these (`backoff`, `retry`) are **core-tier** in the roadmap; they are folded into
this bundle because they complete the resilience family.

The `cache` design is informed by the prior forge `pkg/cache` implementation
(commit `0b68f05`): the `Cache[V]` interface, memory + Redis adapters, standalone
`GetOrSet` stampede protection, `Marshaler` seam, key-prefix isolation, and eviction
callbacks all carry forward, re-expressed in v2 conventions.

## Design principles (all six packages)

1. **Instances over globals.** Every constructor returns an isolated instance that owns
   its own state. **Zero package-level mutable state, no singletons.**
2. **Isolation.** Two instances never interfere. In-memory caches hold **separate maps**
   (never shared buckets); Redis caches isolate via a **required key `Prefix`**. Two
   `cache.NewMemory` calls in the same process are fully independent stores.
3. **Usability first.** Sane defaults, one-line read-through (`GetOrSet`), JSON marshaling
   by default for Redis, minimal construction boilerplate. Efficiency is necessary but not
   sufficient — the API must be pleasant to use without a wrapper.

## Conventions applied (existing forge DNA — not re-litigated)

- `type Option func(*config)` with an **unexported** `config`; **no builders**.
- **Options-only** configuration. These are code-configured library primitives, not
  deploy-time services — no exported `Config`/env tags. (The `cache/redis` adapter may
  grow an env-loadable `Config` later following the `redis` package pattern; not in v1.)
- `errors.go` with `errors.Is`-matchable single-line sentinels.
- `doc.go` with a runnable example.
- `clock.Clock` injection via `WithClock` on packages that read the current time
  (`circuitbreaker`, `cache` memory adapter) → deterministic tests with `clock.NewMock`.
  The shipped `clock.Clock` is `Now()`-only (no `Sleep`/`After`), so `retry` — which must
  *wait* — sleeps via a real ctx-aware timer instead (see Deviation #3).
- **Black-box tests only** (`package <name>_test`). Flat top-level packages; Go 1.26
  idioms (`new(expr)`; run `modernize` + `just fmt`/`just lint` before done).

## Scope decisions (agreed)

In scope for v1:

- `parallel` **functional `Map`/`ForEach`** helpers and an opt-in **`WithCollectAll()`**
  aggregation mode.
- `circuitbreaker` **`OnStateChange`** callback.
- `cache` **memory + Redis adapters**, **`GetOrSet`** stampede protection, **LRU eviction**
  (`WithMaxEntries`), **optional background janitor** (`WithCleanupInterval` + `Close`),
  **eviction callback** (`WithOnEvict`), **`Marshaler`** seam (JSON default), **prefix
  isolation** for Redis.

Out of scope for v1 (YAGNI, addable later):

- `backoff` decorrelated-jitter / pluggable RNG seam (jitter uses `math/rand/v2`).
- A byte-level `kv` Store seam for other stateful packages (`lock`/`otp`/…). Distinct from
  `cache`; built when those packages are.
- Env-loadable `Config` for the cache adapters.

## Deviations from the roadmap sketch (deliberate)

1. **`Backoff` is stateless.** The roadmap interface had both `Next(attempt int)` and
   `Reset()`, which contradict. The interface is `Next(attempt int) time.Duration` only
   (goroutine-safe); `retry` owns the attempt counter.
2. **`parallel` is fail-fast by default, aggregation opt-in.** `Wait` returns the *first*
   error and the derived context cancels the siblings (errgroup semantics);
   `WithCollectAll()` switches to run-all-then-`errors.Join`.
3. **`retry` takes no clock and is instance-based.** The shipped `clock.Clock` is
   `Now()`-only, so a mock clock cannot drive retry's waits; it sleeps via a context-aware
   real `time.Timer`. Shaped as `New(...Option) *Retrier` (configure once, reuse) plus a
   convenience `retry.Do(...)`. Drops the roadmap's `clock` dependency — depends only on
   `backoff`.
4. **`cache` is a provider-backed package, not an in-memory-only `ttlcache`.** Renamed from
   the roadmap's `ttlcache`; expanded to a generic `Cache[V]` interface with memory +
   `cache/redis` adapters, **string keys** (Redis-native), and `GetOrSet` stampede
   protection — per the prior `pkg/cache` implementation and the requirement to support
   store providers. Consequently `singleflight` stays **string-keyed** (roadmap original);
   the earlier "generalize to `Group[K,V]`" idea is dropped since `cache` keys by string.

---

## Package specifications

### `backoff`

Pure, stateless delay strategies. Deps: `time`, `math`, `math/rand/v2`.

```go
type Backoff interface {
    Next(attempt int) time.Duration // attempt ≥ 1
}

func Constant(d time.Duration) Backoff
func Exponential(base, max time.Duration, opts ...Option) Backoff
//   WithMultiplier(f float64)     // growth factor, default 2.0
//   WithJitter(fraction float64)  // 0..1; randomizes the delay by ±fraction
```

- `Exponential` computes `base * multiplier^(attempt-1)`, capped at `max`.
- Constructors clamp degenerate input (`base` ≥ 1ns, `max` ≥ `base`); no error returns.
- Jitter uses `math/rand/v2` (auto-seeded, concurrency-safe).
- **Errors:** none. **Tests:** exact for `Constant`/un-jittered; jittered within bounds.

### `retry`

Execute a function with retries. Instance-based. Deps: `backoff`; `context`, `time`,
`errors`.

```go
type Retrier struct { ... }

func New(opts ...Option) *Retrier
func (r *Retrier) Do(ctx context.Context, fn func(ctx context.Context) error) error

// Convenience one-shot (= New(opts...).Do(ctx, fn)); no global state:
func Do(ctx context.Context, fn func(ctx context.Context) error, opts ...Option) error

func Permanent(err error) error // wrap to stop retrying immediately

// Options:
//   WithMaxAttempts(n int)          // default 3
//   WithBackoff(b backoff.Backoff)  // default Exponential(100ms, 10s, WithJitter(0.5))
//   WithRetryIf(func(error) bool)   // default: retry all non-Permanent errors
```

- Loops up to `maxAttempts`. `Permanent`-wrapped → return unwrapped immediately;
  `RetryIf==false` → return; else sleep `backoff.Next(attempt)` and retry.
- Sleeps use a context-aware real `time.Timer`; on cancellation returns `ctx.Err()`.
- `Permanent` detected via `errors.As` on an internal `permanentError` (not a package var).
- **Tests:** tiny backoff durations (`backoff.Constant(time.Millisecond)`); assert attempt
  counts, `Permanent` short-circuit, `RetryIf` gating, ctx cancellation. Delay math is
  covered by `backoff`'s exact tests, so no fake clock needed.

### `singleflight`

Generic request coalescing (cache-stampede protection). String-keyed. Deps: `context`,
`sync`.

```go
type Group[V any] struct { /* zero-value usable; no New() */ }

func (g *Group[V]) Do(ctx context.Context, key string,
    fn func(ctx context.Context) (V, error)) (v V, shared bool, err error)
func (g *Group[V]) Forget(key string)
```

- Zero-value usable (lazy map init under mutex), like `sync.Once`. Each `Group` is
  independent — no shared state across instances.
- The in-flight `fn` runs under a **cancellation-detached context**
  (`context.WithoutCancel` of the leader's ctx) so one caller cancelling does not poison
  the shared execution; each caller's own `ctx` still bounds *its* wait.
- `shared` reports whether the result was shared with concurrent callers.
- Consumed internally by `cache.GetOrSet` (as `Group[any]`). **Errors:** none.
- **Tests:** N goroutines on one key behind a sync barrier → `fn` ran once, all got the
  same value with `shared==true`; `Forget` re-arms.

### `parallel`

Bounded concurrent execution. Fail-fast by default, opt-in aggregation. Deps: `context`,
`sync`, `errors`.

```go
type Group struct { ... }

func New(ctx context.Context, opts ...Option) (*Group, context.Context)
func (g *Group) Go(fn func(ctx context.Context) error)
func (g *Group) Wait() error

// Options:
//   WithLimit(n int)    // max concurrent; n ≤ 0 = unbounded
//   WithCollectAll()    // run all, no sibling cancellation; Wait returns errors.Join(...)

// Functional helpers (fail-fast, order-preserving results):
func ForEach[T any](ctx context.Context, items []T, limit int,
    fn func(ctx context.Context, item T) error) error
func Map[T, U any](ctx context.Context, items []T, limit int,
    fn func(ctx context.Context, item T) (U, error)) ([]U, error)
```

- Default: `New`'s derived context cancels on the **first** error; `Wait` returns it.
- `WithCollectAll()`: no cancellation on error; every func runs to completion; `Wait`
  returns `errors.Join` of all non-nil errors (nil if all succeed).
- `Map` preserves order (`result[i]` ↔ `items[i]`); fail-fast; on error returns the first
  error and a nil slice. `limit ≤ 0` = unbounded.
- **Errors:** none exported. **Tests:** bounded concurrency (peak in-flight ≤ limit via
  atomic counter), first-error cancellation, `WithCollectAll` joins all, `Map` ordering.

### `circuitbreaker`

Closed/open/half-open breaker. Instance-based. Deps: `clock`; `sync`, `time`, `errors`.

```go
type State int // Closed, Open, HalfOpen; fmt.Stringer
var ErrOpen = errors.New("circuitbreaker: circuit open")

type Breaker struct { ... }
func New(opts ...Option) *Breaker
func (b *Breaker) Do(ctx context.Context, fn func(ctx context.Context) error) error
func (b *Breaker) State() State

// Options:
//   WithFailureThreshold(n int)              // consecutive failures to open; default 5
//   WithOpenTimeout(d time.Duration)         // before half-open probe; default 30s
//   WithHalfOpenMax(n int)                   // concurrent probes; default 1
//   WithOnStateChange(func(from, to State))  // transition hook
//   WithClock(c clock.Clock)
```

- Closed → counts consecutive failures, `≥ threshold` opens. Open → `Do` returns `ErrOpen`
  without calling `fn`; after `openTimeout` → half-open. Half-open → up to `halfOpenMax`
  probes; success closes (resets), failure re-opens.
- **Errors:** `ErrOpen`. **Tests:** `clock.NewMock` + `Advance` drive the open→half-open
  timeout; assert threshold tripping, fast-fail, probe gating, `OnStateChange`.

### `cache` (+ `cache/redis`)

Generic cache with pluggable providers. Core deps: `clock`, `singleflight`; `context`,
`sync`, `time`, `container/list`, `encoding/json`. Subpackage `cache/redis` deps: forge
`redis` + go-redis (isolated).

```go
// package cache
type Cache[V any] interface {
    Get(ctx context.Context, key string) (V, error)               // ErrNotFound on miss
    Set(ctx context.Context, key string, value V, ttl time.Duration) error
    Delete(ctx context.Context, key string) error
    Has(ctx context.Context, key string) (bool, error)
    Clear(ctx context.Context) error                               // scoped to this instance
    Close() error
}

// TTL semantics for Set: positive = expire after d; zero = configured DefaultTTL;
// negative = never expires.

type Marshaler[V any] interface {          // storage backends needing bytes (Redis)
    Marshal(v V) ([]byte, error)
    Unmarshal(data []byte) (V, error)
}                                          // JSON default; override via WithMarshaler

// Memory adapter — zero-marshal, own map per instance (isolation)
func NewMemory[V any](opts ...Option) Cache[V]
//   WithDefaultTTL(d)         // default 5m
//   WithMaxEntries(n)         // 0 = unbounded; >0 = LRU via container/list
//   WithCleanupInterval(d)    // >0 starts a janitor goroutine; Close() stops it; 0 = lazy only
//   WithOnEvict(func(key string, v V))  // fired on LRU evict / TTL cleanup / Delete / Clear
//   WithClock(c clock.Clock)

// Read-through with singleflight stampede protection (standalone: Go methods can't add
// a type param; takes the instance, no globals):
func GetOrSet[V any](ctx context.Context, c Cache[V], key string,
    fn func(ctx context.Context) (V, time.Duration, error)) (V, error)
```

```go
// package cache/redis — isolated adapter; not pulled in by memory-only users
func New[V any](client goredis.UniversalClient, opts ...Option) cache.Cache[V]
//   WithPrefix(p string)      // REQUIRED — namespaces keys; Clear() only deletes this prefix
//   WithDefaultTTL(d)
//   WithMarshaler(m cache.Marshaler[V])  // default JSON
```

- **Isolation:** each `NewMemory` owns its map; each `cache/redis` instance requires a
  `Prefix`. `Clear()` on Redis uses `SCAN`+`DEL` over the prefix — **never `FLUSHDB`**.
- **`GetOrSet`:** fast-path `Get`; on miss, dedups concurrent loads via a per-instance
  `singleflight.Group[any]` (through a small non-generic `deduper` interface the adapters
  implement); best-effort `Set` with the returned TTL; load errors are **not** cached.
- **Memory expiry:** lazy on `Get` (checks `clock.Now()`), plus the optional janitor
  goroutine. Without `WithMaxEntries` or `WithCleanupInterval`, expired-but-unread entries
  linger until the process exits — documented; set one of them for long-lived caches.
- **Errors:** `ErrNotFound`, `ErrClosed`, `ErrMarshal`, `ErrUnmarshal`.
- **Tests:** memory — `clock.NewMock` drives lazy expiry, LRU order, `GetOrSet`
  single-flight, `WithOnEvict`, idempotent `Close`, and **cross-instance isolation** (two
  caches don't see each other's keys). `cache/redis` — key-prefixing and marshaling logic
  unit-tested without a server; integration tests hit a real Redis via `TEST_REDIS_URL`
  and skip when unset (matching the shipped `redis` package; no miniredis dependency):
  marshal round-trip, prefix isolation, `Clear` deletes only the prefix.

---

## Cross-cutting

- **Error surface:** `cache.{ErrNotFound,ErrClosed,ErrMarshal,ErrUnmarshal}`,
  `circuitbreaker.ErrOpen`, `retry.Permanent()`. Each package still gets an `errors.go`.
- **Determinism:** `clock.NewMock` + `Mock.Advance` make circuitbreaker timeouts and cache
  lazy-expiry deterministic. `retry` uses tiny real backoff durations. `singleflight` and
  `parallel` use sync barriers + atomic counters; `backoff` jitter is tested by bounds.
  The cache janitor goroutine is integration-tested (time-based).
- **Dependencies:** `backoff`/`retry`/`singleflight`/`parallel` are pure stdlib;
  `circuitbreaker` adds `clock`; `cache` core adds `clock` + `singleflight`; `cache/redis`
  adds the shipped `redis` (go-redis) — isolated in the subpackage. **No new `go.mod`
  entries** (go-redis already present).

## Build order (DAG)

Four parallelizable tracks:

1. `backoff` → `retry`
2. `singleflight` → `cache` → `cache/redis`
3. `parallel` (independent)
4. `circuitbreaker` (independent)

## Non-goals

- No env-loadable configuration in v1 (code-configured primitives; a `cache/redis` env
  `Config` can follow the `redis` pattern later).
- No byte-level `kv` Store seam here (distinct from the typed `cache`; built with the
  `lock`/`otp`/`sessionstore` packages that need it).
- No supervised background services — the only goroutine is the opt-in cache janitor,
  stopped by `Close()`; long-running services (`jobqueue`/`lock`) build on these later.
- No `errors.Join` aggregation in `parallel`'s default mode (opt-in via `WithCollectAll`).
