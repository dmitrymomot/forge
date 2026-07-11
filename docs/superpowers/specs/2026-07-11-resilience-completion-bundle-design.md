# Resilience Completion Bundle — Design

**Date:** 2026-07-11
**Status:** Approved (brainstorming) — ready for implementation planning
**Scope:** The three remaining `resilience/` roadmap packages: `quota`,
`loadshed`, `lock`. Shipping them completes the resilience domain. One spec,
one implementation plan, one PR (split only if the plan balloons).

---

## 1. Context & goals

`resilience/` already ships `backoff`, `retry`, `singleflight`, `parallel`,
`circuitbreaker`, `cache`, and `ratelimit`. This bundle adds the last three:

- **`quota`** — cumulative usage caps per subject over calendar / rolling
  windows plus live gauges (the plan-entitlement counterpart to `ratelimit`).
- **`loadshed`** — adaptive admission control that protects a service from
  its own overload.
- **`lock`** — distributed mutex (TTL leases + fencing tokens + auto-refresh)
  and cluster-singleton leader election.

All three follow the design DNA in [design.md](../../design.md): one of the
three idioms, `doc.go` runnable example, `config.go` / `options.go` /
`errors.go` anatomy, storage-agnostic `Store` seams with an in-memory built-in
and isolated driver subpackages, black-box tests, and `clock.Mock` for time.

Reused seams: `ratelimit.Store` (counter seam), `web/middleware.Middleware`,
`ops/supervisor.Service`, `data/postgres` (pool + `Migrator`), `core/clock`.

---

## 2. `resilience/quota`

### 2.1 Purpose & shapes

Cumulative usage caps per **subject** (opaque string — tenant, user, API key)
against a **caller-owned limit**, with no billing coupling. Covers three usage
shapes behind one `Meter` type:

| Shape | Example | Reset |
|-------|---------|-------|
| Calendar-window meter | events / month, LLM calls / month | at a boundary (1st, midnight, …) |
| Rolling-window meter | N in any trailing 30 days | fixed-window approximation (à la ratelimit) |
| Gauge | members incl. pending invites; storage bytes | never (live count) |

**Explicit non-goal:** feature-tier entitlement ("feature X on tier Y") — that
is set-membership, not a counter, and is served by `ops/featureflag`. The spec
and `doc.go` say so and point there.

### 2.2 Storage — rides the shared counter seam

`quota` does **not** define its own store. It consumes `ratelimit.Store`
(the counter seam: `Incr` add-and-return with TTL, `Get`, `Reset`, `Close`),
per design.md ("ratelimit owns the counter Store contract; quota and lockout
share it"). Implementations available to quota:

- `ratelimit` memory store (tests / dev / single node).
- `ratelimit/redisstore` (existing).
- `ratelimit/pgstore` — **new in this bundle** (see §5). Durable; gauges
  require it because the memory store is LRU/TTL and would silently reset a
  still-held seat count.

### 2.3 Window seam

```go
// Window maps "now" to the current period's key suffix and its reset time.
// period is "" for gauges (no suffix); reset is the zero Time for gauges.
type Window func(subject string, now time.Time) (period string, reset time.Time)

type Unit int // Daily | Weekly | Monthly

func Calendar(unit Unit, loc *time.Location) Window // loc nil => UTC
func Rolling(d time.Duration) Window
func Gauge() Window
```

A consumer with real billing anchors (renews on the 14th) writes its own
`Window` — quota imports nothing billing-related. The period string is folded
into the store key (`subject + ":" + period`); `reset` populates `Result.Reset`
and the calendar-meter TTL (time until `reset`). Gauges store under `subject`
with **no expiry** (see §5.1).

### 2.4 Limit & Result

```go
// Unlimited marks "no ceiling" for Max (pay-as-you-go / pure metering).
const Unlimited int64 = -1

// Limit is caller-resolved per call (from the subject's plan). No billing dep.
type Limit struct {
    Included int64 // allotment included in the plan
    Max      int64 // hard ceiling; usage in (Included, Max] is allowed but billable. Unlimited => no ceiling.
}

type Result struct {
    Reset     time.Time // when the window rolls (zero for gauges)
    Limit     Limit
    Used      int64     // total consumed this window (post-call)
    Remaining int64     // max(0, Included - Used)
    Overage   int64     // max(0, Used - Included) — the billable signal
    Allowed   bool
}
```

`Limit` combinations:
- **Hard cap** (events/month): `Included == Max` → deny past it, `Overage` = 0.
- **Overage** (LLM + extra): `Max > Included` → allowed to `Max`, `Overage > 0`.
- **Pay-as-you-go / metering-only**: `Included = 0, Max = Unlimited` → always
  allowed, `Overage == Used`.

### 2.5 API & semantics

```go
func New(store ratelimit.Store, window Window, opts ...Option) *Meter
// Options: WithClock(clock.Clock), WithKeyPrefix(string).

// Allow consumes cost against subject and reports the decision. Uses
// incr-then-rollback: Incr(cost); if the new total exceeds Max, compensate with
// Incr(-cost) and return Allowed=false (a rejected call does not burn quota).
func (m *Meter) Allow(ctx context.Context, subject string, cost int64, limit Limit) (Result, error)

// Usage reports current consumption without consuming (read-only).
func (m *Meter) Usage(ctx context.Context, subject string, limit Limit) (Result, error)

// Add applies a signed delta — reconcile token estimates to actual, or release
// gauge units (negative delta). Never rejects; returns the new total.
func (m *Meter) Add(ctx context.Context, subject string, delta int64) (int64, error)

// Set forces the counter to an absolute value — seed/repair a gauge from the
// consumer's authoritative DB count. Gauge windows only.
func (m *Meter) Set(ctx context.Context, subject string, value int64) error

// Reset clears the counter for subject's current window.
func (m *Meter) Reset(ctx context.Context, subject string) error
```

- **Reserve/adjust flow** (AI tokens, unknown cost until after the call):
  `Allow(estimate)` up front, then `Add(actual - estimate)` to reconcile. Full
  crash-safe `Reserve`/`Commit`/`Release` is a deferred non-goal.
- **incr-then-rollback race:** between `Incr(cost)` and the compensating
  `Incr(-cost)`, a concurrent reader may briefly see the inflated total. Accepted
  — correct for billing caps, and the window is microseconds off the hot path.
- `Set` is implemented as `Get` + `Add(target - current)` (the counter seam has
  no absolute set); documented as best-effort under concurrency — callers use it
  for periodic reconciliation, not per-request writes.

### 2.6 Errors

`errors.go` sentinels (single-line, `errors.Is`-matchable): `ErrInvalidCost`
(negative cost to `Allow`), `ErrInvalidLimit` (`Max < Included` and not
`Unlimited`). Store failures propagate wrapped.

### 2.7 Example

```go
m := quota.New(store, quota.Calendar(quota.Monthly, nil))
lim := quota.Limit{Included: 10_000, Max: 12_000} // 10k included, 2k overage
res, err := m.Allow(ctx, tenantID, callTokens, lim)
if err != nil { /* ... */ }
if !res.Allowed { return errPlanExceeded }
if res.Overage > 0 { billing.RecordOverage(tenantID, res.Overage) }
```

---

## 3. `resilience/loadshed`

### 3.1 Purpose

Adaptive admission control: reject a *fraction* of incoming work early and
cheaply when the service is overloaded, so admitted work still succeeds.
Protects the **callee** based on its **own current health** (contrast:
`ratelimit` = per-client fairness; `circuitbreaker` = protect the caller from a
failing dependency).

### 3.2 Criteria seam

```go
// Criteria reports current load pressure in [0,1]; 0 idle, 1 saturated.
// Polled once per admission decision.
type Criteria interface {
    Pressure() float64
}
```

Built-ins in core (constructed by loadshed, fed by the admit→done lifecycle):

- `Concurrency(max int) Criteria` — pressure = in-flight / max.
- `Latency(threshold time.Duration, opts ...LatencyOption) Criteria` —
  pressure = EWMA(recent latency) / threshold, clamped to [0,1].

**CPU stays consumer-side:** no `gopsutil` dependency. A consumer implements
`Pressure()` over its own CPU/queue-depth/pool reader and passes it via
`WithCriteria`.

Built-in criteria that need per-request signals (concurrency inc/dec, latency
samples) receive them through internal `onAdmit()` / `onDone(latency)` hooks the
shedder invokes; the public interface stays the single `Pressure()` method.

### 3.3 Decision logic

- Overall pressure = **max** of all criteria (worst signal wins).
- **Probabilistic rejection ramp:** below `threshold` (low-water) admit all;
  above it, reject probability rises linearly with pressure, **capped below
  1.0** so a `floor` fraction is always admitted (the "fail-open sampler" —
  keeps latency signals fresh and lets the system recover instead of a
  self-perpetuating 100%-reject lockout).
- **Fail-open on fault:** a Criteria panic or error → admit (a monitoring glitch
  must never become an outage; mirrors `ratelimit`'s fail-open on Store error).
- Randomness: `math/rand/v2` (no seeding, no shared-lock contention).

### 3.4 API

```go
func New(opts ...Option) *Shedder
// Options: WithCriteria(...Criteria), WithThreshold(float64) (default e.g. 0.8),
// WithFloor(float64) (min admit rate at saturation, default e.g. 0.05),
// WithClock(clock.Clock).

// Acquire is the non-HTTP admission path. If admitted, returns a Ticket whose
// Release() MUST be called on completion (records latency, decrements inflight).
// admitted=false means shed — do not call Release.
func (s *Shedder) Acquire(ctx context.Context) (Ticket, bool)

type Ticket interface{ Release() }

// Middleware wraps Acquire: on shed, emits 503 + Retry-After via a configurable
// responder; on admit, serves and Releases. WithSkip never-sheds matched
// requests (health checks, admin).
func (s *Shedder) Middleware(opts ...MiddlewareOption) middleware.Middleware
// MiddlewareOption: WithResponder(func(w,r)), WithSkip(func(*http.Request) bool).
```

**Non-goal:** priority / LIFO admission classes (Netflix-concurrency-limits
territory). `WithSkip` covers hard exemptions; anything richer stays out.

### 3.5 Example

```go
sh := loadshed.New(
    loadshed.WithCriteria(loadshed.Concurrency(500), loadshed.Latency(200*time.Millisecond)),
)
mux.Use(sh.Middleware()) // 503s a slice of traffic when inflight or p-latency climbs
// non-HTTP:
if t, ok := sh.Acquire(ctx); ok { defer t.Release(); process(job) }
```

---

## 4. `resilience/lock`

### 4.1 Purpose

Distributed mutex with TTL leases, monotonic fencing tokens, and auto-refresh;
plus cluster-singleton leader election as a `supervisor.Service`. Two consumer
shapes:

- **Mutual exclusion** around a critical section: `Acquire` → work → `Release`.
- **Cluster singleton** (run a cron / outbox pump / migration on exactly one
  node with automatic failover): `RunOnLeader`.

### 4.2 Store seam (3 methods, lease-based)

```go
type Store interface {
    // Acquire claims key for owner until now+ttl. Returns a monotonic fencing
    // token on success; ok=false if another live owner holds it.
    Acquire(ctx context.Context, key, owner string, ttl time.Duration) (fence uint64, ok bool, err error)
    // Refresh extends the lease iff owner still holds key; ok=false if lost.
    Refresh(ctx context.Context, key, owner string, ttl time.Duration) (ok bool, err error)
    // Release frees key iff held by owner (no-op otherwise).
    Release(ctx context.Context, key, owner string) error
}
```

Implementations:

- **memory** (built-in) — map + mutex + atomic fence counter; expired leases
  reclaimed lazily on `Acquire`. Single-process only (tests / dev / single node).
- **`lock/pgstore`** — table-based lease (see §5.2). `key` PK, `owner`,
  `expires_at`, `fence bigint`. Expiry compared against the DB's own `now()`
  (no cross-node clock skew); fence from a sequence / monotonic bump on acquire.
  Works through any pooler; observable via `SELECT`.
- **`lock/redisstore`** — single-instance Redis. `SET key owner NX PX ttl` for
  acquire; fence via a companion `INCR key:fence` on successful claim; Lua
  scripts for owner-checked release and refresh (`PEXPIRE`). Documented as
  single-instance semantics — **not** Redlock (multi-master is out of scope).

### 4.3 Lock / Lease API

```go
func New(store Store, opts ...Option) *Lock
// Options: WithTTL(d) (default e.g. 30s), WithOwner(string) (default random id
// via core/id), WithRefreshInterval(d) (default TTL/3), WithClock(clock.Clock).

// Acquire blocks, retrying, until the lock is held or ctx is done.
func (l *Lock) Acquire(ctx context.Context, key string) (*Lease, error)
// TryAcquire makes a single attempt; ok=false if already held.
func (l *Lock) TryAcquire(ctx context.Context, key string) (*Lease, bool, error)

type Lease struct { /* ... */ }
func (le *Lease) Fence() uint64            // pass to the protected resource to reject stale holders
func (le *Lease) Done() <-chan struct{}    // closed when the lease is LOST (a refresh failed)
func (le *Lease) Release(ctx context.Context) error
```

- On a successful acquire, a background goroutine refreshes at
  `RefreshInterval` until `Release` or ctx-cancel. If a refresh fails (expired
  or stolen), it closes `Done()` so the holder can abort its critical section.
- **Fencing tokens** are the Kleppmann guard: a holder paused past its TTL (GC
  stall) is rejected by the resource because a newer holder carries a higher
  token. `Fence()` is monotonic per key.

### 4.4 `RunOnLeader` — cluster singleton

```go
// RunOnLeader returns a supervisor.Service that runs `run` on exactly one node.
// It continuously campaigns for `key`; whoever holds the lease runs run(leaderCtx);
// the rest stand by. leaderCtx is cancelled on leadership loss (run must return
// promptly). On supervisor shutdown it releases the lease (instant failover
// instead of waiting a TTL). Automatic and continuous — no explicit "become
// leader" call.
func (l *Lock) RunOnLeader(name, key string, run func(ctx context.Context) error) supervisor.Service
```

`Service.Name()` returns `name`. `Run(ctx)`: loop { `Acquire`(key) → run leader
work with a leaderCtx cancelled on `Lease.Done()` or parent ctx → on loss,
re-campaign }. Wire with one line: `sup.Add(l.RunOnLeader("scheduler", "cron", scheduler.Run))`.

### 4.5 Errors

`errors.go`: `ErrNotHeld` (release/refresh a lease you don't own),
`ErrLockLost` (surfaced on `Done()`-driven abort paths where an error is
needed). Store failures propagate wrapped.

### 4.6 Example

```go
l := lock.New(store, lock.WithTTL(30*time.Second))
// mutual exclusion:
lease, err := l.Acquire(ctx, "tenant:42:import")
if err != nil { return err }
defer lease.Release(ctx)
importBatch(ctx, lease.Fence())
// cluster singleton:
sup.Add(l.RunOnLeader("outbox", "outbox-pump", outbox.Run))
```

---

## 5. Cross-cutting

### 5.1 Counter-seam contract fix — `ttl ≤ 0` means "no expiry"

Quota gauges need a non-expiring counter. Today `ratelimit.Store` sets
`expiresAt = now.Add(ttl)`, so `ttl ≤ 0` is a useless "already expired". Change
the **contract** and all three implementations:

- Contract (`ratelimit.Store` doc): "A `ttl ≤ 0` creates a key with **no
  expiry**."
- memory store: represent no-expiry with a zero-`time.Time` sentinel that
  `Get`/`Incr`/`sweep` treat as never-expired. (Still LRU/janitor-prunable and
  single-process — durable gauges use pgstore.)
- `ratelimit/redisstore`: skip `PEXPIRE` when `ttl ≤ 0` (adjust the Lua script).
- `ratelimit/pgstore` (new): store `NULL expires_at` for `ttl ≤ 0`; `NULL` is
  never expired.

Backward-compatible: no existing caller passes `ttl ≤ 0` (it was meaningless).
Covered by new tests on each implementation.

### 5.2 Postgres migrations — pattern set by this bundle

These are the repo's first pg-table-owning drivers. Each pg driver
(`ratelimit/pgstore`, `lock/pgstore`):

- Embeds its DDL via `//go:embed schema.sql`.
- Exposes `Schema string` and an idempotent `Migrate(ctx, *pgxpool.Pool) error`
  (`CREATE TABLE IF NOT EXISTS …`) for self-contained setup.
- Also ships the DDL as a versioned migrations `fs.FS` (`Migrations embed.FS`)
  so consumers using the `migration`/goose runner (via `postgres.WithMigrator`)
  can fold it into their normal migration flow.

Tables: counter store → `(key text PK, value bigint, expires_at timestamptz NULL)`;
lock store → `(key text PK, owner text, expires_at timestamptz, fence bigint)`
plus a `fence` sequence (or a monotonic bump). Final DDL decided in the plan.

### 5.3 Dependencies

- `quota`, `loadshed`, `lock` cores: stdlib + `core/clock`, `core/id`
  (lock owner), `web/middleware` (loadshed), `ops/supervisor` (lock). No new
  external deps.
- `*/redisstore`: `github.com/redis/go-redis/v9` (already vendored).
- `*/pgstore`: `github.com/jackc/pgx/v5` (already vendored).
- Each real dep stays isolated in its driver subpackage; cores never import a
  driver client.

### 5.4 Testing policy

- Black-box tests (`package X_test`); white-box only to assert unexported state.
- `clock.Mock` drives windows, TTL expiry, refresh, and latency EWMA
  deterministically.
- Memory stores are the in-suite doubles; the counter memory store (from
  `ratelimit`) doubles for quota.
- Driver subpackages (`pgstore`, `redisstore`) get integration tests gated on a
  live service, following the repo's existing pattern.
- Any perf-motivated complexity (e.g. loadshed's sharded counters, quota's
  hot-path `Allow`) requires a benchmark in the PR per design.md §Performance.

---

## 6. Package layout & rough size

```
resilience/quota/                 ~450–650 LOC  (meter, window, limit, errors, doc)
resilience/ratelimit/pgstore/     ~200–300 LOC  (+ schema.sql, migration)
resilience/loadshed/              ~400–600 LOC  (criteria, ramp, shedder, middleware)
resilience/lock/                  ~500–700 LOC  (store iface, memory, lock, lease, RunOnLeader)
resilience/lock/pgstore/          ~200–300 LOC  (+ schema.sql, migration)
resilience/lock/redisstore/       ~200–300 LOC
```

All within the single-responsibility ~250–850 LOC guidance; drivers are thin
leaves. Consistent with prior multi-package bundles (caching = 6 pkgs).

## 7. Non-goals (recorded so they aren't silently reintroduced)

- Feature-tier entitlement in quota → `ops/featureflag`.
- Full `Reserve`/`Commit`/`Release` crash-safe two-phase quota reservations.
- loadshed priority / LIFO admission classes; built-in CPU criterion.
- lock Redlock / multi-master Redis; advisory-lock driver (can't carry TTL or
  fencing — the roadmap's "advisory-lock" wording is superseded by table-lease).

## 8. Open questions

None — all design decisions resolved during brainstorming.
