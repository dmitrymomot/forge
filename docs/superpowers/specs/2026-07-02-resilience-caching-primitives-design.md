# Resilience & Caching Primitives — Design

**Date:** 2026-07-02
**Status:** Approved (design), pending implementation plan
**Bundle:** `backoff`, `retry`, `singleflight`, `parallel`, `ttlcache`, `circuitbreaker`

## Context & motivation

These six packages are the **dependency leaves** of the resilience/async layer: each
depends only on the Go standard library and the already-shipped `clock` package —
nothing unbuilt, and no additions to `go.mod`. They are the widest-fan-out building
blocks in the recommended tier, forming the substrate under the not-yet-built
`httpclient`, `jobqueue`, `lock`, `ratelimit`, `kv`, `otp`, `idempotency`, and
`sessionstore` packages.

Two of them (`backoff`, `retry`) are **core-tier** in the roadmap; they are folded
into this bundle because they complete the resilience family and share the exact same
zero-dependency profile.

Selection criterion (agreed during brainstorming): *near-zero-dependency packages that
are building blocks for more complex packages.* This bundle was chosen over
"auth building-blocks" and the "cookie/pagination/objectstore trio" because it unblocks
the most downstream work.

## Conventions applied (existing forge DNA — not re-litigated)

- `type Option func(*config)` with an **unexported** `config`; **no builders**.
- **Options-only** configuration (agreed). These are code-configured library primitives
  (like `sync.Mutex` / `errgroup`), *not* deploy-time services — so **no exported
  `Config`, no `DefaultConfig`, no `Validate`, no `env` tags**.
- `errors.go` with `errors.Is`-matchable single-line sentinels.
- `doc.go` with a runnable example.
- `clock.Clock` injection via `WithClock` on packages that read the current time
  (`ttlcache`, `circuitbreaker`) → deterministic tests with `clock.NewMock`. The shipped
  `clock.Clock` is `Now()`-only (no `Sleep`/`After`), so `retry` — which must *wait* —
  sleeps via a real ctx-aware timer instead (see Deviation #4).
- **Black-box tests only** (`package <name>_test`).
- Flat top-level packages, ~120–300 LOC each. Go 1.26 idioms (`new(expr)`, run
  `modernize` + `just fmt`/`just lint` before done).

## Scope decisions (agreed)

In scope for v1 (all four optional capabilities kept):

- `ttlcache` **LRU eviction** (`WithMaxEntries`, `container/list`).
- `ttlcache` **`GetOrLoad`** with singleflight stampede protection.
- `parallel` **functional `Map`/`ForEach`** helpers (atop the bounded `Group`).
- `circuitbreaker` **`OnStateChange`** callback hook.

Explicitly out of scope for v1 (YAGNI, addable later):

- `ttlcache` background janitor goroutine (lazy expiry only in v1).
- `backoff` decorrelated-jitter / pluggable RNG seam (jitter uses `math/rand/v2`).
- `parallel` error aggregation (`errors.Join`) — v1 is fail-fast.

## Deviations from the roadmap sketch (deliberate)

1. **`Backoff` is stateless.** The roadmap interface had both `Next(attempt int)` and
   `Reset()`, which contradict — if the caller passes `attempt`, there is no internal
   state to reset. The interface is `Next(attempt int) time.Duration` only, making it
   goroutine-safe; `retry` owns the attempt counter.
2. **`singleflight` is generic over the key type.** Roadmap keyed by `string`
   (`Group[T any]`); this design is `Group[K comparable, V any]` so `ttlcache` can
   coalesce on its own key type directly (`string` is just `K=string`).
3. **`parallel` is fail-fast**, not aggregating. `Wait` returns the *first* error and the
   derived context cancels the siblings (errgroup semantics), rather than
   `errors.Join`-ing all failures.
4. **`retry` does not take a clock.** The shipped `clock.Clock` interface is `Now()`-only
   (no `Sleep`/`After`), so a mock clock cannot drive retry's waits. `retry` sleeps via a
   context-aware real `time.Timer`; its tests use tiny backoff durations. Only the
   `Now()`-based packages (`ttlcache`, `circuitbreaker`) take `WithClock`. This drops
   `retry`'s roadmap dependency on `clock` — it depends only on `backoff`.

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

// Options (apply to Exponential):
//   WithMultiplier(f float64)     // growth factor, default 2.0
//   WithJitter(fraction float64)  // 0..1; randomizes the delay by ±fraction
```

- `Exponential` computes `base * multiplier^(attempt-1)`, capped at `max`.
- Constructors are tolerant of degenerate input (clamp `base` to ≥ 1ns, `max` ≥ `base`);
  they do **not** return errors — pure value constructors.
- Jitter uses `math/rand/v2` top-level functions (auto-seeded, concurrency-safe).
- **Tests:** exact values for `Constant`/un-jittered `Exponential`; jittered results
  asserted within `[expected*(1-fraction), expected*(1+fraction)]` bounds.
- **Errors:** none.

### `retry`

Execute a function with retries. Deps: `backoff`; `context`, `time`, `errors`.

```go
func Do(ctx context.Context, fn func(ctx context.Context) error, opts ...Option) error
func Permanent(err error) error // wrap an error to stop retrying immediately

// Options:
//   WithMaxAttempts(n int)          // default 3
//   WithBackoff(b backoff.Backoff)  // default Exponential(100ms, 10s, WithJitter(0.5))
//   WithRetryIf(func(error) bool)   // default: retry all non-Permanent errors
```

- Loops up to `maxAttempts`. On error: if the error is `Permanent`-wrapped → return the
  unwrapped error immediately; if `RetryIf` returns false → return the error; otherwise
  sleep `backoff.Next(attempt)` and retry.
- Sleeps use a context-aware real `time.Timer`
  (`select { case <-ctx.Done(): …; case <-timer.C: }`); on cancellation returns
  `ctx.Err()`.
- On exhaustion returns the last error from `fn`.
- `Permanent` is an unwrappable sentinel wrapper (detected via `errors.As` on an internal
  `permanentError` type), not a package var.
- **Tests:** use tiny backoff durations (e.g. `backoff.Constant(time.Millisecond)`) and
  assert attempt counts, `Permanent` short-circuit, `RetryIf` gating, and ctx
  cancellation. Exact delay math is covered by `backoff`'s own (pure) tests, so no fake
  clock is needed here.

### `singleflight`

Generic request coalescing (cache-stampede protection). Deps: `context`, `sync`.

```go
type Group[K comparable, V any] struct { /* zero-value usable; no New() */ }

func (g *Group[K, V]) Do(ctx context.Context, key K,
    fn func(ctx context.Context) (V, error)) (v V, shared bool, err error)
func (g *Group[K, V]) Forget(key K)
```

- Zero-value usable (lazy map init under mutex), like `sync.Once`.
- The in-flight `fn` runs under a **cancellation-detached context**
  (`context.WithoutCancel` of the leader's ctx) so one caller cancelling does not poison
  the shared execution for the others. Each caller's own `ctx` still bounds *its* wait
  and is what its `Do` returns on cancellation.
- `shared` reports whether the result was shared with concurrent callers.
- **Errors:** none.
- **Tests:** launch N goroutines on the same key behind a sync barrier, assert `fn` ran
  once and all callers got the same value with `shared==true`; `Forget` lets the next
  call re-execute.

### `parallel`

Bounded concurrent execution with fail-fast error handling. Deps: `context`, `sync`.

```go
type Group struct { ... }

func New(ctx context.Context, opts ...Option) (*Group, context.Context)
func (g *Group) Go(fn func(ctx context.Context) error)
func (g *Group) Wait() error

// Option:
//   WithLimit(n int) // max concurrent; n ≤ 0 = unbounded

// Functional helpers (order-preserving results):
func ForEach[T any](ctx context.Context, items []T, limit int,
    fn func(ctx context.Context, item T) error) error
func Map[T, U any](ctx context.Context, items []T, limit int,
    fn func(ctx context.Context, item T) (U, error)) ([]U, error)
```

- `New` returns the group and a derived context that is **cancelled on the first error**
  (or when all goroutines finish). `Wait` returns that first non-nil error.
- `Map` preserves input order: `result[i]` corresponds to `items[i]`; on error returns
  the first error and a nil slice.
- `limit ≤ 0` means unbounded; otherwise a semaphore of size `limit` bounds concurrency.
- **Errors:** none exported (returns caller `fn` errors verbatim).
- **Tests:** assert bounded concurrency (peak in-flight ≤ limit via an atomic counter),
  first-error propagation + sibling cancellation, order preservation in `Map`.

### `ttlcache`

Generic in-memory TTL cache with optional LRU cap and stampede-safe load.
Deps: `clock`, `singleflight`; `sync`, `time`, `container/list`.

```go
type Cache[K comparable, V any] struct { ... }

func New[K comparable, V any](opts ...Option) *Cache[K, V] // Option is NON-generic

func (c *Cache[K, V]) Get(k K) (V, bool)
func (c *Cache[K, V]) Set(k K, v V)
func (c *Cache[K, V]) SetTTL(k K, v V, ttl time.Duration)
func (c *Cache[K, V]) Delete(k K)
func (c *Cache[K, V]) Len() int
func (c *Cache[K, V]) GetOrLoad(ctx context.Context, k K,
    fn func(ctx context.Context) (V, error)) (V, error)

// Options:
//   WithDefaultTTL(d time.Duration) // default 0 = no expiry
//   WithMaxEntries(n int)           // default 0 = unbounded; >0 = LRU eviction
//   WithClock(c clock.Clock)
```

- `Option` is **non-generic** (`func(*config)`): TTL, size, and clock do not reference
  `K`/`V`, keeping call sites clean:
  `ttlcache.New[string,int](ttlcache.WithMaxEntries(1000), ttlcache.WithDefaultTTL(time.Minute))`.
- **Lazy expiry:** entries carry an expiry timestamp (from the clock); `Get` treats an
  expired entry as absent and drops it. No background janitor in v1.
- **LRU:** with `WithMaxEntries(n)`, storage is `map[K]*list.Element` over a
  `container/list`; `Get`/`Set` move the entry to the front; exceeding `n` evicts the
  back (least-recently-used). This bounds memory even without expiry.
- **`GetOrLoad`** wraps an internal `singleflight.Group[K, V]`: on a cache miss, one
  concurrent load runs, its result is stored with the default TTL, and all waiters
  receive it. Load errors are not cached.
- **Errors:** none (miss is `(zero, false)`); load errors from `fn` returned verbatim.
- **Tests:** `clock.NewMock` + `Mock.Advance` drive expiry deterministically; assert LRU
  eviction order, `GetOrLoad` single-flight under concurrent misses, and that a load
  error is not stored.

### `circuitbreaker`

Closed/open/half-open breaker to fail fast against a failing dependency.
Deps: `clock`; `sync`, `time`, `errors`.

```go
type State int // Closed, Open, HalfOpen; implements fmt.Stringer

var ErrOpen = errors.New("circuitbreaker: circuit open")

type Breaker struct { ... }

func New(opts ...Option) *Breaker
func (b *Breaker) Do(ctx context.Context, fn func(ctx context.Context) error) error
func (b *Breaker) State() State

// Options:
//   WithFailureThreshold(n int)              // consecutive failures to open; default 5
//   WithOpenTimeout(d time.Duration)         // time before half-open probe; default 30s
//   WithHalfOpenMax(n int)                   // concurrent probes allowed; default 1
//   WithOnStateChange(func(from, to State))  // transition hook (logging/metrics)
//   WithClock(c clock.Clock)
```

- **Closed:** counts consecutive failures; at `≥ threshold` → **Open**.
- **Open:** `Do` returns `ErrOpen` without calling `fn`; after `openTimeout` elapses →
  **HalfOpen**.
- **HalfOpen:** allows up to `halfOpenMax` probe calls; a success → **Closed** (counters
  reset), a failure → **Open** (timeout restarts).
- `OnStateChange` fires on every transition with `(from, to)`.
- **Errors:** `ErrOpen`.
- **Tests:** `clock.NewMock` + `Mock.Advance` drive the open→half-open timeout
  deterministically; assert threshold tripping, `ErrOpen` fast-fail, half-open probe
  gating, and `OnStateChange` callbacks.

---

## Cross-cutting

- **Error surface (minimal):** only `circuitbreaker.ErrOpen` and `retry.Permanent()`.
  The rest signal via `(V, bool)` or plain returns. Each package still gets an
  `errors.go` per convention.
- **Determinism:** `clock.NewMock` + `Mock.Advance` make ttlcache expiry and breaker
  timeouts fully deterministic. `retry` is tested with tiny real backoff durations (its
  delay math is covered by `backoff`'s exact tests). `singleflight`/`parallel` are tested
  with sync barriers and atomic counters; `backoff` jitter is tested by bounds.
- **Dependencies:** stdlib + shipped `clock` only. The single intra-bundle edge is
  `ttlcache → singleflight`. Nothing changes `go.mod`.

## Build order (DAG)

Four parallelizable tracks:

1. `backoff` → `retry`
2. `singleflight` → `ttlcache`
3. `parallel` (independent)
4. `circuitbreaker` (independent)

## Non-goals

- No env-loadable configuration (these are code-configured primitives).
- No background goroutines / lifecycle (`ttlcache` janitor deferred; supervised services
  like `jobqueue`/`lock` build on these later).
- No distributed/remote backends (Redis stores live in the consuming packages,
  e.g. `ratelimit/redisstore`, not here).
- No `errors.Join` aggregation in `parallel` v1.
