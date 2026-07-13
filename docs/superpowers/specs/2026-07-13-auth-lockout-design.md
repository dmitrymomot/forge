# auth/lockout — Design

**Date:** 2026-07-13
**Status:** Approved

## Purpose

Login/OTP failure counting with exponential (escalating) lockout windows.
After a configurable number of free failures, each further failure locks the
identity for an escalating duration. Not rate shaping (`ratelimit`), not
cumulative caps (`quota`) — failure-triggered escalation only.

## Decisions (from brainstorming Q&A)

1. **Model:** threshold → escalating lock. N free failures, then lock for
   `base × factor^k` capped at `maxLock`. `factor: 1.0` degenerates to
   fixed-duration lockout — one model covers both.
2. **API shape:** explicit `Allow`/`Fail`/`Reset` core **plus** a thin `Do`
   closure wrapper.
3. **Storage:** dual-seam consumer — `ratelimit.Store` (counter seam) for
   atomic failure counting, `cache.Store` (TTL-KV seam) for the lock marker.
   Zero new store code or drivers; both seams ship memory + redis backends
   (counters also pg). Sanctioned by design.md, which lists `lockout` under
   both seams. Precedent: `quota.New(store ratelimit.Store, ...)`.
4. **Middleware:** included, mirroring `ratelimit.Middleware` (KeyFunc +
   WithResponder) with two deliberate divergences (fail-closed default;
   middleware covers only the `Allow` gate).

## Architecture

Two store keys per identity:

```
lockout:<scope>:f:<hash>   failure counter   ratelimit.Store   TTL = window
lockout:<scope>:l:<hash>   lock marker       cache.Store       TTL = lock duration
                                             value = lock-until unix timestamp
```

`<hash>` = first 16 bytes of SHA-256 of the caller key, hex-encoded.
Hashing keeps PII (emails, phones, IPs) out of store key listings and makes
arbitrary bytes store-safe. Hygiene, not secrecy — no pepper (documented).
Single-tenant apps have no `<scope>` segment.

## Core API

```go
package lockout // auth/lockout

func New(counters ratelimit.Store, locks cache.Store, opts ...Option) (*Locker, error)

func (l *Locker) Allow(ctx context.Context, key string) (Result, error) // read-only pre-check
func (l *Locker) Fail(ctx context.Context, key string) (Result, error)  // record one failure
func (l *Locker) Reset(ctx context.Context, key string) error           // clear on success
func (l *Locker) Do(ctx context.Context, key string, fn func(ctx context.Context) error) error

type Result struct {
    Locked     bool
    RetryAfter time.Duration // >0 when Locked
    Until      time.Time     // lock expiry; zero when unlocked
    Failures   int64         // failures in the current memory window
    Remaining  int64         // free attempts left before the next lock
}
```

### Options and defaults

| Option | Default | Meaning |
|---|---|---|
| `WithThreshold(n int)` | 5 | free failures before the first lock |
| `WithBaseLock(d time.Duration)` | 1m | first lock duration |
| `WithFactor(f float64)` | 2.0 | escalation multiplier (1.0 = fixed) |
| `WithMaxLock(d time.Duration)` | 15m | lock duration cap |
| `WithWindow(d time.Duration)` | 30m | failure-memory window |
| `WithClock(clock.Clock)` | `clock.System()` | time injection for tests |
| `WithScope(func(ctx) (string, error))` | off | tenancy hook, fail-closed |

`New` validates config: nil stores, `threshold < 1`, `base <= 0`,
`factor < 1`, `max < base`, `window <= 0` → constructor error.

## State machine

**`Fail`:** `Incr` the failure counter (TTL = window, set on creation only).

- `n <= threshold` → no lock; `Remaining = threshold - n`.
- `n > threshold` → lock duration `L = min(base × factor^(n-threshold-1), maxLock)`;
  `until = now + L`; create the lock marker with `SetNX(lockKey, until, L)`.
  Concurrent threshold-crossers race safely: exactly one lock wins; losers
  `Get` the winner's expiry and report it (a marker that vanished between
  `SetNX` and `Get` — expiry race — is treated as unlocked). A `Fail` while
  already locked still increments the counter (escalating the *next* lock)
  but never extends the current one. `Fail` always populates
  `Result.Failures` (it just incremented the counter).
- Escalation math clamps before float→Duration conversion: huge counts must
  yield `maxLock`, never overflow or `Inf`.

**`Allow`:** `Get` lock marker. Present and `until > now` → locked with exact
`RetryAfter = until - now`; `Failures`/`Remaining` stay zero (no second
round-trip on the locked path). Present but `until <= now` (clock skew edge)
→ treated as unlocked. Absent → `Get` counter for `Failures`/`Remaining`.
Purely read-only: a parallel burst can pass `Allow` before a lock lands
(TOCTOU) — acceptable for anti-brute-force since the lock still lands
exactly once; documented.

**`Reset`:** delete both keys. Called on successful authentication.

**Escalation example (defaults):** failures 1–5 free → 6th locks 1m →
7th 2m → 8th 4m → 9th 8m → 10th+ 15m (cap).

**Documented sharp edge:** `ratelimit.Store.Incr` never extends a live key's
TTL, so the memory window is fixed from the *first* failure of a burst
(fixed-window semantics, same as `ratelimit`). Keep `window >= maxLock`
(defaults comply) or escalation memory expires before the last lock does.

## `Do` wrapper contract

1. Locked on entry → return `*LockedError`; `fn` never runs.
2. `fn` returns `nil` → `Reset`; a failed reset returns an
   `ErrStore`-wrapped error (never silently swallowed; worst case of a
   stale count is an early threshold, and the caller decides).
3. `errors.Is(err, ErrFailedAttempt)` → record `Fail`. If that crossed the
   threshold, return `*LockedError` wrapping the fn error — `errors.Is`
   matches both `ErrLocked` and `ErrFailedAttempt`. Otherwise return the fn
   error unchanged. If `Fail` itself hits a store error, return
   `errors.Join(fnErr, storeErr)`.
4. Any other error → passes through untouched, **not counted** — infra
   failures can never lock a user out.

## Middleware

```go
type KeyFunc func(*http.Request) string

func (l *Locker) Middleware(key KeyFunc, opts ...MiddlewareOption) middleware.Middleware
```

- Extract key → `Allow` → locked ⇒ 429 + `Retry-After` header; default
  plain-text body, `WithResponder(func(w, r, Result))` override (problem+json).
- Unlocked ⇒ stash the extracted key in the request context;
  `lockout.KeyFromContext(ctx) (string, bool)` lets the handler call
  `Fail`/`Reset` with the identical key.
- Empty key from the extractor ⇒ skip the check, pass through (the
  handler's own validation rejects malformed logins; documented).
- **Fail closed** (503) on store error — brute-force protection must not
  silently disable during an outage. `WithFailOpen()` opts into
  ratelimit-style fail-open for availability-first teams.
- Middleware covers only the `Allow` gate; `Fail`/`Reset` remain handler
  calls because only the handler knows the outcome. Pure-library usage
  stays fully supported.
- Form-key extractors rely on `ParseForm` caching (handler's
  `PostFormValue` still works); JSON-body extraction (read + restore) is
  the app's responsibility, documented with an example.

## Tenancy

Standard seam: `WithScope(func(ctx context.Context) (string, error))`.
Resolved on every call when configured; an error or empty scope fails
closed with `ErrScope`. Scope becomes a key segment — tenant A's failures
can never lock tenant B's identically-named user. Single-tenant apps omit
the option and pay zero ceremony.

## Errors

```go
var (
    ErrLocked        = errors.New("lockout: locked")
    ErrFailedAttempt = errors.New("lockout: failed attempt")
    ErrScope         = errors.New("lockout: scope resolution failed")
    ErrStore         = errors.New("lockout: store operation failed")
)

type LockedError struct {
    Result Result // RetryAfter, Until, Failures
    Err    error  // fn error that triggered the lock, if any
}
// Unwrap matches ErrLocked and (when present) the fn error chain.
```

`Allow`/`Fail` never return `ErrLocked` — locked is a normal `Result`.
Only `Do` converts locked state into `*LockedError`. Store failures wrap
`ErrStore` (naming matches `auth/otp`).

## Usage example

```go
counters := ratelimit.NewMemoryStore() // or ratelimit/redisstore, ratelimit/pgstore
locks := cache.NewMemoryStore()        // or cache/redis
lk, err := lockout.New(counters, locks,
    lockout.WithThreshold(5),
    lockout.WithBaseLock(time.Minute),
    lockout.WithMaxLock(15*time.Minute),
)

// Explicit style:
res, err := lk.Allow(ctx, email)
// res.Locked → 429 + Retry-After
// wrong creds → lk.Fail(ctx, email); res.Locked means "just locked, tell them how long"
// success → lk.Reset(ctx, email)

// Wrapper style:
err = lk.Do(ctx, email, func(ctx context.Context) error {
    if !credentialsValid(ctx) {
        return lockout.ErrFailedAttempt // counted
    }
    return nil // auto-Reset
})
```

Same shape for OTP flows: key by email/phone, `Fail` per wrong code,
`Reset` on the correct one.

## Testing

Black-box `lockout_test` package with real memory stores.

- **Escalation math:** table-driven — threshold boundary, 1m→2m→4m
  progression, cap, `factor:1` fixed mode, overflow clamping.
- **State machine:** fail-while-locked increments without extending; Reset
  clears both keys; lock expiry unlocks; window expiry forgets; unknown
  key → clean slate.
- **Clock:** `clock.Clock` injection for lock-until math; store-TTL expiry
  via short real TTLs (memory stores keep their own time, as in otp tests).
- **Concurrency:** N goroutines crossing threshold → exactly one lock
  (`SetNX`), consistent `Until` for losers; `-race` clean.
- **Tenancy** (`tenancy_test.go`): scope isolation, fail-closed empty/error
  scope, unscoped single-tenant path.
- **`Do` contract:** all four rules, double-`errors.Is` case, infra
  passthrough, reset-failure surfacing.
- **Middleware:** 429 + Retry-After, context key propagation, empty-key
  skip, fail-closed 503 + `WithFailOpen`, `WithResponder`.
- **Fuzz:** arbitrary caller keys (unicode, huge, empty, colons) → composed
  keys valid, `f:`/`l:` never collide.

## Benchmarks

`bench_test.go` (required): `Allow` unlocked (hot path), `Allow` locked,
`Fail` below threshold, `Fail` crossing threshold — memory stores.
Post-benchmark optimization pass, before/after numbers in the PR. Package's
own work is one SHA-256 + key concat; target minimal allocs beyond store
round-trips.

## Anti-scope

- Not rate shaping (`ratelimit`), not cumulative caps (`quota`).
- No CAPTCHA hooks, no notifications/webhooks on lockout — compose around
  `Fail`'s `Result`.
- No admin unlock API beyond `Reset` — that is the unlock.
- No IP reputation or velocity heuristics — key choice is caller policy.
- Successful-login anomaly detection belongs to `web/fingerprint`.

## Package layout & deps

```
auth/lockout/
  lockout.go     Locker, New, Allow/Fail/Reset
  do.go          Do wrapper
  middleware.go  Middleware, KeyFunc, KeyFromContext
  options.go     Options
  errors.go      Sentinels, LockedError
  keys.go        Key composition + hashing
  doc.go         Package docs, examples, anti-scope
  *_test.go      Tests, tenancy_test.go, fuzz, bench_test.go
```

**Deps:** `resilience/ratelimit`, `resilience/cache`, `core/clock`,
`web/middleware`, stdlib `crypto/sha256`.

On ship: delete the `auth/lockout` entry from `docs/packages.md` (roadmap
lists only unbuilt packages).
