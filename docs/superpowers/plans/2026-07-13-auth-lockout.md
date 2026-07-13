# auth/lockout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `auth/lockout` — login/OTP failure counting with escalating lockout windows over the existing counter + TTL-KV store seams.

**Architecture:** A `Locker` keeps two store keys per identity: a failure counter on `ratelimit.Store` (atomic `Incr`, TTL = memory window) and a lock marker on `cache.Store` (value = lock-until unix-milli, TTL = lock duration, created with `SetNX` so exactly one concurrent threshold-crosser wins). `Allow`/`Fail`/`Reset` form the core; `Do` wraps an attempt with sentinel-based failure classification; optional net/http middleware gates the `Allow` half.

**Tech Stack:** Go 1.26, `resilience/ratelimit` (counter seam), `resilience/cache` (TTL-KV seam), `core/clock`, `core/ctxkey`, `web/middleware`, stdlib `crypto/sha256`.

**Spec:** `docs/superpowers/specs/2026-07-13-auth-lockout-design.md` (approved).

## Global Constraints

- Work ONLY in the current branch (`dm/auth-lockout-brainstorm-12b6fc`); never switch.
- Run `just fmt ./auth/lockout/...` after file changes (package-path form — single-file form trips a betteralign quirk). Run `just lint` when a task finishes.
- Tests are black-box in `package lockout_test`; white-box (`package lockout`) only to assert unexported state (used once, for `lockDuration` overflow math).
- Use `httptest.NewRequest` in tests, never `http.NewRequest` with `_` err (nilaway rejects it).
- Use `wg.Go(fn)` (Go 1.26), never `wg.Add(1)/go/defer wg.Done()` (modernize rejects it). Use `b.Loop()` in benchmarks.
- Structured errors: single-line messages, `lockout:` prefix on sentinels, wrap with `%w`.
- Benchmarks are REQUIRED (`bench_test.go`) + post-benchmark optimization pass; before/after numbers go in the PR.
- NO Claude/Anthropic attribution lines in commits or PR text.
- Test command: `just test ./auth/lockout/...` (runs `-race -cover`).

---

### Task 1: Core Locker — options, errors, keys, Allow/Fail/Reset

**Files:**
- Create: `auth/lockout/errors.go`
- Create: `auth/lockout/options.go`
- Create: `auth/lockout/keys.go`
- Create: `auth/lockout/lockout.go`
- Test: `auth/lockout/lockout_test.go`
- Test: `auth/lockout/tenancy_test.go`
- Test: `auth/lockout/lockduration_internal_test.go` (white-box, math only)

**Interfaces:**
- Consumes:
  - `ratelimit.Store` (`github.com/dmitrymomot/forge/resilience/ratelimit`): `Incr(ctx, key string, delta int64, ttl time.Duration) (int64, error)` — creates key with TTL, NEVER extends a live key's TTL; `Get(ctx, key) (int64, error)` — 0 when absent; `Reset(ctx, key) error`; `Close() error`. Memory impl: `ratelimit.NewMemoryStore()`.
  - `cache.Store` (`github.com/dmitrymomot/forge/resilience/cache`): `Get(ctx, key) ([]byte, error)` — `cache.ErrNotFound` when absent/expired; `Set(ctx, key, val []byte, opts ...cache.SetOption) error` with `cache.WithTTL(d)` + `cache.WithSetNonExist()` (returns `cache.ErrExists` on a live key); `Delete(ctx, key) error` — idempotent, nil on absent. Memory impl: `cache.NewMemoryStore()`.
  - `clock.Clock` (`github.com/dmitrymomot/forge/core/clock`): `Now() time.Time`; `clock.System()`; `clock.NewMock(t)` with `Set/Advance`.
- Produces (later tasks rely on these exact signatures):
  - `func New(counters ratelimit.Store, locks cache.Store, opts ...Option) (*Locker, error)`
  - `func (l *Locker) Allow(ctx context.Context, key string) (Result, error)`
  - `func (l *Locker) Fail(ctx context.Context, key string) (Result, error)`
  - `func (l *Locker) Reset(ctx context.Context, key string) error`
  - `type Result struct { Until time.Time; RetryAfter time.Duration; Failures, Remaining int64; Locked bool }`
  - Options: `WithThreshold(int)`, `WithBaseLock(time.Duration)`, `WithFactor(float64)`, `WithMaxLock(time.Duration)`, `WithWindow(time.Duration)`, `WithClock(clock.Clock)`, `WithScope(func(context.Context) (string, error))`
  - Sentinels: `ErrLocked`, `ErrFailedAttempt`, `ErrScope`, `ErrStore`
  - Unexported: `(l *Locker) keys(ctx, key) (fails, lock string, err error)`, `(l *Locker) lockState(ctx, lockKey string) (Result, bool, error)`, `(l *Locker) lockDuration(n int64) time.Duration`, `l.cfg config`

- [ ] **Step 1: Write the failing tests**

`auth/lockout/lockout_test.go`:

```go
package lockout_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/lockout"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/resilience/ratelimit"
	"github.com/stretchr/testify/require"
)

func newLocker(t *testing.T, opts ...lockout.Option) *lockout.Locker {
	t.Helper()
	counters := ratelimit.NewMemoryStore()
	t.Cleanup(func() { _ = counters.Close() })
	locks := cache.NewMemoryStore()
	t.Cleanup(func() { _ = locks.Close() })
	lk, err := lockout.New(counters, locks, opts...)
	require.NoError(t, err)
	return lk
}

func TestNewValidation(t *testing.T) {
	t.Parallel()
	counters := ratelimit.NewMemoryStore()
	t.Cleanup(func() { _ = counters.Close() })
	locks := cache.NewMemoryStore()
	t.Cleanup(func() { _ = locks.Close() })

	tests := []struct {
		name     string
		counters ratelimit.Store
		locks    cache.Store
		opts     []lockout.Option
	}{
		{"nil counters", nil, locks, nil},
		{"nil locks", counters, nil, nil},
		{"zero threshold", counters, locks, []lockout.Option{lockout.WithThreshold(0)}},
		{"zero base lock", counters, locks, []lockout.Option{lockout.WithBaseLock(0)}},
		{"factor below one", counters, locks, []lockout.Option{lockout.WithFactor(0.5)}},
		{"max below base", counters, locks, []lockout.Option{lockout.WithBaseLock(time.Hour), lockout.WithMaxLock(time.Minute)}},
		{"zero window", counters, locks, []lockout.Option{lockout.WithWindow(0)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := lockout.New(tt.counters, tt.locks, tt.opts...)
			require.Error(t, err)
		})
	}
}

func TestAllowCleanSlate(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(3))
	res, err := lk.Allow(context.Background(), "user@example.com")
	require.NoError(t, err)
	require.False(t, res.Locked)
	require.Zero(t, res.RetryAfter)
	require.True(t, res.Until.IsZero())
	require.EqualValues(t, 0, res.Failures)
	require.EqualValues(t, 3, res.Remaining)
}

func TestFailBelowThreshold(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(3))
	ctx := context.Background()

	res, err := lk.Fail(ctx, "u")
	require.NoError(t, err)
	require.False(t, res.Locked)
	require.EqualValues(t, 1, res.Failures)
	require.EqualValues(t, 2, res.Remaining)

	res, err = lk.Fail(ctx, "u")
	require.NoError(t, err)
	require.False(t, res.Locked)
	require.EqualValues(t, 2, res.Failures)
	require.EqualValues(t, 1, res.Remaining)
}

// Escalation asserted through the winner-path Result.RetryAfter, which is the
// computed duration exactly (deterministic even on the system clock). Real
// short TTLs let each lock expire so the next Fail creates the next lock.
func TestFailEscalationProgression(t *testing.T) {
	t.Parallel()
	lk := newLocker(t,
		lockout.WithThreshold(1),
		lockout.WithBaseLock(40*time.Millisecond),
		lockout.WithFactor(2),
		lockout.WithMaxLock(200*time.Millisecond),
		lockout.WithWindow(5*time.Second),
	)
	ctx := context.Background()

	res, err := lk.Fail(ctx, "u") // n=1: free
	require.NoError(t, err)
	require.False(t, res.Locked)

	steps := []time.Duration{
		40 * time.Millisecond,  // n=2: base × 2^0
		80 * time.Millisecond,  // n=3: base × 2^1
		160 * time.Millisecond, // n=4: base × 2^2
		200 * time.Millisecond, // n=5: base × 2^3 = 320ms → capped
		200 * time.Millisecond, // n=6: stays capped
	}
	for _, want := range steps {
		res, err = lk.Fail(ctx, "u")
		require.NoError(t, err)
		require.True(t, res.Locked)
		require.Equal(t, want, res.RetryAfter)
		require.False(t, res.Until.IsZero())
		time.Sleep(want + 60*time.Millisecond) // let the marker expire
	}
}

func TestFailFactorOneFixedLocks(t *testing.T) {
	t.Parallel()
	lk := newLocker(t,
		lockout.WithThreshold(1),
		lockout.WithBaseLock(40*time.Millisecond),
		lockout.WithFactor(1),
		lockout.WithMaxLock(40*time.Millisecond),
		lockout.WithWindow(5*time.Second),
	)
	ctx := context.Background()
	_, err := lk.Fail(ctx, "u")
	require.NoError(t, err)
	for range 3 {
		res, err := lk.Fail(ctx, "u")
		require.NoError(t, err)
		require.True(t, res.Locked)
		require.Equal(t, 40*time.Millisecond, res.RetryAfter)
		time.Sleep(100 * time.Millisecond)
	}
}

func TestFailWhileLockedDoesNotExtend(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(1)) // base 1m: lock outlives the test
	ctx := context.Background()

	_, err := lk.Fail(ctx, "u") // n=1: free
	require.NoError(t, err)
	first, err := lk.Fail(ctx, "u") // n=2: locks for 1m
	require.NoError(t, err)
	require.True(t, first.Locked)
	require.EqualValues(t, 2, first.Failures)

	again, err := lk.Fail(ctx, "u") // n=3: counted, lock unchanged
	require.NoError(t, err)
	require.True(t, again.Locked)
	require.EqualValues(t, 3, again.Failures)
	require.Equal(t, first.Until.UnixMilli(), again.Until.UnixMilli())
	require.LessOrEqual(t, again.RetryAfter, first.RetryAfter)
}

func TestAllowLocked(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(1))
	ctx := context.Background()
	_, err := lk.Fail(ctx, "u")
	require.NoError(t, err)
	_, err = lk.Fail(ctx, "u")
	require.NoError(t, err)

	res, err := lk.Allow(ctx, "u")
	require.NoError(t, err)
	require.True(t, res.Locked)
	require.Positive(t, res.RetryAfter)
	require.LessOrEqual(t, res.RetryAfter, time.Minute)
	// Locked path skips the counter read: Failures/Remaining stay zero.
	require.EqualValues(t, 0, res.Failures)
	require.EqualValues(t, 0, res.Remaining)
}

// A marker whose embedded expiry has passed is treated as unlocked even if
// the store TTL has not fired yet (cross-node clock skew edge).
func TestAllowSkewedMarkerUnlocked(t *testing.T) {
	t.Parallel()
	mock := clock.NewMock(time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC))
	lk := newLocker(t, lockout.WithThreshold(1), lockout.WithClock(mock))
	ctx := context.Background()
	_, err := lk.Fail(ctx, "u")
	require.NoError(t, err)
	_, err = lk.Fail(ctx, "u") // locks until mock-now + 1m; real store TTL 1m
	require.NoError(t, err)

	mock.Advance(2 * time.Minute) // past the embedded expiry, store TTL still live
	res, err := lk.Allow(ctx, "u")
	require.NoError(t, err)
	require.False(t, res.Locked)
	require.EqualValues(t, 2, res.Failures)
}

func TestLockExpiryUnlocks(t *testing.T) {
	t.Parallel()
	lk := newLocker(t,
		lockout.WithThreshold(1),
		lockout.WithBaseLock(100*time.Millisecond),
		lockout.WithMaxLock(100*time.Millisecond),
		lockout.WithWindow(5*time.Second),
	)
	ctx := context.Background()
	_, err := lk.Fail(ctx, "u")
	require.NoError(t, err)
	res, err := lk.Fail(ctx, "u")
	require.NoError(t, err)
	require.True(t, res.Locked)

	time.Sleep(250 * time.Millisecond)
	out, err := lk.Allow(ctx, "u")
	require.NoError(t, err)
	require.False(t, out.Locked)
	require.EqualValues(t, 2, out.Failures) // window still remembers
}

func TestWindowExpiryForgets(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(3), lockout.WithWindow(100*time.Millisecond))
	ctx := context.Background()
	_, err := lk.Fail(ctx, "u")
	require.NoError(t, err)
	_, err = lk.Fail(ctx, "u")
	require.NoError(t, err)

	time.Sleep(250 * time.Millisecond)
	res, err := lk.Allow(ctx, "u")
	require.NoError(t, err)
	require.EqualValues(t, 0, res.Failures)
	require.EqualValues(t, 3, res.Remaining)
}

func TestResetClearsBoth(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(1))
	ctx := context.Background()
	_, err := lk.Fail(ctx, "u")
	require.NoError(t, err)
	_, err = lk.Fail(ctx, "u")
	require.NoError(t, err)

	require.NoError(t, lk.Reset(ctx, "u"))
	res, err := lk.Allow(ctx, "u")
	require.NoError(t, err)
	require.False(t, res.Locked)
	require.EqualValues(t, 0, res.Failures)
	require.EqualValues(t, 1, res.Remaining)
}

func TestConcurrentThresholdCross(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(1)) // base 1m: no expiry mid-test
	ctx := context.Background()

	const workers = 20
	results := make([]lockout.Result, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Go(func() {
			results[i], errs[i] = lk.Fail(ctx, "u")
		})
	}
	wg.Wait()

	var locked int
	var until int64
	for i := range workers {
		require.NoError(t, errs[i])
		if !results[i].Locked {
			continue // the single n=1 free failure
		}
		locked++
		require.Positive(t, results[i].RetryAfter)
		if until == 0 {
			until = results[i].Until.UnixMilli()
		}
		require.Equal(t, until, results[i].Until.UnixMilli(), "all lockers must agree on the winner's expiry")
	}
	require.Equal(t, workers-1, locked)
}

func TestKeysAreIndependent(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(1))
	ctx := context.Background()
	_, err := lk.Fail(ctx, "a@example.com")
	require.NoError(t, err)
	_, err = lk.Fail(ctx, "a@example.com")
	require.NoError(t, err)

	res, err := lk.Allow(ctx, "b@example.com")
	require.NoError(t, err)
	require.False(t, res.Locked)
	require.EqualValues(t, 0, res.Failures)
}

func TestStoreErrorWrapsErrStore(t *testing.T) {
	t.Parallel()
	lk, err := lockout.New(failingCounters{}, failingLocks{})
	require.NoError(t, err)
	ctx := context.Background()

	_, aerr := lk.Allow(ctx, "u")
	require.ErrorIs(t, aerr, lockout.ErrStore)
	_, ferr := lk.Fail(ctx, "u")
	require.ErrorIs(t, ferr, lockout.ErrStore)
	require.ErrorIs(t, lk.Reset(ctx, "u"), lockout.ErrStore)
}

var errBoom = errors.New("boom")

type failingCounters struct{}

func (failingCounters) Incr(context.Context, string, int64, time.Duration) (int64, error) {
	return 0, errBoom
}
func (failingCounters) Get(context.Context, string) (int64, error) { return 0, errBoom }
func (failingCounters) Reset(context.Context, string) error        { return errBoom }
func (failingCounters) Close() error                               { return nil }

type failingLocks struct{}

func (failingLocks) Get(context.Context, string) ([]byte, error) { return nil, errBoom }
func (failingLocks) Set(context.Context, string, []byte, ...cache.SetOption) error {
	return errBoom
}
func (failingLocks) Delete(context.Context, string) error       { return errBoom }
func (failingLocks) Has(context.Context, string) (bool, error)  { return false, errBoom }
func (failingLocks) DeletePrefix(context.Context, string) error { return errBoom }
func (failingLocks) Close() error                               { return nil }
```

(`stretchr/testify v1.11.1` is a direct dependency, used across `auth/apikey` and resilience tests — `require` style is house standard.)

`auth/lockout/tenancy_test.go`:

```go
package lockout_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dmitrymomot/forge/auth/lockout"
	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/resilience/ratelimit"
	"github.com/stretchr/testify/require"
)

var tenantKey = ctxkey.New[string]("tenant")

func scopeFromCtx(ctx context.Context) (string, error) {
	s, _ := tenantKey.From(ctx)
	return s, nil // empty when absent → Locker fails closed
}

func newScopedLocker(t *testing.T) *lockout.Locker {
	t.Helper()
	counters := ratelimit.NewMemoryStore()
	t.Cleanup(func() { _ = counters.Close() })
	locks := cache.NewMemoryStore()
	t.Cleanup(func() { _ = locks.Close() })
	lk, err := lockout.New(counters, locks,
		lockout.WithThreshold(1), lockout.WithScope(scopeFromCtx))
	require.NoError(t, err)
	return lk
}

func TestScopeIsolation(t *testing.T) {
	t.Parallel()
	lk := newScopedLocker(t)
	ctxA := tenantKey.With(context.Background(), "tenant-a")
	ctxB := tenantKey.With(context.Background(), "tenant-b")

	_, err := lk.Fail(ctxA, "user@example.com")
	require.NoError(t, err)
	resA, err := lk.Fail(ctxA, "user@example.com")
	require.NoError(t, err)
	require.True(t, resA.Locked)

	resB, err := lk.Allow(ctxB, "user@example.com")
	require.NoError(t, err)
	require.False(t, resB.Locked)
	require.EqualValues(t, 0, resB.Failures)
}

func TestScopeFailClosed(t *testing.T) {
	t.Parallel()
	lk := newScopedLocker(t)
	ctx := context.Background() // no tenant in context → empty scope

	_, err := lk.Allow(ctx, "u")
	require.ErrorIs(t, err, lockout.ErrScope)
	_, err = lk.Fail(ctx, "u")
	require.ErrorIs(t, err, lockout.ErrScope)
	require.ErrorIs(t, lk.Reset(ctx, "u"), lockout.ErrScope)
}

func TestScopeHookError(t *testing.T) {
	t.Parallel()
	counters := ratelimit.NewMemoryStore()
	t.Cleanup(func() { _ = counters.Close() })
	locks := cache.NewMemoryStore()
	t.Cleanup(func() { _ = locks.Close() })
	hookErr := errors.New("no tenant")
	lk, err := lockout.New(counters, locks,
		lockout.WithScope(func(context.Context) (string, error) { return "", hookErr }))
	require.NoError(t, err)

	_, aerr := lk.Allow(context.Background(), "u")
	require.ErrorIs(t, aerr, lockout.ErrScope)
	require.ErrorIs(t, aerr, hookErr)
}

func TestUnscopedSingleTenant(t *testing.T) {
	t.Parallel()
	counters := ratelimit.NewMemoryStore()
	t.Cleanup(func() { _ = counters.Close() })
	locks := cache.NewMemoryStore()
	t.Cleanup(func() { _ = locks.Close() })
	lk, err := lockout.New(counters, locks, lockout.WithThreshold(1))
	require.NoError(t, err)

	// No WithScope: plain context works, zero ceremony.
	res, err := lk.Fail(context.Background(), "u")
	require.NoError(t, err)
	require.False(t, res.Locked)
}
```

`auth/lockout/lockduration_internal_test.go` (white-box: pure math on unexported `lockDuration`):

```go
package lockout

import (
	"testing"
	"time"
)

func TestLockDurationClampsOverflow(t *testing.T) {
	t.Parallel()
	l := &Locker{cfg: config{
		threshold: 1,
		baseLock:  time.Minute,
		factor:    2,
		maxLock:   15 * time.Minute,
	}}
	tests := []struct {
		n    int64
		want time.Duration
	}{
		{2, time.Minute},          // 2^0
		{3, 2 * time.Minute},      // 2^1
		{5, 8 * time.Minute},      // 2^3
		{6, 15 * time.Minute},     // 16m → cap
		{100, 15 * time.Minute},   // huge exponent → cap, no overflow
		{1 << 62, 15 * time.Minute}, // float Inf → cap, no panic
	}
	for _, tt := range tests {
		if got := l.lockDuration(tt.n); got != tt.want {
			t.Errorf("lockDuration(%d) = %v, want %v", tt.n, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./auth/lockout/... 2>&1 | head -20`
Expected: compile errors — `package lockout` does not exist yet.

- [ ] **Step 3: Write the implementation**

`auth/lockout/errors.go`:

```go
package lockout

import "errors"

var (
	// ErrLocked reports that the identity is currently locked out. Only Do
	// returns it (wrapped in *LockedError); Allow and Fail report locked
	// state via Result instead.
	ErrLocked = errors.New("lockout: locked")
	// ErrFailedAttempt marks an authentication failure inside a Do callback:
	// return it (or an error wrapping it) to count the attempt. Any other
	// error passes through uncounted.
	ErrFailedAttempt = errors.New("lockout: failed attempt")
	// ErrScope reports a configured scope hook that failed or returned empty.
	ErrScope = errors.New("lockout: scope resolution failed")
	// ErrStore wraps failures of the underlying counter or lock stores.
	ErrStore = errors.New("lockout: store operation failed")
)
```

(`LockedError` is added in Task 2 with `Do`.)

`auth/lockout/options.go`:

```go
package lockout

import (
	"context"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

type config struct {
	scope     func(context.Context) (string, error)
	clk       clock.Clock
	threshold int64
	baseLock  time.Duration
	maxLock   time.Duration
	window    time.Duration
	factor    float64
}

// Option configures a Locker.
type Option func(*config)

// WithThreshold sets the number of free failures before the first lock.
// Default 5.
func WithThreshold(n int) Option { return func(c *config) { c.threshold = int64(n) } }

// WithBaseLock sets the first lock duration. Default 1 minute.
func WithBaseLock(d time.Duration) Option { return func(c *config) { c.baseLock = d } }

// WithFactor sets the escalation multiplier applied to each lock after the
// first; 1.0 means fixed-duration locks. Default 2.0.
func WithFactor(f float64) Option { return func(c *config) { c.factor = f } }

// WithMaxLock caps the escalated lock duration. Default 15 minutes.
func WithMaxLock(d time.Duration) Option { return func(c *config) { c.maxLock = d } }

// WithWindow sets the failure-memory window. The counter's TTL is fixed when
// the first failure of a burst creates it (Incr never extends a live TTL):
// keep window >= max lock or escalation memory expires before the last lock
// does. Default 30 minutes.
func WithWindow(d time.Duration) Option { return func(c *config) { c.window = d } }

// WithClock injects a clock (for tests). Default clock.System().
func WithClock(clk clock.Clock) Option {
	return func(c *config) {
		if clk != nil {
			c.clk = clk
		}
	}
}

// WithScope derives a tenant scope from the request context on every call and
// isolates all keys per scope. An error or empty scope fails closed with
// ErrScope. Without this option keys are unscoped (single-tenant).
func WithScope(fn func(context.Context) (string, error)) Option {
	return func(c *config) { c.scope = fn }
}
```

`auth/lockout/keys.go`:

```go
package lockout

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// keys composes the failure-counter and lock-marker store keys for one caller
// identity. The caller key is SHA-256 hashed (first 16 bytes, hex) to keep
// PII out of store key listings and make arbitrary bytes store-safe —
// hygiene, not secrecy. With a scope hook configured the scope becomes a key
// segment; resolution failure or an empty scope fails closed with ErrScope.
func (l *Locker) keys(ctx context.Context, key string) (fails, lock string, err error) {
	scope := ""
	if l.cfg.scope != nil {
		s, err := l.cfg.scope(ctx)
		if err != nil {
			return "", "", fmt.Errorf("%w: %w", ErrScope, err)
		}
		if s == "" {
			return "", "", ErrScope
		}
		scope = s + ":"
	}
	sum := sha256.Sum256([]byte(key))
	h := hex.EncodeToString(sum[:16])
	return "lockout:" + scope + "f:" + h, "lockout:" + scope + "l:" + h, nil
}
```

`auth/lockout/lockout.go`:

```go
package lockout

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/resilience/ratelimit"
)

// Result reports the lockout state of one identity.
type Result struct {
	Until      time.Time     // lock expiry; zero when unlocked
	RetryAfter time.Duration // >0 when Locked
	Failures   int64         // failures recorded in the current memory window
	Remaining  int64         // free attempts left before the next lock
	Locked     bool
}

// Locker tracks authentication failures per identity and escalates lockout
// windows. Failure counts ride the ratelimit counter seam; lock markers ride
// the cache TTL-KV seam. Store lifecycles remain the caller's.
type Locker struct {
	counters ratelimit.Store
	locks    cache.Store
	cfg      config
}

// New builds a Locker over the two stores. See the With* options for the
// escalation defaults (5 free failures, then 1m locks doubling up to 15m,
// failures remembered for 30m).
func New(counters ratelimit.Store, locks cache.Store, opts ...Option) (*Locker, error) {
	cfg := config{
		threshold: 5,
		baseLock:  time.Minute,
		factor:    2.0,
		maxLock:   15 * time.Minute,
		window:    30 * time.Minute,
		clk:       clock.System(),
	}
	for _, o := range opts {
		o(&cfg)
	}
	switch {
	case counters == nil:
		return nil, errors.New("lockout: nil counter store")
	case locks == nil:
		return nil, errors.New("lockout: nil lock store")
	case cfg.threshold < 1:
		return nil, errors.New("lockout: threshold must be >= 1")
	case cfg.baseLock <= 0:
		return nil, errors.New("lockout: base lock must be > 0")
	case cfg.factor < 1:
		return nil, errors.New("lockout: factor must be >= 1")
	case cfg.maxLock < cfg.baseLock:
		return nil, errors.New("lockout: max lock must be >= base lock")
	case cfg.window <= 0:
		return nil, errors.New("lockout: window must be > 0")
	}
	return &Locker{counters: counters, locks: locks, cfg: cfg}, nil
}

// Allow reports whether key may attempt authentication. It is a read-only
// pre-check: a parallel burst may pass Allow before a lock lands, but the
// lock still lands exactly once (see Fail). On the locked path Failures and
// Remaining stay zero — no second store round-trip.
func (l *Locker) Allow(ctx context.Context, key string) (Result, error) {
	failsKey, lockKey, err := l.keys(ctx, key)
	if err != nil {
		return Result{}, err
	}
	res, locked, err := l.lockState(ctx, lockKey)
	if err != nil {
		return Result{}, err
	}
	if locked {
		return res, nil
	}
	n, err := l.counters.Get(ctx, failsKey)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrStore, err)
	}
	return Result{Failures: n, Remaining: max(l.cfg.threshold-n, 0)}, nil
}

// Fail records one authentication failure for key. Crossing the threshold
// creates a lock whose duration escalates with the failure count; exactly one
// concurrent crosser creates the marker (SetNX), losers report the winner's
// expiry. A Fail while already locked increments the counter (escalating the
// next lock) but never extends the current one.
func (l *Locker) Fail(ctx context.Context, key string) (Result, error) {
	failsKey, lockKey, err := l.keys(ctx, key)
	if err != nil {
		return Result{}, err
	}
	n, err := l.counters.Incr(ctx, failsKey, 1, l.cfg.window)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrStore, err)
	}
	if n <= l.cfg.threshold {
		return Result{Failures: n, Remaining: l.cfg.threshold - n}, nil
	}

	dur := l.lockDuration(n)
	until := l.cfg.clk.Now().Add(dur)
	val := strconv.AppendInt(nil, until.UnixMilli(), 10)
	err = l.locks.Set(ctx, lockKey, val, cache.WithTTL(dur), cache.WithSetNonExist())
	switch {
	case err == nil:
		return Result{Locked: true, RetryAfter: dur, Until: until, Failures: n}, nil
	case errors.Is(err, cache.ErrExists):
		res, locked, err := l.lockState(ctx, lockKey)
		if err != nil {
			return Result{}, err
		}
		if !locked {
			// The concurrent winner's marker expired between SetNX and Get:
			// report unlocked; the next failure locks again.
			return Result{Failures: n}, nil
		}
		res.Failures = n
		return res, nil
	default:
		return Result{}, fmt.Errorf("%w: %w", ErrStore, err)
	}
}

// Reset clears the failure count and any active lock for key. Call it after
// successful authentication.
func (l *Locker) Reset(ctx context.Context, key string) error {
	failsKey, lockKey, err := l.keys(ctx, key)
	if err != nil {
		return err
	}
	if err := errors.Join(l.counters.Reset(ctx, failsKey), l.locks.Delete(ctx, lockKey)); err != nil {
		return fmt.Errorf("%w: %w", ErrStore, err)
	}
	return nil
}

// lockState reads the lock marker. A marker whose embedded expiry has passed
// is treated as unlocked even when its store TTL has not fired yet (clock
// skew across nodes).
func (l *Locker) lockState(ctx context.Context, lockKey string) (Result, bool, error) {
	raw, err := l.locks.Get(ctx, lockKey)
	if errors.Is(err, cache.ErrNotFound) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, fmt.Errorf("%w: %w", ErrStore, err)
	}
	ms, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil {
		return Result{}, false, fmt.Errorf("%w: malformed lock marker: %w", ErrStore, err)
	}
	until := time.UnixMilli(ms)
	now := l.cfg.clk.Now()
	if !until.After(now) {
		return Result{}, false, nil
	}
	return Result{Locked: true, RetryAfter: until.Sub(now), Until: until}, true, nil
}

// lockDuration computes min(base × factor^(n-threshold-1), maxLock), clamping
// before the float→Duration conversion so huge counts cannot overflow.
func (l *Locker) lockDuration(n int64) time.Duration {
	exp := float64(n - l.cfg.threshold - 1)
	d := float64(l.cfg.baseLock) * math.Pow(l.cfg.factor, exp)
	if d >= float64(l.cfg.maxLock) || math.IsInf(d, 1) {
		return l.cfg.maxLock
	}
	return time.Duration(d)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `just test ./auth/lockout/...`
Expected: PASS, race-clean. The sleep-based tests (`Escalation`, `FactorOne`, `LockExpiry`, `WindowExpiry`) total under ~1.5s.

- [ ] **Step 5: Format and commit**

```bash
just fmt ./auth/lockout/...
git add auth/lockout/
git commit -m "feat(lockout): core Locker with escalating lockout windows"
```

---

### Task 2: Do wrapper + LockedError

**Files:**
- Create: `auth/lockout/do.go`
- Modify: `auth/lockout/errors.go` (append `LockedError`)
- Test: `auth/lockout/do_test.go`

**Interfaces:**
- Consumes (from Task 1): `(*Locker).Allow/Fail/Reset`, `Result`, `ErrLocked`, `ErrFailedAttempt`, `ErrStore`. Also `ratelimit.Store`/`cache.Store` signatures from Task 1's Consumes block (for test stubs).
- Produces:
  - `func (l *Locker) Do(ctx context.Context, key string, fn func(ctx context.Context) error) error`
  - `type LockedError struct { Result Result; Err error }` with `Error() string` and `Unwrap() []error`

- [ ] **Step 1: Write the failing tests**

`auth/lockout/do_test.go`:

```go
package lockout_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/lockout"
	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/resilience/ratelimit"
	"github.com/stretchr/testify/require"
)

func TestDoSuccessResets(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(2))
	ctx := context.Background()
	_, err := lk.Fail(ctx, "u")
	require.NoError(t, err)

	require.NoError(t, lk.Do(ctx, "u", func(context.Context) error { return nil }))

	res, err := lk.Allow(ctx, "u")
	require.NoError(t, err)
	require.EqualValues(t, 0, res.Failures) // success cleared the slate
}

func TestDoFailedAttemptCounted(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(3))
	ctx := context.Background()

	wrapped := fmt.Errorf("bad password: %w", lockout.ErrFailedAttempt)
	err := lk.Do(ctx, "u", func(context.Context) error { return wrapped })
	require.ErrorIs(t, err, lockout.ErrFailedAttempt)
	require.NotErrorIs(t, err, lockout.ErrLocked)
	require.Equal(t, wrapped, err) // below threshold: fn error passes through unchanged

	res, aerr := lk.Allow(ctx, "u")
	require.NoError(t, aerr)
	require.EqualValues(t, 1, res.Failures)
}

func TestDoCrossingThresholdReturnsLockedError(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(1))
	ctx := context.Background()
	_, err := lk.Fail(ctx, "u") // n=1: at threshold, next failure locks
	require.NoError(t, err)

	err = lk.Do(ctx, "u", func(context.Context) error { return lockout.ErrFailedAttempt })
	require.ErrorIs(t, err, lockout.ErrLocked)
	require.ErrorIs(t, err, lockout.ErrFailedAttempt)

	var le *lockout.LockedError
	require.ErrorAs(t, err, &le)
	require.True(t, le.Result.Locked)
	require.Equal(t, time.Minute, le.Result.RetryAfter)
	require.EqualValues(t, 2, le.Result.Failures)
}

func TestDoLockedOnEntry(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(1))
	ctx := context.Background()
	_, err := lk.Fail(ctx, "u")
	require.NoError(t, err)
	_, err = lk.Fail(ctx, "u") // locked for 1m
	require.NoError(t, err)

	ran := false
	err = lk.Do(ctx, "u", func(context.Context) error { ran = true; return nil })
	require.ErrorIs(t, err, lockout.ErrLocked)
	require.NotErrorIs(t, err, lockout.ErrFailedAttempt)
	require.False(t, ran, "fn must not run while locked")

	var le *lockout.LockedError
	require.ErrorAs(t, err, &le)
	require.Positive(t, le.Result.RetryAfter)
}

func TestDoInfraErrorPassthroughUncounted(t *testing.T) {
	t.Parallel()
	lk := newLocker(t)
	ctx := context.Background()

	infra := errors.New("db down")
	err := lk.Do(ctx, "u", func(context.Context) error { return infra })
	require.Equal(t, infra, err) // untouched
	require.NotErrorIs(t, err, lockout.ErrLocked)

	res, aerr := lk.Allow(ctx, "u")
	require.NoError(t, aerr)
	require.EqualValues(t, 0, res.Failures) // not counted
}

func TestDoResetFailureSurfaced(t *testing.T) {
	t.Parallel()
	locks := cache.NewMemoryStore()
	t.Cleanup(func() { _ = locks.Close() })
	lk, err := lockout.New(resetFailingCounters{inner: ratelimit.NewMemoryStore()}, locks)
	require.NoError(t, err)

	err = lk.Do(context.Background(), "u", func(context.Context) error { return nil })
	require.ErrorIs(t, err, lockout.ErrStore) // reset failure is never swallowed
}

type resetFailingCounters struct{ inner ratelimit.Store }

func (s resetFailingCounters) Incr(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	return s.inner.Incr(ctx, key, delta, ttl)
}
func (s resetFailingCounters) Get(ctx context.Context, key string) (int64, error) {
	return s.inner.Get(ctx, key)
}
func (s resetFailingCounters) Reset(context.Context, string) error {
	return errors.New("reset boom")
}
func (s resetFailingCounters) Close() error { return s.inner.Close() }
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./auth/lockout/... 2>&1 | head -10`
Expected: compile error — `lk.Do` and `lockout.LockedError` undefined.

- [ ] **Step 3: Write the implementation**

Append to `auth/lockout/errors.go`:

```go
// LockedError carries lock details when Do rejects a locked identity or when
// a counted failure crosses the threshold. errors.Is(err, ErrLocked) always
// matches; when a failed attempt triggered the lock, the fn error chain
// (including ErrFailedAttempt) matches too.
type LockedError struct {
	Err    error // fn error that triggered the lock; nil when locked on entry
	Result Result
}

func (e *LockedError) Error() string {
	if e.Err != nil {
		return ErrLocked.Error() + ": " + e.Err.Error()
	}
	return ErrLocked.Error()
}

func (e *LockedError) Unwrap() []error {
	if e.Err != nil {
		return []error{ErrLocked, e.Err}
	}
	return []error{ErrLocked}
}
```

`auth/lockout/do.go`:

```go
package lockout

import (
	"context"
	"errors"
)

// Do wraps one authentication attempt: it rejects locked identities with a
// *LockedError before calling fn, counts failures fn reports, and resets the
// slate when fn succeeds.
//
// Classification: fn errors matching ErrFailedAttempt (errors.Is) are counted
// as failures; if the count crosses the threshold Do returns a *LockedError
// wrapping the fn error, otherwise the fn error passes through unchanged.
// Every other error passes through uncounted, so infrastructure failures can
// never lock a user out. A failed post-success Reset is returned as an
// ErrStore-wrapped error rather than swallowed.
func (l *Locker) Do(ctx context.Context, key string, fn func(ctx context.Context) error) error {
	res, err := l.Allow(ctx, key)
	if err != nil {
		return err
	}
	if res.Locked {
		return &LockedError{Result: res}
	}

	err = fn(ctx)
	switch {
	case err == nil:
		return l.Reset(ctx, key)
	case errors.Is(err, ErrFailedAttempt):
		fres, ferr := l.Fail(ctx, key)
		if ferr != nil {
			return errors.Join(err, ferr)
		}
		if fres.Locked {
			return &LockedError{Result: fres, Err: err}
		}
		return err
	default:
		return err
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `just test ./auth/lockout/...`
Expected: PASS, race-clean.

- [ ] **Step 5: Format and commit**

```bash
just fmt ./auth/lockout/...
git add auth/lockout/
git commit -m "feat(lockout): Do wrapper with sentinel-classified failure counting"
```

---

### Task 3: HTTP middleware

**Files:**
- Create: `auth/lockout/middleware.go`
- Test: `auth/lockout/middleware_test.go`

**Interfaces:**
- Consumes (from Task 1): `(*Locker).Allow`, `Result`, `ErrLocked`; `middleware.Middleware = func(http.Handler) http.Handler` (`github.com/dmitrymomot/forge/web/middleware`); `ctxkey.New[string](name)` / `key.With(ctx, v)` / `key.From(ctx)` (`github.com/dmitrymomot/forge/core/ctxkey`).
- Produces:
  - `type KeyFunc func(*http.Request) string`
  - `func (l *Locker) Middleware(key KeyFunc, opts ...MiddlewareOption) middleware.Middleware`
  - `type MiddlewareOption func(*middlewareConfig)` with `WithResponder(func(http.ResponseWriter, *http.Request, Result))` and `WithFailOpen()`
  - `func KeyFromContext(ctx context.Context) (string, bool)`

- [ ] **Step 1: Write the failing tests**

`auth/lockout/middleware_test.go`:

```go
package lockout_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/auth/lockout"
	"github.com/stretchr/testify/require"
)

func mwRequest(t *testing.T, mw func(http.Handler) http.Handler, next http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	mw(next).ServeHTTP(rec, req)
	return rec
}

func keyU(*http.Request) string { return "u@example.com" }

func TestMiddlewarePassesUnlocked(t *testing.T) {
	t.Parallel()
	lk := newLocker(t)
	var gotKey string
	var gotOK bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotOK = lockout.KeyFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	rec := mwRequest(t, lk.Middleware(keyU), next)
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, gotOK)
	require.Equal(t, "u@example.com", gotKey)
}

func TestMiddlewareBlocksLocked(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(1))
	ctx := context.Background()
	_, err := lk.Fail(ctx, "u@example.com")
	require.NoError(t, err)
	_, err = lk.Fail(ctx, "u@example.com") // locked for 1m
	require.NoError(t, err)

	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next must not run while locked")
	})
	rec := mwRequest(t, lk.Middleware(keyU), next)
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.NotEmpty(t, rec.Header().Get("Retry-After"))
}

func TestMiddlewareEmptyKeySkips(t *testing.T) {
	t.Parallel()
	lk := newLocker(t)
	var ok bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, ok = lockout.KeyFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	rec := mwRequest(t, lk.Middleware(func(*http.Request) string { return "" }), next)
	require.Equal(t, http.StatusOK, rec.Code)
	require.False(t, ok, "no key must be stashed when the check is skipped")
}

func TestMiddlewareFailsClosedOnStoreError(t *testing.T) {
	t.Parallel()
	lk, err := lockout.New(failingCounters{}, failingLocks{})
	require.NoError(t, err)
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next must not run on store error (fail closed)")
	})

	rec := mwRequest(t, lk.Middleware(keyU), next)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestMiddlewareFailOpenOption(t *testing.T) {
	t.Parallel()
	lk, err := lockout.New(failingCounters{}, failingLocks{})
	require.NoError(t, err)
	next := http.HandlerFunc(func(w http.ResponseWriter, *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := mwRequest(t, lk.Middleware(keyU, lockout.WithFailOpen()), next)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestMiddlewareCustomResponder(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(1))
	ctx := context.Background()
	_, err := lk.Fail(ctx, "u@example.com")
	require.NoError(t, err)
	_, err = lk.Fail(ctx, "u@example.com")
	require.NoError(t, err)

	responder := func(w http.ResponseWriter, _ *http.Request, res lockout.Result) {
		require.True(t, res.Locked)
		http.Error(w, "custom", http.StatusLocked)
	}
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	rec := mwRequest(t, lk.Middleware(keyU, lockout.WithResponder(responder)), next)
	require.Equal(t, http.StatusLocked, rec.Code)
	require.NotEmpty(t, rec.Header().Get("Retry-After"), "Retry-After set before the responder runs")
}
```

(`failingCounters`/`failingLocks` are the stubs already defined in `lockout_test.go`, Task 1.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./auth/lockout/... 2>&1 | head -10`
Expected: compile error — `Middleware`, `KeyFromContext`, `WithFailOpen`, `WithResponder` undefined.

- [ ] **Step 3: Write the implementation**

`auth/lockout/middleware.go`:

```go
package lockout

import (
	"context"
	"math"
	"net/http"
	"strconv"

	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/web/middleware"
)

// KeyFunc selects the lockout key for a request — e.g. a form email
// (r.PostFormValue parses and caches the form, so the handler's own
// PostFormValue calls still work) or a client IP. An empty key skips the
// check entirely; the handler's own validation rejects malformed logins.
// JSON-body extraction (read + restore r.Body) is the app's responsibility.
type KeyFunc func(*http.Request) string

var requestKey = ctxkey.New[string]("lockout")

// KeyFromContext returns the lockout key the middleware extracted for this
// request, so handlers call Fail/Reset with the identical key.
func KeyFromContext(ctx context.Context) (string, bool) {
	return requestKey.From(ctx)
}

type middlewareConfig struct {
	responder func(http.ResponseWriter, *http.Request, Result)
	failOpen  bool
}

// MiddlewareOption configures Middleware.
type MiddlewareOption func(*middlewareConfig)

// WithResponder overrides the 429 response (default plain text). Use it to
// emit problem+json via web/problem. Retry-After is already set when it runs.
func WithResponder(fn func(http.ResponseWriter, *http.Request, Result)) MiddlewareOption {
	return func(c *middlewareConfig) {
		if fn != nil {
			c.responder = fn
		}
	}
}

// WithFailOpen serves requests when the lockout check errors instead of
// returning 503. The default fails closed: brute-force protection must not
// silently disable during a store outage.
func WithFailOpen() MiddlewareOption {
	return func(c *middlewareConfig) { c.failOpen = true }
}

// Middleware gates requests on the lockout state of key(r): locked requests
// get 429 with Retry-After; unlocked requests proceed with the extracted key
// stashed in the context (KeyFromContext). It covers only the Allow half —
// handlers still call Fail/Reset with the attempt's outcome.
func (l *Locker) Middleware(key KeyFunc, opts ...MiddlewareOption) middleware.Middleware {
	cfg := middlewareConfig{responder: defaultResponder}
	for _, o := range opts {
		o(&cfg)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			k := key(r)
			if k == "" {
				next.ServeHTTP(w, r)
				return
			}
			res, err := l.Allow(r.Context(), k)
			if err != nil {
				if cfg.failOpen {
					next.ServeHTTP(w, r)
					return
				}
				http.Error(w, "service unavailable", http.StatusServiceUnavailable)
				return
			}
			if res.Locked {
				w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(res.RetryAfter.Seconds()))))
				cfg.responder(w, r, res)
				return
			}
			next.ServeHTTP(w, r.WithContext(requestKey.With(r.Context(), k)))
		})
	}
}

func defaultResponder(w http.ResponseWriter, _ *http.Request, _ Result) {
	http.Error(w, ErrLocked.Error(), http.StatusTooManyRequests)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `just test ./auth/lockout/...`
Expected: PASS, race-clean.

- [ ] **Step 5: Format and commit**

```bash
just fmt ./auth/lockout/...
git add auth/lockout/
git commit -m "feat(lockout): fail-closed HTTP middleware with key extractor"
```

---

### Task 4: doc.go, fuzz test, roadmap entry removal

**Files:**
- Create: `auth/lockout/doc.go`
- Test: `auth/lockout/fuzz_test.go`
- Modify: `docs/packages.md` (delete the `auth/lockout` entry — roadmap lists only unbuilt packages)

**Interfaces:**
- Consumes: everything produced by Tasks 1–3 (exact signatures in those tasks' Produces blocks).
- Produces: package documentation only; no new API.

- [ ] **Step 1: Write the fuzz test**

`auth/lockout/fuzz_test.go`:

```go
package lockout_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/lockout"
	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/resilience/ratelimit"
)

var hashSuffix = regexp.MustCompile(`^[0-9a-f]{32}$`)

// recordingCounters/recordingLocks capture the composed store keys so the
// fuzz target can assert key-shape invariants black-box.
type recordingCounters struct {
	ratelimit.Store
	keys *[]string
}

func (r recordingCounters) Incr(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	*r.keys = append(*r.keys, key)
	return r.Store.Incr(ctx, key, delta, ttl)
}

func (r recordingCounters) Get(ctx context.Context, key string) (int64, error) {
	*r.keys = append(*r.keys, key)
	return r.Store.Get(ctx, key)
}

type recordingLocks struct {
	cache.Store
	keys *[]string
}

func (r recordingLocks) Get(ctx context.Context, key string) ([]byte, error) {
	*r.keys = append(*r.keys, key)
	return r.Store.Get(ctx, key)
}

func FuzzKeyComposition(f *testing.F) {
	f.Add("user@example.com", "", false)
	f.Add("user@example.com", "tenant-a", true)
	f.Add("", "t", true)
	f.Add("key:with:colons", "scope:with:colons", true)
	f.Add(strings.Repeat("x", 10_000), "t", true)
	f.Add("\x00\xff unicode ⚡", "теnant", true)

	f.Fuzz(func(t *testing.T, key, scope string, scoped bool) {
		var counterKeys, lockKeys []string
		counters := ratelimit.NewMemoryStore()
		defer counters.Close()
		locks := cache.NewMemoryStore()
		defer locks.Close()

		opts := []lockout.Option{lockout.WithThreshold(2)}
		if scoped {
			opts = append(opts, lockout.WithScope(func(context.Context) (string, error) {
				return scope, nil
			}))
		}
		lk, err := lockout.New(
			recordingCounters{Store: counters, keys: &counterKeys},
			recordingLocks{Store: locks, keys: &lockKeys},
			opts...,
		)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		ctx := context.Background()
		res, err := lk.Fail(ctx, key)
		if scoped && scope == "" {
			if !errors.Is(err, lockout.ErrScope) {
				t.Fatalf("empty scope must fail closed, got %v", err)
			}
			return
		}
		if err != nil {
			t.Fatalf("Fail: %v", err)
		}
		if res.Failures != 1 {
			t.Fatalf("Failures = %d, want 1", res.Failures)
		}

		out, err := lk.Allow(ctx, key)
		if err != nil {
			t.Fatalf("Allow: %v", err)
		}
		if out.Failures != 1 {
			t.Fatalf("round-trip Failures = %d, want 1", out.Failures)
		}

		wantPrefix := "lockout:"
		if scoped {
			wantPrefix += scope + ":"
		}
		for _, k := range counterKeys {
			rest, ok := strings.CutPrefix(k, wantPrefix+"f:")
			if !ok || !hashSuffix.MatchString(rest) {
				t.Fatalf("counter key %q lacks shape %sf:<32 hex>", k, wantPrefix)
			}
		}
		for _, k := range lockKeys {
			rest, ok := strings.CutPrefix(k, wantPrefix+"l:")
			if !ok || !hashSuffix.MatchString(rest) {
				t.Fatalf("lock key %q lacks shape %sl:<32 hex>", k, wantPrefix)
			}
		}
		if counterKeys[0] == lockKeys[0] {
			t.Fatalf("counter and lock keys must differ: %q", counterKeys[0])
		}
	})
}
```

- [ ] **Step 2: Run the fuzz seed corpus and a short fuzz session**

Run: `go test -run FuzzKeyComposition ./auth/lockout/` then `go test -fuzz FuzzKeyComposition -fuzztime 30s ./auth/lockout/`
Expected: seeds PASS; 30s fuzz finds no failing input.

- [ ] **Step 3: Write doc.go**

`auth/lockout/doc.go`:

```go
// Package lockout counts authentication failures per identity and escalates
// lockout windows: after a configurable number of free failures, each further
// failure locks the identity for base × factor^k, capped at a maximum
// (factor 1.0 gives fixed-duration locks). It is not rate shaping (that is
// resilience/ratelimit) and not cumulative caps (resilience/quota) —
// failure-triggered escalation only.
//
// Failure counts ride the ratelimit counter seam; lock markers ride the
// cache TTL-KV seam. Both bring their own backends, so no drivers ship here:
//
//	counters := ratelimit.NewMemoryStore() // or ratelimit/redisstore, ratelimit/pgstore
//	locks := cache.NewMemoryStore()        // or cache/redis
//	lk, err := lockout.New(counters, locks,
//		lockout.WithThreshold(5),
//		lockout.WithBaseLock(time.Minute),
//		lockout.WithMaxLock(15*time.Minute),
//	)
//
// The explicit core wires into any login or OTP flow:
//
//	res, err := lk.Allow(ctx, email)
//	if err != nil { /* 500 */ }
//	if res.Locked { /* 429 + Retry-After: res.RetryAfter */ }
//
//	// wrong credentials:
//	res, err = lk.Fail(ctx, email) // res.Locked → "locked for res.RetryAfter"
//
//	// success:
//	err = lk.Reset(ctx, email)
//
// Do wraps the same cycle around a callback; only errors matching
// ErrFailedAttempt count, so infrastructure failures never lock a user out:
//
//	err := lk.Do(ctx, email, func(ctx context.Context) error {
//		if !credentialsValid(ctx) {
//			return lockout.ErrFailedAttempt
//		}
//		return nil
//	})
//
// Middleware gates the Allow half over net/http and stashes the extracted
// key in the context for the handler's Fail/Reset calls:
//
//	mw := lk.Middleware(func(r *http.Request) string { return r.PostFormValue("email") })
//	mux.Handle("POST /login", mw(loginHandler))
//
// It fails closed (503) on store errors by default; WithFailOpen restores
// availability-first behavior.
//
// Caller keys (emails, phones, IPs) are SHA-256 hashed into store keys —
// PII hygiene, not secrecy. Multi-tenant apps set WithScope to derive a
// tenant scope from the context on every call; an error or empty scope fails
// closed with ErrScope. Single-tenant apps omit it and pay zero ceremony.
//
// The failure counter's TTL is fixed when the first failure of a burst
// creates it (the counter seam never extends a live TTL), so keep the window
// at or above the maximum lock — the defaults (30m window, 15m max lock)
// comply — or escalation memory expires before the last lock does.
//
// Out of scope: CAPTCHA hooks, lockout notifications, IP reputation, and
// admin unlock APIs beyond Reset (which is the unlock). Successful-login
// anomaly detection belongs to web/fingerprint.
package lockout
```

- [ ] **Step 4: Delete the auth/lockout entry from docs/packages.md**

Remove the block (around line 623):

```markdown
**auth/lockout**

Login/OTP failure counting with exponential delay and lockout windows over
the ratelimit counter seam. (Not rate shaping — that's `ratelimit`; not
cumulative caps — that's `quota`.)

Deps: `resilience/ratelimit`.

---
```

Delete the whole entry including its trailing `---` separator. If `main` has drifted and the surrounding entries changed, re-locate by the `**auth/lockout**` heading (this file conflicts often; resolve by keeping both sides' deletions).

- [ ] **Step 5: Run all tests, format, commit**

Run: `just test ./auth/lockout/...`
Expected: PASS.

```bash
just fmt ./auth/lockout/...
git add auth/lockout/ docs/packages.md
git commit -m "docs(lockout): package docs, key-composition fuzz, drop roadmap entry"
```

---

### Task 5: Benchmarks + optimization pass + lint

**Files:**
- Test: `auth/lockout/bench_test.go`

**Interfaces:**
- Consumes: `New`, `Allow`, `Fail`, `Reset`, options from Task 1.
- Produces: benchmark numbers for the PR description (before/after any optimization).

- [ ] **Step 1: Write the benchmarks**

`auth/lockout/bench_test.go`:

```go
package lockout_test

import (
	"context"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/lockout"
	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/resilience/ratelimit"
)

func benchLocker(b *testing.B, opts ...lockout.Option) *lockout.Locker {
	b.Helper()
	counters := ratelimit.NewMemoryStore()
	b.Cleanup(func() { _ = counters.Close() })
	locks := cache.NewMemoryStore()
	b.Cleanup(func() { _ = locks.Close() })
	lk, err := lockout.New(counters, locks, opts...)
	if err != nil {
		b.Fatal(err)
	}
	return lk
}

// The hot path: every login attempt pays one Allow.
func BenchmarkAllowUnlocked(b *testing.B) {
	lk := benchLocker(b)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := lk.Allow(ctx, "user@example.com"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAllowLocked(b *testing.B) {
	lk := benchLocker(b, lockout.WithThreshold(1), lockout.WithBaseLock(time.Hour), lockout.WithMaxLock(time.Hour))
	ctx := context.Background()
	if _, err := lk.Fail(ctx, "u"); err != nil {
		b.Fatal(err)
	}
	if _, err := lk.Fail(ctx, "u"); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := lk.Allow(ctx, "u"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFailBelowThreshold(b *testing.B) {
	lk := benchLocker(b, lockout.WithThreshold(1<<40)) // never crosses
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := lk.Fail(ctx, "u"); err != nil {
			b.Fatal(err)
		}
	}
}

// Steady-state Fail after the threshold: SetNX conflict + marker read.
func BenchmarkFailWhileLocked(b *testing.B) {
	lk := benchLocker(b, lockout.WithThreshold(1), lockout.WithBaseLock(time.Hour), lockout.WithMaxLock(time.Hour))
	ctx := context.Background()
	if _, err := lk.Fail(ctx, "u"); err != nil {
		b.Fatal(err)
	}
	if _, err := lk.Fail(ctx, "u"); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := lk.Fail(ctx, "u"); err != nil {
			b.Fatal(err)
		}
	}
}
```

- [ ] **Step 2: Run benchmarks, record BEFORE numbers**

Run: `just bench ./auth/lockout/...`
Expected: all four benchmarks report ns/op, B/op, allocs/op. Save the output — it goes in the PR as the "before" column.

- [ ] **Step 3: Optimization pass (measured wins only)**

Inspect allocs with `go test -bench . -benchmem -memprofile mem.out ./auth/lockout/` if any benchmark shows unexpected allocations. Known candidates, apply ONLY if the numbers confirm them:
- `keys()` string concatenation: pre-size with a single `strings.Builder`/`make([]byte, 0, n)` if concat shows >2 allocs.
- `hex.EncodeToString` + `sha256.Sum256`: both are cheap and non-escaping in current Go; leave alone unless profiling disagrees.

Per repo policy (design.md §Performance): readable first — if no benchmark shows a win, change nothing and record "no optimization needed; numbers already minimal" in the PR. Re-run `just bench ./auth/lockout/...` after any change and save AFTER numbers.

- [ ] **Step 4: Full verification**

Run: `just test ./auth/lockout/...` — PASS, race-clean.
Run: `just lint` — zero findings (vet, golangci-lint, nilaway, betteralign, modernize).
Run: `go test -cover ./auth/lockout/` — expect ≥90% coverage (siblings land 92–99%).

- [ ] **Step 5: Format and commit**

```bash
just fmt ./auth/lockout/...
git add auth/lockout/
git commit -m "test(lockout): benchmarks + optimization pass"
```

---

## Plan self-review (done at write time)

- **Spec coverage:** model/options/defaults (Task 1), state machine incl. SetNX race + skew + fail-while-locked (Task 1), tenancy fail-closed (Task 1), `Do` four-rule contract incl. reset-failure surfacing and double-`errors.Is` (Task 2), middleware incl. fail-closed default, `WithFailOpen`, `WithResponder`, context key, empty-key skip (Task 3), key hygiene + fuzz + doc.go + anti-scope + packages.md removal (Task 4), benchmarks + optimization pass (Task 5). Spec's "Allow locked path leaves Failures zero" asserted in `TestAllowLocked`.
- **Type consistency:** `Result` field order matches across tasks; `LockedError{Err, Result}` construction sites use field names; `middleware.Middleware` return type consistent; stubs implement the full 4-method/6-method store interfaces.
- **Placeholders:** none; every step carries complete code or exact commands.
