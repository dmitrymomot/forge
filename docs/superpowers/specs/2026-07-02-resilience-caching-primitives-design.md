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
is a **provider-backed package**: a pluggable byte-level `Store` (with built-in **memory**
and **Redis** providers) and a typed `Cache[V]` facade over it. Its core adds only
`singleflight` (plus `clock` for the built-in in-memory store); the Redis provider is
isolated in a `cache/redis` subpackage. **No new `go.mod` dependencies:** go-redis is
already present via the shipped `redis` package.

Two of these (`backoff`, `retry`) are **core-tier** in the roadmap; they are folded into
this bundle because they complete the resilience family.

The `cache` design is informed by the prior forge `pkg/cache` implementation
(commit `0b68f05`) — `Cache[V]`, memory + Redis providers, `GetOrSet` stampede protection,
`Marshaler` seam, key-prefix isolation — re-expressed in v2 conventions and re-layered so
the cache facade **does not own the backend lifecycle**.

## Design principles (all six packages)

1. **Instances over globals.** Every constructor returns an isolated instance that owns its
   own state. **Zero package-level mutable state, no singletons.**
2. **The cache facade owns no backend lifecycle.** A `Store` (in-memory or Redis) is a
   standalone instance you construct and `Close` yourself — exactly like a `redis` client.
   `Cache[V]` is a thin typed facade injected with a `Store`; it has **no `Close`** and
   never starts or stops a backend. The in-memory engine (map, LRU, janitor goroutine) is a
   first-class separate instance, not something hidden inside a cache constructor.
3. **Isolation.** Two instances never interfere. Over a shared `Store`, each `Cache[V]`
   isolates via a key `Prefix`; for physical isolation, construct separate `Store`s. Two
   caches never share buckets.
4. **Usability first.** Sane defaults, one-line read-through (`GetOrSet`), JSON marshaling
   by default, minimal construction boilerplate. Efficiency is necessary but not sufficient
   — the API must be pleasant without a wrapper.

## Conventions applied (existing forge DNA — not re-litigated)

- `type Option func(*config)` with an **unexported** `config`; **no builders**.
- **Options-only** configuration (no exported `Config`/env tags in v1). A `cache/redis` env
  `Config` may follow the `redis` package pattern later.
- `errors.go` with `errors.Is`-matchable single-line sentinels; `doc.go` runnable example.
- `clock.Clock` injection via `WithClock` on packages that read the current time
  (`circuitbreaker`, the memory `Store`) → deterministic tests with `clock.NewMock`. The
  shipped `clock.Clock` is `Now()`-only (no `Sleep`/`After`), so `retry` sleeps via a real
  ctx-aware timer instead (Deviation #3).
- **Black-box tests only** (`package <name>_test`). Flat top-level packages; Go 1.26 idioms
  (`new(expr)`; run `modernize` + `just fmt`/`just lint` before done).

## Scope decisions (agreed)

In scope for v1:

- `parallel` functional `Map`/`ForEach` + opt-in `WithCollectAll()` aggregation.
- `circuitbreaker` `OnStateChange` callback.
- `cache`: pluggable `Store` with **memory + Redis** providers (each a standalone,
  caller-closed instance), typed `Cache[V]` facade, `GetOrSet` stampede protection,
  `Marshaler` seam (JSON default), LRU (`WithMaxEntries`), optional memory janitor
  (`WithCleanupInterval` + the store's `Close`), key-prefix isolation.

Out of scope for v1 (YAGNI, addable later):

- `backoff` decorrelated-jitter / pluggable RNG seam.
- Typed eviction callbacks (`WithOnEvict`) — the byte-level `Store` cannot surface a typed
  `V` at eviction time; revisit if a concrete need appears.
- A separate byte-level `kv` seam for `lock`/`otp`/`sessionstore`. (Those may reuse
  `cache.Store` or get their own; decided when built.)
- Env-loadable `Config` for the cache providers.

## Deviations from the roadmap sketch (deliberate)

1. **`Backoff` is stateless.** Interface is `Next(attempt int) time.Duration` only
   (goroutine-safe); `retry` owns the attempt counter. (Roadmap had a contradictory
   `Reset()`.)
2. **`parallel` is fail-fast by default, aggregation opt-in** (`WithCollectAll()` →
   run-all-then-`errors.Join`).
3. **`retry` takes no clock and is instance-based.** Shipped `clock` is `Now()`-only, so it
   sleeps via a ctx-aware real `time.Timer`. Shaped as `New(...Option) *Retrier` + a
   convenience `retry.Do(...)`. Depends only on `backoff`.
4. **`cache` is a two-layer provider-backed package**, not the roadmap's in-memory-only
   `ttlcache`. Byte-level `Store` (memory + `cache/redis` providers) + typed `Cache[V]`
   facade; **string keys**; facade owns no backend lifecycle. `singleflight` stays
   **string-keyed** (the earlier "generalize to `Group[K,V]`" idea is dropped — `cache`
   keys by string).

---

## Package specifications

### `backoff`

Pure, stateless delay strategies. Deps: `time`, `math`, `math/rand/v2`.

```go
type Backoff interface { Next(attempt int) time.Duration } // attempt ≥ 1

func Constant(d time.Duration) Backoff
func Exponential(base, max time.Duration, opts ...Option) Backoff
//   WithMultiplier(f float64)     // default 2.0
//   WithJitter(fraction float64)  // 0..1; ±fraction randomization
```

- `base * multiplier^(attempt-1)`, capped at `max`. Constructors clamp degenerate input;
  no error returns. Jitter uses `math/rand/v2`.
- **Errors:** none. **Tests:** exact un-jittered; jittered within bounds.

### `retry`

Instance-based retry. Deps: `backoff`; `context`, `time`, `errors`.

```go
type Retrier struct { ... }
func New(opts ...Option) *Retrier
func (r *Retrier) Do(ctx context.Context, fn func(ctx context.Context) error) error
func Do(ctx context.Context, fn func(ctx context.Context) error, opts ...Option) error // one-shot
func Permanent(err error) error

//   WithMaxAttempts(n int)          // default 3
//   WithBackoff(b backoff.Backoff)  // default Exponential(100ms, 10s, WithJitter(0.5))
//   WithRetryIf(func(error) bool)   // default: retry all non-Permanent
```

- `Permanent`-wrapped → return unwrapped immediately; `RetryIf==false` → return; else sleep
  `backoff.Next(attempt)` via a ctx-aware `time.Timer` and retry; cancellation → `ctx.Err()`.
- `Permanent` detected via `errors.As` on an internal type. **Tests:** tiny backoff
  durations; assert attempts, short-circuit, gating, cancellation.

### `singleflight`

String-keyed request coalescing. Deps: `context`, `sync`.

```go
type Group[V any] struct { /* zero-value usable */ }
func (g *Group[V]) Do(ctx context.Context, key string, fn func(ctx context.Context) (V, error)) (v V, shared bool, err error)
func (g *Group[V]) Forget(key string)
```

- Zero-value usable; each `Group` independent. In-flight `fn` runs under
  `context.WithoutCancel` of the leader ctx so one caller cancelling doesn't poison the
  shared execution; each caller's ctx still bounds its wait. Used by `cache.GetOrSet`.
- **Errors:** none. **Tests:** N goroutines/one key → `fn` runs once, `shared==true`.

### `parallel`

Bounded concurrency; fail-fast default, opt-in aggregation. Deps: `context`, `sync`, `errors`.

```go
type Group struct { ... }
func New(ctx context.Context, opts ...Option) (*Group, context.Context)
func (g *Group) Go(fn func(ctx context.Context) error)
func (g *Group) Wait() error
//   WithLimit(n int)   // ≤0 = unbounded
//   WithCollectAll()   // no cancel-on-error; Wait returns errors.Join(...)

func ForEach[T any](ctx context.Context, items []T, limit int, fn func(ctx context.Context, T) error) error
func Map[T, U any](ctx context.Context, items []T, limit int, fn func(ctx context.Context, T) (U, error)) ([]U, error)
```

- Default: derived ctx cancels on first error; `Wait` returns it. `WithCollectAll`: run all,
  join errors. `Map` preserves order, fail-fast, nil slice on error.
- **Tests:** bounded concurrency (atomic peak ≤ limit), first-error cancel, collect-all
  join, `Map` ordering.

### `circuitbreaker`

Closed/open/half-open breaker. Deps: `clock`; `sync`, `time`, `errors`.

```go
type State int // Closed, Open, HalfOpen; Stringer
var ErrOpen = errors.New("circuitbreaker: circuit open")
type Breaker struct { ... }
func New(opts ...Option) *Breaker
func (b *Breaker) Do(ctx context.Context, fn func(ctx context.Context) error) error
func (b *Breaker) State() State
//   WithFailureThreshold(n)  // default 5    WithOpenTimeout(d)   // default 30s
//   WithHalfOpenMax(n)       // default 1    WithOnStateChange(func(from, to State))
//   WithClock(c clock.Clock)
```

- Closed→(≥threshold failures)→Open→(after timeout)→HalfOpen→(success)→Closed / (fail)→Open.
- **Errors:** `ErrOpen`. **Tests:** `clock.NewMock` + `Advance` drive timeouts; assert
  tripping, fast-fail, probe gating, `OnStateChange`.

### `cache` (+ `cache/redis`)

Two layers: a byte-level **`Store`** provider (you construct and own it, like a redis
client) and a typed **`Cache[V]`** facade over it (no lifecycle ownership).

```go
// package cache — abstraction + facade + memory store (deps: singleflight, clock; stdlib)

type Store interface {                                    // the pluggable backend
    Get(ctx context.Context, key string) ([]byte, error)             // ErrNotFound on miss
    Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
    Delete(ctx context.Context, key string) error
    Has(ctx context.Context, key string) (bool, error)
    DeletePrefix(ctx context.Context, prefix string) error           // scoped clear
    Close() error                                                    // lifecycle: caller-owned
}

type Marshaler[V any] interface {                         // JSON default; WithMarshaler overrides
    Marshal(v V) ([]byte, error)
    Unmarshal(data []byte) (V, error)
}

// Built-in in-memory Store — a standalone instance (own map/LRU/janitor/Close):
func NewMemoryStore(opts ...MemoryOption) Store
//   WithMaxEntries(n)         // 0 = unbounded; >0 = LRU (container/list)
//   WithCleanupInterval(d)    // >0 starts a janitor goroutine; Close() stops it; 0 = lazy
//   WithClock(c clock.Clock)

// Typed facade over ANY Store — has NO Close():
type Cache[V any] struct { ... }
func New[V any](store Store, opts ...Option) *Cache[V]
//   WithPrefix(p string)            // namespaces keys over a shared store (isolation)
//   WithDefaultTTL(d)               // applied when Set ttl == 0; default 5m
//   WithMarshaler(m Marshaler[V])   // default JSON

func (c *Cache[V]) Get(ctx context.Context, key string) (V, error)   // ErrNotFound on miss
func (c *Cache[V]) Set(ctx context.Context, key string, v V, ttl time.Duration) error
func (c *Cache[V]) Delete(ctx context.Context, key string) error
func (c *Cache[V]) Has(ctx context.Context, key string) (bool, error)
func (c *Cache[V]) Clear(ctx context.Context) error                  // deletes only c's prefix
func (c *Cache[V]) GetOrSet(ctx context.Context, key string,
    fn func(ctx context.Context) (V, time.Duration, error)) (V, error)
```

```go
// package cache/redis — isolated Redis Store provider (deps: forge redis + go-redis)
func NewStore(client goredis.UniversalClient, opts ...Option) cache.Store
```

TTL semantics: positive = expire; zero = facade `DefaultTTL`; negative = never.

Usage — memory and Redis are **symmetric injected backends the caller owns**:

```go
// in-memory: engine is a separate instance with its own lifecycle
store := cache.NewMemoryStore(cache.WithMaxEntries(10_000), cache.WithCleanupInterval(30*time.Second))
defer store.Close()                                   // caller owns the backend
users  := cache.New[User](store, cache.WithPrefix("users:"), cache.WithDefaultTTL(30*time.Minute))
orders := cache.New[Order](store, cache.WithPrefix("orders:"))   // same engine, isolated buckets

// Redis: identical shape — construct the client, own its Close
client := redis.Open(ctx, redis.WithConfig(cfg)); defer client.Close()
rstore := cacheredis.NewStore(client)
sess   := cache.New[Session](rstore, cache.WithPrefix("sess:"))
```

- **Lifecycle:** `Store.Close()` is owned by the constructing code; `Cache[V]` has **no
  `Close`** and never starts/stops a backend. This is the "backend is a separate instance,
  like redis" requirement.
- **Isolation:** `WithPrefix` namespaces a cache's keys over a shared store; construct
  separate stores for physical isolation. `Clear()` deletes only the prefix (memory: prefix
  match; redis: `SCAN`+`DEL`, **never `FLUSHDB`**). `Clear` with an empty prefix clears the
  whole store — documented.
- **GetOrSet:** method on the typed facade; dedups concurrent misses via a per-instance
  `singleflight.Group[V]`; best-effort `Set`; load errors are not cached.
- **Memory expiry:** lazy on access (`clock.Now()`) + optional janitor. Without
  `WithMaxEntries`/`WithCleanupInterval`, expired-unread entries persist until the store is
  closed/GC'd — documented; set one for long-lived stores.
- **Errors:** `ErrNotFound/ErrClosed/ErrMarshal/ErrUnmarshal`.
- **Tests:** memory store — `clock.NewMock` drives expiry, LRU order, `DeletePrefix`
  isolation, idempotent `Close`. facade — marshal round-trip, prefix scoping, `GetOrSet`
  single-flight (over a fake in-memory store). redis store — key/marshal logic unit-tested;
  integration via `TEST_REDIS_URL`, skip when unset (matching the shipped `redis` package;
  no miniredis dep): round-trip, prefix isolation, scoped `Clear`.

---

## Cross-cutting

- **Error surface:** `cache.{ErrNotFound,ErrClosed,ErrMarshal,ErrUnmarshal}`,
  `circuitbreaker.ErrOpen`, `retry.Permanent()`. Each package gets an `errors.go`.
- **Determinism:** `clock.NewMock` + `Advance` for circuitbreaker timeouts and memory
  expiry; `retry` uses tiny real backoff; `singleflight`/`parallel` use barriers + atomic
  counters; `backoff` jitter by bounds; the memory janitor goroutine is integration-tested.
- **Dependencies:** `backoff`/`retry`/`singleflight`/`parallel` pure stdlib;
  `circuitbreaker` adds `clock`; `cache` core adds `singleflight` (+ `clock` for the memory
  store); `cache/redis` adds the shipped `redis` (go-redis), isolated. **No new `go.mod`
  entries.**

## Build order (DAG)

1. `backoff` → `retry`
2. `singleflight` → `cache` → `cache/redis`
3. `parallel` (independent)
4. `circuitbreaker` (independent)

(`clock` — already shipped — is a dep of `circuitbreaker` and the memory store.)

## Non-goals

- **The cache facade never owns backend lifecycle** — no implicit engine creation/teardown
  inside `Cache[V]`; the `Store` is injected and caller-closed.
- No env-loadable configuration in v1.
- No supervised background services — the only goroutine is the opt-in memory janitor,
  stopped by the store's `Close()`.
- No `errors.Join` aggregation in `parallel`'s default mode (opt-in via `WithCollectAll`).
- No typed eviction callbacks in v1 (byte-level `Store` can't surface `V` at eviction).
