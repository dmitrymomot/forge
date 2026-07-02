# Resilience & Caching Primitives Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship six flat packages — `backoff`, `retry`, `singleflight`, `parallel`, `circuitbreaker`, and `cache` (with a `cache/redis` subpackage) — as the resilience/caching leaves of the forge recommended tier.

**Architecture:** Five near-zero-dependency primitives (stdlib, or stdlib + shipped `clock`) plus a provider-backed `cache`: a byte-level `Store` (built-in memory store + isolated `cache/redis` adapter) with a typed `Cache[V]` facade that owns no backend lifecycle. Instances over globals; each constructor returns isolated state.

**Tech Stack:** Go 1.26, stdlib, `github.com/dmitrymomot/forge/clock` (shipped), `github.com/dmitrymomot/forge/singleflight` (this plan), `github.com/redis/go-redis/v9` (already in go.mod), `github.com/stretchr/testify` for tests.

**Spec:** `docs/superpowers/specs/2026-07-02-resilience-caching-primitives-design.md`

## Global Constraints

- Module path `github.com/dmitrymomot/forge`; Go 1.26.
- **Options only, no builders.** `type Option func(*config)` with an unexported `config`. No exported env `Config` in v1.
- **Instances over globals.** Zero package-level mutable state; no singletons.
- **Black-box tests only** — test package is `<pkg>_test`, importing the package under its public API. Use `testify/assert` and `testify/require`.
- Each package has `doc.go` (runnable example), `errors.go` (single-line `errors.Is` sentinels) where it has errors, and focused impl files.
- `clock.Clock` is `Now()`-only. Inject via `WithClock`; test with `clock.NewMock(t).Advance(d)`.
- Flat top-level packages. Run `just fmt ./<pkg>/...` after edits and `just lint` when the feature is done; both must pass.
- Test a package with `just test ./<pkg>/...` (runs `go test -race -cover`).
- Work only in the current branch. No Claude attribution in commits.

---

### Task 1: `backoff`

**Files:**
- Create: `backoff/backoff.go`
- Create: `backoff/doc.go`
- Test: `backoff/backoff_test.go`

**Interfaces:**
- Produces: `type Backoff interface { Next(attempt int) time.Duration }`; `Constant(d time.Duration) Backoff`; `Exponential(base, max time.Duration, opts ...Option) Backoff`; `WithMultiplier(f float64) Option`; `WithJitter(fraction float64) Option`.

- [ ] **Step 1: Write the failing test** — `backoff/backoff_test.go`

```go
package backoff_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/backoff"
)

func TestConstant(t *testing.T) {
	b := backoff.Constant(2 * time.Second)
	assert.Equal(t, 2*time.Second, b.Next(1))
	assert.Equal(t, 2*time.Second, b.Next(9))
}

func TestExponential(t *testing.T) {
	b := backoff.Exponential(100*time.Millisecond, 10*time.Second)
	assert.Equal(t, 100*time.Millisecond, b.Next(1))
	assert.Equal(t, 200*time.Millisecond, b.Next(2))
	assert.Equal(t, 400*time.Millisecond, b.Next(3))
	assert.Equal(t, 10*time.Second, b.Next(30)) // capped at max
}

func TestExponentialMultiplier(t *testing.T) {
	b := backoff.Exponential(10*time.Millisecond, time.Minute, backoff.WithMultiplier(3))
	assert.Equal(t, 10*time.Millisecond, b.Next(1))
	assert.Equal(t, 30*time.Millisecond, b.Next(2))
	assert.Equal(t, 90*time.Millisecond, b.Next(3))
}

func TestExponentialJitterWithinBounds(t *testing.T) {
	b := backoff.Exponential(100*time.Millisecond, 10*time.Second, backoff.WithJitter(0.5))
	for range 200 {
		d := b.Next(1) // base 100ms ±50% => [50ms, 150ms]
		assert.GreaterOrEqual(t, d, 50*time.Millisecond)
		assert.LessOrEqual(t, d, 150*time.Millisecond)
	}
}
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `just test ./backoff/...`
Expected: FAIL — `undefined: backoff.Constant` (package does not compile yet).

- [ ] **Step 3: Write the implementation** — `backoff/backoff.go`

```go
// Package backoff computes retry delays as pure, stateless strategies.
package backoff

import (
	"math"
	"math/rand/v2"
	"time"
)

// Backoff returns the delay to wait before a given 1-based attempt.
// Implementations are stateless and safe for concurrent use.
type Backoff interface {
	Next(attempt int) time.Duration
}

type constant struct{ d time.Duration }

func (c constant) Next(int) time.Duration { return c.d }

// Constant always returns d. A negative d is clamped to 0.
func Constant(d time.Duration) Backoff {
	if d < 0 {
		d = 0
	}
	return constant{d: d}
}

type exponential struct {
	base       time.Duration
	max        time.Duration
	multiplier float64
	jitter     float64
}

// Option configures Exponential.
type Option func(*exponential)

// WithMultiplier sets the growth factor (default 2.0). Values ≤ 0 are ignored.
func WithMultiplier(f float64) Option {
	return func(e *exponential) {
		if f > 0 {
			e.multiplier = f
		}
	}
}

// WithJitter randomizes each delay by ±fraction (clamped to 0..1).
func WithJitter(fraction float64) Option {
	return func(e *exponential) {
		e.jitter = math.Min(1, math.Max(0, fraction))
	}
}

func (e exponential) Next(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := float64(e.base) * math.Pow(e.multiplier, float64(attempt-1))
	if d > float64(e.max) {
		d = float64(e.max)
	}
	if e.jitter > 0 {
		delta := d * e.jitter
		d = d - delta + rand.Float64()*(2*delta) //nolint:gosec // non-crypto jitter
	}
	if d < 0 {
		d = 0
	}
	return time.Duration(d)
}

// Exponential grows the delay as base*multiplier^(attempt-1), capped at max.
// base is clamped to ≥ 1ns and max to ≥ base.
func Exponential(base, max time.Duration, opts ...Option) Backoff {
	if base < 1 {
		base = 1
	}
	if max < base {
		max = base
	}
	e := exponential{base: base, max: max, multiplier: 2.0}
	for _, o := range opts {
		o(&e)
	}
	return e
}
```

- [ ] **Step 4: Create `backoff/doc.go`**

```go
// Package backoff computes retry delays as pure, stateless strategies.
//
//	b := backoff.Exponential(100*time.Millisecond, 10*time.Second, backoff.WithJitter(0.5))
//	for attempt := 1; attempt <= 5; attempt++ {
//	    time.Sleep(b.Next(attempt))
//	}
package backoff
```

- [ ] **Step 5: Run the test, verify it passes**

Run: `just test ./backoff/...`
Expected: PASS (ok, coverage reported).

- [ ] **Step 6: Format and commit**

```bash
just fmt ./backoff/...
git add backoff/
git commit -m "feat(backoff): stateless exponential/constant delay strategies"
```

---

### Task 2: `retry`

**Files:**
- Create: `retry/retry.go`
- Create: `retry/doc.go`
- Test: `retry/retry_test.go`

**Interfaces:**
- Consumes: `backoff.Backoff`, `backoff.Constant`, `backoff.Exponential`, `backoff.WithJitter`.
- Produces: `type Retrier struct{}`; `New(opts ...Option) *Retrier`; `(*Retrier).Do(ctx, fn func(context.Context) error) error`; `Do(ctx, fn, opts ...Option) error`; `Permanent(err error) error`; `WithMaxAttempts(n int) Option`; `WithBackoff(b backoff.Backoff) Option`; `WithRetryIf(fn func(error) bool) Option`.

- [ ] **Step 1: Write the failing test** — `retry/retry_test.go`

```go
package retry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/backoff"
	"github.com/dmitrymomot/forge/retry"
)

var fast = retry.WithBackoff(backoff.Constant(time.Millisecond))

func TestSucceedsAfterTransientFailures(t *testing.T) {
	calls := 0
	err := retry.Do(t.Context(), func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("boom")
		}
		return nil
	}, retry.WithMaxAttempts(5), fast)
	assert.NoError(t, err)
	assert.Equal(t, 3, calls)
}

func TestExhaustsAndReturnsLastError(t *testing.T) {
	calls := 0
	err := retry.Do(t.Context(), func(context.Context) error {
		calls++
		return errors.New("boom")
	}, retry.WithMaxAttempts(3), fast)
	assert.Error(t, err)
	assert.Equal(t, 3, calls)
}

func TestPermanentStopsImmediately(t *testing.T) {
	sentinel := errors.New("nope")
	calls := 0
	err := retry.Do(t.Context(), func(context.Context) error {
		calls++
		return retry.Permanent(sentinel)
	}, retry.WithMaxAttempts(5), fast)
	assert.ErrorIs(t, err, sentinel)
	assert.Equal(t, 1, calls)
}

func TestRetryIfGate(t *testing.T) {
	stop := errors.New("do-not-retry")
	calls := 0
	err := retry.Do(t.Context(), func(context.Context) error {
		calls++
		return stop
	}, retry.WithMaxAttempts(5), fast, retry.WithRetryIf(func(e error) bool {
		return !errors.Is(e, stop)
	}))
	assert.ErrorIs(t, err, stop)
	assert.Equal(t, 1, calls)
}

func TestContextCancellationDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := retry.Do(ctx, func(context.Context) error {
		return errors.New("boom")
	}, retry.WithMaxAttempts(5), retry.WithBackoff(backoff.Constant(time.Hour)))
	assert.ErrorIs(t, err, context.Canceled)
}

func TestRetrierReusable(t *testing.T) {
	r := retry.New(retry.WithMaxAttempts(2), fast)
	assert.Error(t, r.Do(t.Context(), func(context.Context) error { return errors.New("x") }))
	assert.NoError(t, r.Do(t.Context(), func(context.Context) error { return nil }))
}
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `just test ./retry/...`
Expected: FAIL — `undefined: retry.Do`.

- [ ] **Step 3: Write the implementation** — `retry/retry.go`

```go
// Package retry runs an operation with a backoff strategy until it succeeds,
// a permanent error is returned, or the context is cancelled.
package retry

import (
	"context"
	"errors"
	"time"

	"github.com/dmitrymomot/forge/backoff"
)

type config struct {
	maxAttempts int
	backoff     backoff.Backoff
	retryIf     func(error) bool
}

// Option configures a Retrier.
type Option func(*config)

// WithMaxAttempts caps total attempts (default 3). Values ≤ 0 are ignored.
func WithMaxAttempts(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.maxAttempts = n
		}
	}
}

// WithBackoff sets the delay strategy (default Exponential(100ms,10s,jitter 0.5)).
func WithBackoff(b backoff.Backoff) Option {
	return func(c *config) {
		if b != nil {
			c.backoff = b
		}
	}
}

// WithRetryIf decides, per error, whether to retry (default: retry all non-Permanent).
func WithRetryIf(fn func(error) bool) Option {
	return func(c *config) {
		if fn != nil {
			c.retryIf = fn
		}
	}
}

// Retrier executes functions with a fixed retry policy and is reusable and
// safe for concurrent use.
type Retrier struct{ cfg config }

// New builds a Retrier from options.
func New(opts ...Option) *Retrier {
	c := config{
		maxAttempts: 3,
		backoff:     backoff.Exponential(100*time.Millisecond, 10*time.Second, backoff.WithJitter(0.5)),
		retryIf:     func(error) bool { return true },
	}
	for _, o := range opts {
		o(&c)
	}
	return &Retrier{cfg: c}
}

type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

// Permanent wraps err so retry stops immediately and returns the wrapped error.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

// Do runs fn, retrying per the policy. It returns nil on success, the wrapped
// error for a Permanent, ctx.Err() on cancellation, or the last error on
// exhaustion.
func (r *Retrier) Do(ctx context.Context, fn func(context.Context) error) error {
	var lastErr error
	for attempt := 1; attempt <= r.cfg.maxAttempts; attempt++ {
		err := fn(ctx)
		if err == nil {
			return nil
		}
		var perm *permanentError
		if errors.As(err, &perm) {
			return perm.err
		}
		lastErr = err
		if !r.cfg.retryIf(err) {
			return err
		}
		if attempt == r.cfg.maxAttempts {
			break
		}
		timer := time.NewTimer(r.cfg.backoff.Next(attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}

// Do is a one-shot convenience equivalent to New(opts...).Do(ctx, fn).
func Do(ctx context.Context, fn func(context.Context) error, opts ...Option) error {
	return New(opts...).Do(ctx, fn)
}
```

- [ ] **Step 4: Create `retry/doc.go`**

```go
// Package retry runs an operation with a backoff strategy until it succeeds,
// a permanent error is returned, or the context is cancelled.
//
//	err := retry.Do(ctx, func(ctx context.Context) error {
//	    return callFlakyAPI(ctx)
//	}, retry.WithMaxAttempts(5))
//
// Wrap an error with retry.Permanent to stop early:
//
//	return retry.Permanent(errBadRequest)
package retry
```

- [ ] **Step 5: Run the test, verify it passes**

Run: `just test ./retry/...`
Expected: PASS.

- [ ] **Step 6: Format and commit**

```bash
just fmt ./retry/...
git add retry/
git commit -m "feat(retry): instance-based retry with backoff, Permanent, ctx cancel"
```

---

### Task 3: `singleflight`

**Files:**
- Create: `singleflight/singleflight.go`
- Create: `singleflight/doc.go`
- Test: `singleflight/singleflight_test.go`

**Interfaces:**
- Produces: `type Group[V any] struct{}` (zero-value usable); `(*Group[V]).Do(ctx, key string, fn func(context.Context) (V, error)) (V, bool, error)`; `(*Group[V]).Forget(key string)`.

- [ ] **Step 1: Write the failing test** — `singleflight/singleflight_test.go`

```go
package singleflight_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/singleflight"
)

func TestCoalescesConcurrentCalls(t *testing.T) {
	var g singleflight.Group[int]
	var calls atomic.Int32
	start := make(chan struct{})
	results := make([]int, 20)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			v, _, err := g.Do(t.Context(), "k", func(context.Context) (int, error) {
				calls.Add(1)
				time.Sleep(25 * time.Millisecond)
				return 42, nil
			})
			assert.NoError(t, err)
			results[i] = v
		}()
	}
	close(start)
	wg.Wait()
	assert.Equal(t, int32(1), calls.Load())
	for _, r := range results {
		assert.Equal(t, 42, r)
	}
}

func TestForgetAllowsReexecution(t *testing.T) {
	var g singleflight.Group[int]
	var calls atomic.Int32
	load := func(context.Context) (int, error) { calls.Add(1); return 1, nil }
	_, _, _ = g.Do(t.Context(), "k", load)
	g.Forget("k")
	_, _, _ = g.Do(t.Context(), "k", load)
	assert.Equal(t, int32(2), calls.Load())
}
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `just test ./singleflight/...`
Expected: FAIL — `undefined: singleflight.Group`.

- [ ] **Step 3: Write the implementation** — `singleflight/singleflight.go`

```go
// Package singleflight coalesces concurrent calls for the same key into a
// single execution whose result is shared among all callers.
package singleflight

import (
	"context"
	"sync"
)

type call[V any] struct {
	wg  sync.WaitGroup
	val V
	err error
}

// Group deduplicates concurrent Do calls by key. The zero value is ready to
// use; a Group must not be copied after first use.
type Group[V any] struct {
	mu sync.Mutex
	m  map[string]*call[V]
}

// Do runs fn once for key while a call is in flight, sharing the result with
// concurrent callers. shared reports whether the result came from another
// caller's execution. fn runs under a cancellation-detached copy of ctx so one
// caller cancelling does not abort the shared work; each caller's own ctx still
// bounds its wait via fn's returned error, not the wait itself.
func (g *Group[V]) Do(ctx context.Context, key string, fn func(context.Context) (V, error)) (V, bool, error) {
	g.mu.Lock()
	if g.m == nil {
		g.m = make(map[string]*call[V])
	}
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, true, c.err
	}
	c := new(call[V])
	c.wg.Add(1)
	g.m[key] = c
	g.mu.Unlock()

	c.val, c.err = fn(context.WithoutCancel(ctx))
	c.wg.Done()

	g.mu.Lock()
	if g.m[key] == c {
		delete(g.m, key)
	}
	g.mu.Unlock()
	return c.val, false, c.err
}

// Forget drops any in-flight/last record for key so the next Do re-executes.
func (g *Group[V]) Forget(key string) {
	g.mu.Lock()
	delete(g.m, key)
	g.mu.Unlock()
}
```

- [ ] **Step 4: Create `singleflight/doc.go`**

```go
// Package singleflight coalesces concurrent calls for the same key into a
// single execution whose result is shared among all callers.
//
//	var g singleflight.Group[User]
//	u, shared, err := g.Do(ctx, id, func(ctx context.Context) (User, error) {
//	    return repo.Load(ctx, id)
//	})
package singleflight
```

- [ ] **Step 5: Run the test, verify it passes**

Run: `just test ./singleflight/...`
Expected: PASS.

- [ ] **Step 6: Format and commit**

```bash
just fmt ./singleflight/...
git add singleflight/
git commit -m "feat(singleflight): generic per-key call coalescing"
```

---

### Task 4: `parallel`

**Files:**
- Create: `parallel/parallel.go`
- Create: `parallel/doc.go`
- Test: `parallel/parallel_test.go`

**Interfaces:**
- Produces: `type Group struct{}`; `New(ctx, opts ...Option) (*Group, context.Context)`; `(*Group).Go(fn func(context.Context) error)`; `(*Group).Wait() error`; `WithLimit(n int) Option`; `WithCollectAll() Option`; `ForEach[T any](ctx, items []T, limit int, fn func(context.Context, T) error) error`; `Map[T, U any](ctx, items []T, limit int, fn func(context.Context, T) (U, error)) ([]U, error)`.

- [ ] **Step 1: Write the failing test** — `parallel/parallel_test.go`

```go
package parallel_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/parallel"
)

func TestMapPreservesOrder(t *testing.T) {
	out, err := parallel.Map(t.Context(), []int{1, 2, 3, 4, 5}, 2,
		func(_ context.Context, n int) (int, error) { return n * n, nil })
	require.NoError(t, err)
	assert.Equal(t, []int{1, 4, 9, 16, 25}, out)
}

func TestForEachBoundsConcurrency(t *testing.T) {
	var inflight, peak atomic.Int32
	items := make([]int, 30)
	err := parallel.ForEach(t.Context(), items, 3, func(context.Context, int) error {
		n := inflight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		inflight.Add(-1)
		return nil
	})
	require.NoError(t, err)
	assert.LessOrEqual(t, peak.Load(), int32(3))
}

func TestForEachFailFast(t *testing.T) {
	err := parallel.ForEach(t.Context(), []int{1, 2, 3}, 2, func(_ context.Context, n int) error {
		if n == 2 {
			return errors.New("boom")
		}
		return nil
	})
	assert.Error(t, err)
}

func TestGroupCollectAll(t *testing.T) {
	g, _ := parallel.New(t.Context(), parallel.WithCollectAll())
	for _, n := range []int{1, 2, 3, 4} {
		g.Go(func(context.Context) error {
			if n%2 == 0 {
				return fmt.Errorf("even %d", n)
			}
			return nil
		})
	}
	err := g.Wait()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "even 2")
	assert.Contains(t, err.Error(), "even 4")
}
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `just test ./parallel/...`
Expected: FAIL — `undefined: parallel.Map`.

- [ ] **Step 3: Write the implementation** — `parallel/parallel.go`

```go
// Package parallel runs work concurrently with a bounded worker count. By
// default it is fail-fast (first error cancels the rest); WithCollectAll runs
// everything and joins all errors.
package parallel

import (
	"context"
	"errors"
	"sync"
)

type config struct {
	limit      int
	collectAll bool
}

// Option configures a Group.
type Option func(*config)

// WithLimit bounds concurrent goroutines. n ≤ 0 means unbounded.
func WithLimit(n int) Option { return func(c *config) { c.limit = n } }

// WithCollectAll runs every task to completion and joins all errors instead of
// cancelling siblings on the first failure.
func WithCollectAll() Option { return func(c *config) { c.collectAll = true } }

// Group runs a set of functions concurrently. Construct it with New.
type Group struct {
	ctx        context.Context
	cancel     context.CancelFunc
	collectAll bool
	sem        chan struct{}
	wg         sync.WaitGroup
	once       sync.Once
	firstErr   error
	mu         sync.Mutex
	errs       []error
}

// New returns a Group and a derived context. In fail-fast mode the context is
// cancelled on the first error; it is always cancelled once Wait returns.
func New(ctx context.Context, opts ...Option) (*Group, context.Context) {
	c := config{}
	for _, o := range opts {
		o(&c)
	}
	ctx, cancel := context.WithCancel(ctx)
	g := &Group{ctx: ctx, cancel: cancel, collectAll: c.collectAll}
	if c.limit > 0 {
		g.sem = make(chan struct{}, c.limit)
	}
	return g, ctx
}

// Go runs fn in a new goroutine, blocking if the concurrency limit is reached.
func (g *Group) Go(fn func(context.Context) error) {
	g.wg.Add(1)
	if g.sem != nil {
		g.sem <- struct{}{}
	}
	go func() {
		defer g.wg.Done()
		if g.sem != nil {
			defer func() { <-g.sem }()
		}
		if err := fn(g.ctx); err != nil {
			if g.collectAll {
				g.mu.Lock()
				g.errs = append(g.errs, err)
				g.mu.Unlock()
				return
			}
			g.once.Do(func() {
				g.firstErr = err
				g.cancel()
			})
		}
	}()
}

// Wait blocks until all Go'd functions return, then reports the error: the
// first error in fail-fast mode, or errors.Join of all in collect-all mode.
func (g *Group) Wait() error {
	g.wg.Wait()
	g.cancel()
	if g.collectAll {
		return errors.Join(g.errs...)
	}
	return g.firstErr
}

// ForEach runs fn for every item with bounded concurrency, fail-fast.
func ForEach[T any](ctx context.Context, items []T, limit int, fn func(context.Context, T) error) error {
	g, _ := New(ctx, WithLimit(limit))
	for _, item := range items {
		g.Go(func(ctx context.Context) error { return fn(ctx, item) })
	}
	return g.Wait()
}

// Map applies fn to every item with bounded concurrency, fail-fast, preserving
// order. On any error it returns a nil slice and the first error.
func Map[T, U any](ctx context.Context, items []T, limit int, fn func(context.Context, T) (U, error)) ([]U, error) {
	results := make([]U, len(items))
	g, _ := New(ctx, WithLimit(limit))
	for i, item := range items {
		g.Go(func(ctx context.Context) error {
			u, err := fn(ctx, item)
			if err != nil {
				return err
			}
			results[i] = u
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return results, nil
}
```

- [ ] **Step 4: Create `parallel/doc.go`**

```go
// Package parallel runs work concurrently with a bounded worker count.
//
//	squares, err := parallel.Map(ctx, ids, 8, func(ctx context.Context, id int) (int, error) {
//	    return id * id, nil
//	})
//
// For imperative use, construct a Group:
//
//	g, ctx := parallel.New(ctx, parallel.WithLimit(4))
//	for _, job := range jobs {
//	    g.Go(func(ctx context.Context) error { return job.Run(ctx) })
//	}
//	err := g.Wait()
package parallel
```

- [ ] **Step 5: Run the test, verify it passes**

Run: `just test ./parallel/...`
Expected: PASS.

- [ ] **Step 6: Format and commit**

```bash
just fmt ./parallel/...
git add parallel/
git commit -m "feat(parallel): bounded concurrency with fail-fast and collect-all modes"
```

---

### Task 5: `circuitbreaker`

**Files:**
- Create: `circuitbreaker/circuitbreaker.go`
- Create: `circuitbreaker/errors.go`
- Create: `circuitbreaker/doc.go`
- Test: `circuitbreaker/circuitbreaker_test.go`

**Interfaces:**
- Consumes: `clock.Clock`, `clock.System()`, `clock.NewMock(t)`, `(*clock.Mock).Advance(d)`.
- Produces: `type State int` with `StateClosed/StateOpen/StateHalfOpen` and `String()`; `var ErrOpen`; `type Breaker struct{}`; `New(opts ...Option) *Breaker`; `(*Breaker).Do(ctx, fn func(context.Context) error) error`; `(*Breaker).State() State`; options `WithFailureThreshold(n)`, `WithOpenTimeout(d)`, `WithHalfOpenMax(n)`, `WithOnStateChange(func(from, to State))`, `WithClock(clock.Clock)`.

- [ ] **Step 1: Write the failing test** — `circuitbreaker/circuitbreaker_test.go`

```go
package circuitbreaker_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/circuitbreaker"
	"github.com/dmitrymomot/forge/clock"
)

var errBoom = errors.New("boom")

func fail(context.Context) error { return errBoom }
func ok(context.Context) error   { return nil }

func TestOpensAfterThresholdAndFastFails(t *testing.T) {
	b := circuitbreaker.New(circuitbreaker.WithFailureThreshold(3))
	for range 3 {
		_ = b.Do(t.Context(), fail)
	}
	assert.Equal(t, circuitbreaker.StateOpen, b.State())

	err := b.Do(t.Context(), func(context.Context) error {
		t.Fatal("fn must not run while open")
		return nil
	})
	assert.ErrorIs(t, err, circuitbreaker.ErrOpen)
}

func TestHalfOpenProbeRecovers(t *testing.T) {
	clk := clock.NewMock(time.Now())
	var transitions []string
	b := circuitbreaker.New(
		circuitbreaker.WithFailureThreshold(2),
		circuitbreaker.WithOpenTimeout(10*time.Second),
		circuitbreaker.WithClock(clk),
		circuitbreaker.WithOnStateChange(func(from, to circuitbreaker.State) {
			transitions = append(transitions, from.String()+"->"+to.String())
		}),
	)
	_ = b.Do(t.Context(), fail)
	_ = b.Do(t.Context(), fail)
	assert.Equal(t, circuitbreaker.StateOpen, b.State())

	clk.Advance(10 * time.Second)
	assert.NoError(t, b.Do(t.Context(), ok))
	assert.Equal(t, circuitbreaker.StateClosed, b.State())
	assert.Equal(t, []string{"closed->open", "open->half-open", "half-open->closed"}, transitions)
}

func TestHalfOpenProbeFailureReopens(t *testing.T) {
	clk := clock.NewMock(time.Now())
	b := circuitbreaker.New(
		circuitbreaker.WithFailureThreshold(1),
		circuitbreaker.WithOpenTimeout(5*time.Second),
		circuitbreaker.WithClock(clk),
	)
	_ = b.Do(t.Context(), fail)
	assert.Equal(t, circuitbreaker.StateOpen, b.State())
	clk.Advance(5 * time.Second)
	_ = b.Do(t.Context(), fail) // half-open probe fails
	assert.Equal(t, circuitbreaker.StateOpen, b.State())
}
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `just test ./circuitbreaker/...`
Expected: FAIL — `undefined: circuitbreaker.New`.

- [ ] **Step 3: Write the sentinel + state** — `circuitbreaker/errors.go`

```go
package circuitbreaker

import "errors"

// ErrOpen is returned by Do when the circuit is open and the call is rejected.
var ErrOpen = errors.New("circuitbreaker: circuit open")
```

- [ ] **Step 4: Write the implementation** — `circuitbreaker/circuitbreaker.go`

```go
// Package circuitbreaker fails fast against a failing dependency using a
// closed/open/half-open state machine.
package circuitbreaker

import (
	"context"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/clock"
)

// State is the breaker's current mode.
type State int

const (
	// StateClosed passes calls through and counts failures.
	StateClosed State = iota
	// StateOpen rejects calls with ErrOpen until the open timeout elapses.
	StateOpen
	// StateHalfOpen admits a limited number of probe calls.
	StateHalfOpen
)

// String returns "closed", "open", or "half-open".
func (s State) String() string {
	switch s {
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "closed"
	}
}

type config struct {
	threshold     int
	openTimeout   time.Duration
	halfOpenMax   int
	onStateChange func(from, to State)
	clk           clock.Clock
}

// Option configures a Breaker.
type Option func(*config)

// WithFailureThreshold sets consecutive failures that open the circuit (default 5).
func WithFailureThreshold(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.threshold = n
		}
	}
}

// WithOpenTimeout sets how long the circuit stays open before a probe (default 30s).
func WithOpenTimeout(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.openTimeout = d
		}
	}
}

// WithHalfOpenMax caps concurrent probe calls in half-open (default 1).
func WithHalfOpenMax(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.halfOpenMax = n
		}
	}
}

// WithOnStateChange registers a transition callback. It runs while the breaker
// lock is held, so it must not call back into the same Breaker.
func WithOnStateChange(fn func(from, to State)) Option {
	return func(c *config) { c.onStateChange = fn }
}

// WithClock injects the time source (default clock.System()).
func WithClock(clk clock.Clock) Option {
	return func(c *config) {
		if clk != nil {
			c.clk = clk
		}
	}
}

// Breaker guards calls to a dependency. Construct it with New; safe for
// concurrent use.
type Breaker struct {
	cfg        config
	mu         sync.Mutex
	state      State
	failures   int
	openedAt   time.Time
	halfOpenIn int
}

// New builds a Breaker from options.
func New(opts ...Option) *Breaker {
	c := config{threshold: 5, openTimeout: 30 * time.Second, halfOpenMax: 1, clk: clock.System()}
	for _, o := range opts {
		o(&c)
	}
	return &Breaker{cfg: c, state: StateClosed}
}

// State reports the current state.
func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// Do runs fn unless the circuit rejects it, recording the outcome. It returns
// ErrOpen without calling fn when the circuit is open.
func (b *Breaker) Do(ctx context.Context, fn func(context.Context) error) error {
	if err := b.before(); err != nil {
		return err
	}
	err := fn(ctx)
	b.after(err)
	return err
}

func (b *Breaker) before() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case StateOpen:
		if b.cfg.clk.Now().Sub(b.openedAt) < b.cfg.openTimeout {
			return ErrOpen
		}
		b.transition(StateHalfOpen)
		b.halfOpenIn = 1
		return nil
	case StateHalfOpen:
		if b.halfOpenIn >= b.cfg.halfOpenMax {
			return ErrOpen
		}
		b.halfOpenIn++
		return nil
	default:
		return nil
	}
}

func (b *Breaker) after(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case StateHalfOpen:
		b.halfOpenIn--
		if err != nil {
			b.openedAt = b.cfg.clk.Now()
			b.transition(StateOpen)
			return
		}
		b.failures = 0
		b.transition(StateClosed)
	case StateClosed:
		if err == nil {
			b.failures = 0
			return
		}
		b.failures++
		if b.failures >= b.cfg.threshold {
			b.openedAt = b.cfg.clk.Now()
			b.transition(StateOpen)
		}
	}
}

// transition sets a new state and fires the callback. Caller holds b.mu.
func (b *Breaker) transition(to State) {
	if b.state == to {
		return
	}
	from := b.state
	b.state = to
	if b.cfg.onStateChange != nil {
		b.cfg.onStateChange(from, to)
	}
}
```

- [ ] **Step 5: Create `circuitbreaker/doc.go`**

```go
// Package circuitbreaker fails fast against a failing dependency using a
// closed/open/half-open state machine.
//
//	cb := circuitbreaker.New(circuitbreaker.WithFailureThreshold(5))
//	err := cb.Do(ctx, func(ctx context.Context) error { return call(ctx) })
//	if errors.Is(err, circuitbreaker.ErrOpen) {
//	    // dependency is being given time to recover
//	}
package circuitbreaker
```

- [ ] **Step 6: Run the test, verify it passes**

Run: `just test ./circuitbreaker/...`
Expected: PASS.

- [ ] **Step 7: Format and commit**

```bash
just fmt ./circuitbreaker/...
git add circuitbreaker/
git commit -m "feat(circuitbreaker): closed/open/half-open breaker with clock injection"
```

---

### Task 6: `cache` — `Store`, `Marshaler`, errors, and the memory store

**Files:**
- Create: `cache/errors.go`
- Create: `cache/store.go`
- Create: `cache/memory.go`
- Test: `cache/memory_test.go`

**Interfaces:**
- Consumes: `clock.Clock`, `clock.System()`, `clock.NewMock`, `(*clock.Mock).Advance`.
- Produces: sentinels `ErrNotFound/ErrClosed/ErrMarshal/ErrUnmarshal`; `type Store interface{ Get/Set/Delete/Has/DeletePrefix/Close }`; `type Marshaler[V any] interface{ Marshal/Unmarshal }`; `NewMemoryStore(opts ...MemoryOption) Store`; `WithMaxEntries(n int) MemoryOption`; `WithCleanupInterval(d time.Duration) MemoryOption`; `WithClock(clk clock.Clock) MemoryOption`.

- [ ] **Step 1: Write the failing test** — `cache/memory_test.go`

```go
package cache_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/cache"
	"github.com/dmitrymomot/forge/clock"
)

func TestMemoryStoreSetGetDelete(t *testing.T) {
	s := cache.NewMemoryStore()
	defer s.Close()

	require.NoError(t, s.Set(t.Context(), "k", []byte("v"), 0))
	got, err := s.Get(t.Context(), "k")
	require.NoError(t, err)
	assert.Equal(t, []byte("v"), got)

	require.NoError(t, s.Delete(t.Context(), "k"))
	_, err = s.Get(t.Context(), "k")
	assert.ErrorIs(t, err, cache.ErrNotFound)
}

func TestMemoryStoreExpiry(t *testing.T) {
	clk := clock.NewMock(time.Now())
	s := cache.NewMemoryStore(cache.WithClock(clk))
	defer s.Close()

	require.NoError(t, s.Set(t.Context(), "k", []byte("v"), 30*time.Second))
	clk.Advance(31 * time.Second)
	_, err := s.Get(t.Context(), "k")
	assert.ErrorIs(t, err, cache.ErrNotFound)
}

func TestMemoryStoreNegativeTTLNeverExpires(t *testing.T) {
	clk := clock.NewMock(time.Now())
	s := cache.NewMemoryStore(cache.WithClock(clk))
	defer s.Close()

	require.NoError(t, s.Set(t.Context(), "k", []byte("v"), -1))
	clk.Advance(1000 * time.Hour)
	got, err := s.Get(t.Context(), "k")
	require.NoError(t, err)
	assert.Equal(t, []byte("v"), got)
}

func TestMemoryStoreLRUEviction(t *testing.T) {
	s := cache.NewMemoryStore(cache.WithMaxEntries(2))
	defer s.Close()

	require.NoError(t, s.Set(t.Context(), "a", []byte("1"), -1))
	require.NoError(t, s.Set(t.Context(), "b", []byte("2"), -1))
	_, _ = s.Get(t.Context(), "a") // touch a -> b is least-recently-used
	require.NoError(t, s.Set(t.Context(), "c", []byte("3"), -1))

	_, err := s.Get(t.Context(), "b")
	assert.ErrorIs(t, err, cache.ErrNotFound)
	_, err = s.Get(t.Context(), "a")
	assert.NoError(t, err)
}

func TestMemoryStoreDeletePrefix(t *testing.T) {
	s := cache.NewMemoryStore()
	defer s.Close()

	require.NoError(t, s.Set(t.Context(), "a:1", []byte("x"), -1))
	require.NoError(t, s.Set(t.Context(), "a:2", []byte("x"), -1))
	require.NoError(t, s.Set(t.Context(), "b:1", []byte("x"), -1))

	require.NoError(t, s.DeletePrefix(t.Context(), "a:"))
	_, err := s.Get(t.Context(), "a:1")
	assert.ErrorIs(t, err, cache.ErrNotFound)
	ok, err := s.Has(t.Context(), "b:1")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestMemoryStoreClosedRejects(t *testing.T) {
	s := cache.NewMemoryStore()
	require.NoError(t, s.Close())
	require.NoError(t, s.Close()) // idempotent
	err := s.Set(t.Context(), "k", []byte("v"), 0)
	assert.ErrorIs(t, err, cache.ErrClosed)
}

// Smoke test: exercises the janitor goroutine start/sweep/stop path under -race.
// The janitor uses a real ticker, so this asserts no panic/leak, not timing.
func TestMemoryStoreJanitorStartsAndStops(t *testing.T) {
	s := cache.NewMemoryStore(cache.WithCleanupInterval(5 * time.Millisecond))
	require.NoError(t, s.Set(t.Context(), "k", []byte("v"), time.Millisecond))
	time.Sleep(20 * time.Millisecond) // let the ticker fire at least once
	require.NoError(t, s.Close())      // must stop the goroutine cleanly
}
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `just test ./cache/...`
Expected: FAIL — `undefined: cache.NewMemoryStore`.

- [ ] **Step 3: Write sentinels** — `cache/errors.go`

```go
package cache

import "errors"

// Sentinel errors for cache operations.
var (
	// ErrNotFound is returned when a key is absent or expired.
	ErrNotFound = errors.New("cache: entry not found")
	// ErrClosed is returned by a store after Close.
	ErrClosed = errors.New("cache: closed")
	// ErrMarshal is returned when value serialization fails.
	ErrMarshal = errors.New("cache: failed to marshal value")
	// ErrUnmarshal is returned when value deserialization fails.
	ErrUnmarshal = errors.New("cache: failed to unmarshal value")
)
```

- [ ] **Step 4: Write the Store interface + JSON marshaler** — `cache/store.go`

```go
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Store is a byte-level key/value backend with TTL. Implementations are
// standalone instances whose lifecycle (background goroutines, connections) is
// owned by the caller via Close. TTL semantics: >0 expires after the duration,
// 0 means the store's own default (none for the memory store), <0 never expires.
type Store interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Has(ctx context.Context, key string) (bool, error)
	DeletePrefix(ctx context.Context, prefix string) error
	Close() error
}

// Marshaler serializes cache values to and from bytes.
type Marshaler[V any] interface {
	Marshal(v V) ([]byte, error)
	Unmarshal(data []byte) (V, error)
}

type jsonMarshaler[V any] struct{}

func (jsonMarshaler[V]) Marshal(v V) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, errors.Join(ErrMarshal, err)
	}
	return b, nil
}

func (jsonMarshaler[V]) Unmarshal(data []byte) (V, error) {
	var v V
	if err := json.Unmarshal(data, &v); err != nil {
		return v, errors.Join(ErrUnmarshal, err)
	}
	return v, nil
}
```

- [ ] **Step 5: Write the memory store** — `cache/memory.go`

```go
package cache

import (
	"container/list"
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/clock"
)

type memoryConfig struct {
	maxEntries int
	cleanup    time.Duration
	clk        clock.Clock
}

// MemoryOption configures NewMemoryStore.
type MemoryOption func(*memoryConfig)

// WithMaxEntries caps entries; exceeding it evicts the least-recently-used.
// 0 (default) is unbounded.
func WithMaxEntries(n int) MemoryOption { return func(c *memoryConfig) { c.maxEntries = n } }

// WithCleanupInterval starts a janitor goroutine that sweeps expired entries
// every d; Close stops it. 0 (default) means lazy expiry only.
func WithCleanupInterval(d time.Duration) MemoryOption {
	return func(c *memoryConfig) { c.cleanup = d }
}

// WithClock injects the time source (default clock.System()).
func WithClock(clk clock.Clock) MemoryOption {
	return func(c *memoryConfig) {
		if clk != nil {
			c.clk = clk
		}
	}
}

type memEntry struct {
	key     string
	val     []byte
	expires time.Time // zero = never
	elem    *list.Element
}

type memoryStore struct {
	cfg    memoryConfig
	mu     sync.Mutex
	items  map[string]*memEntry
	lru    *list.List
	closed bool
	stop   chan struct{}
}

// NewMemoryStore returns an in-process Store backed by a map with LRU eviction
// and TTL expiry. It is a standalone instance: call Close to release it.
func NewMemoryStore(opts ...MemoryOption) Store {
	c := memoryConfig{clk: clock.System()}
	for _, o := range opts {
		o(&c)
	}
	m := &memoryStore{cfg: c, items: make(map[string]*memEntry), lru: list.New()}
	if c.cleanup > 0 {
		m.stop = make(chan struct{})
		go m.janitor()
	}
	return m
}

func (m *memoryStore) janitor() {
	t := time.NewTicker(m.cfg.cleanup)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-t.C:
			m.sweep()
		}
	}
}

func (m *memoryStore) sweep() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.cfg.clk.Now()
	for k, e := range m.items {
		if !e.expires.IsZero() && now.After(e.expires) {
			m.removeLocked(k, e)
		}
	}
}

func (m *memoryStore) removeLocked(k string, e *memEntry) {
	delete(m.items, k)
	m.lru.Remove(e.elem)
}

func (m *memoryStore) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrClosed
	}
	e, ok := m.items[key]
	if !ok {
		return nil, ErrNotFound
	}
	if !e.expires.IsZero() && m.cfg.clk.Now().After(e.expires) {
		m.removeLocked(key, e)
		return nil, ErrNotFound
	}
	m.lru.MoveToFront(e.elem)
	out := make([]byte, len(e.val))
	copy(out, e.val)
	return out, nil
}

func (m *memoryStore) Set(_ context.Context, key string, val []byte, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	var expires time.Time
	if ttl > 0 {
		expires = m.cfg.clk.Now().Add(ttl)
	}
	stored := make([]byte, len(val))
	copy(stored, val)
	if e, ok := m.items[key]; ok {
		e.val = stored
		e.expires = expires
		m.lru.MoveToFront(e.elem)
		return nil
	}
	e := &memEntry{key: key, val: stored, expires: expires}
	e.elem = m.lru.PushFront(e)
	m.items[key] = e
	if m.cfg.maxEntries > 0 && m.lru.Len() > m.cfg.maxEntries {
		if back := m.lru.Back(); back != nil {
			old := back.Value.(*memEntry)
			m.removeLocked(old.key, old)
		}
	}
	return nil
}

func (m *memoryStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	if e, ok := m.items[key]; ok {
		m.removeLocked(key, e)
	}
	return nil
}

func (m *memoryStore) Has(ctx context.Context, key string) (bool, error) {
	_, err := m.Get(ctx, key)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, ErrNotFound):
		return false, nil
	default:
		return false, err
	}
}

func (m *memoryStore) DeletePrefix(_ context.Context, prefix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	for k, e := range m.items {
		if strings.HasPrefix(k, prefix) {
			m.removeLocked(k, e)
		}
	}
	return nil
}

func (m *memoryStore) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	if m.stop != nil {
		close(m.stop)
	}
	m.items = nil
	m.lru.Init()
	return nil
}
```

- [ ] **Step 6: Run the test, verify it passes**

Run: `just test ./cache/...`
Expected: PASS (the janitor smoke test also runs clean under `-race`).

- [ ] **Step 7: Format and commit**

```bash
just fmt ./cache/...
git add cache/errors.go cache/store.go cache/memory.go cache/memory_test.go
git commit -m "feat(cache): Store interface, JSON marshaler, in-memory store"
```

---

### Task 7: `cache` — typed `Cache[V]` facade + `GetOrSet`

**Files:**
- Create: `cache/cache.go`
- Create: `cache/doc.go`
- Test: `cache/cache_test.go`

**Interfaces:**
- Consumes: `Store`, `Marshaler[V]`, `jsonMarshaler[V]`, `NewMemoryStore`, sentinels (Task 6); `singleflight.Group[V]` (Task 3).
- Produces: `type Cache[V any] struct{}`; `New[V any](store Store, opts ...Option) *Cache[V]`; methods `Get/Set/Delete/Has/Clear/GetOrSet`; `WithPrefix(p string) Option`; `WithDefaultTTL(d time.Duration) Option`; `WithMarshaler[V any](m Marshaler[V]) Option`.

- [ ] **Step 1: Write the failing test** — `cache/cache_test.go`

```go
package cache_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/cache"
)

func TestCacheSetGetTyped(t *testing.T) {
	store := cache.NewMemoryStore()
	defer store.Close()
	c := cache.New[string](store, cache.WithPrefix("greet:"))

	require.NoError(t, c.Set(t.Context(), "en", "hello", 0))
	v, err := c.Get(t.Context(), "en")
	require.NoError(t, err)
	assert.Equal(t, "hello", v)
}

func TestCacheMissIsErrNotFound(t *testing.T) {
	store := cache.NewMemoryStore()
	defer store.Close()
	c := cache.New[int](store)
	_, err := c.Get(t.Context(), "nope")
	assert.ErrorIs(t, err, cache.ErrNotFound)
}

func TestCacheIsolationByPrefixOverSharedStore(t *testing.T) {
	store := cache.NewMemoryStore()
	defer store.Close()
	a := cache.New[string](store, cache.WithPrefix("a:"))
	b := cache.New[string](store, cache.WithPrefix("b:"))

	require.NoError(t, a.Set(t.Context(), "k", "AAA", -1))
	require.NoError(t, b.Set(t.Context(), "k", "BBB", -1))

	av, _ := a.Get(t.Context(), "k")
	bv, _ := b.Get(t.Context(), "k")
	assert.Equal(t, "AAA", av)
	assert.Equal(t, "BBB", bv)

	require.NoError(t, a.Clear(t.Context()))
	_, err := a.Get(t.Context(), "k")
	assert.ErrorIs(t, err, cache.ErrNotFound)

	still, err := b.Get(t.Context(), "k") // b untouched by a.Clear
	require.NoError(t, err)
	assert.Equal(t, "BBB", still)
}

func TestGetOrSetStampede(t *testing.T) {
	store := cache.NewMemoryStore()
	defer store.Close()
	c := cache.New[int](store)

	var calls atomic.Int32
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			v, err := c.GetOrSet(t.Context(), "k", func(context.Context) (int, time.Duration, error) {
				calls.Add(1)
				time.Sleep(25 * time.Millisecond)
				return 99, time.Minute, nil
			})
			assert.NoError(t, err)
			assert.Equal(t, 99, v)
		}()
	}
	close(start)
	wg.Wait()
	assert.Equal(t, int32(1), calls.Load())

	// value is now cached; loader is not called again
	v, err := c.GetOrSet(t.Context(), "k", func(context.Context) (int, time.Duration, error) {
		t.Fatal("loader must not run on hit")
		return 0, 0, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 99, v)
}
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `just test ./cache/...`
Expected: FAIL — `undefined: cache.New`.

- [ ] **Step 3: Write the facade** — `cache/cache.go`

```go
package cache

import (
	"context"
	"time"

	"github.com/dmitrymomot/forge/singleflight"
)

type config struct {
	prefix     string
	defaultTTL time.Duration
	marshaler  any // Marshaler[V] set by WithMarshaler; resolved in New
}

// Option configures a Cache. Options are non-generic so call sites need no type
// arguments; WithMarshaler infers V from its argument.
type Option func(*config)

// WithPrefix namespaces this cache's keys within its Store, isolating it from
// other caches sharing the same Store.
func WithPrefix(p string) Option { return func(c *config) { c.prefix = p } }

// WithDefaultTTL is applied when Set receives ttl == 0 (default 5m).
func WithDefaultTTL(d time.Duration) Option { return func(c *config) { c.defaultTTL = d } }

// WithMarshaler overrides the default JSON serialization.
func WithMarshaler[V any](m Marshaler[V]) Option {
	return func(c *config) {
		if m != nil {
			c.marshaler = m
		}
	}
}

// Cache is a typed facade over a Store. It marshals values, applies the default
// TTL, isolates keys by prefix, and provides GetOrSet. It does NOT own the
// Store: construct and Close the Store yourself.
type Cache[V any] struct {
	store      Store
	prefix     string
	defaultTTL time.Duration
	marshaler  Marshaler[V]
	sf         singleflight.Group[V]
}

// New builds a typed cache over store. The Store's lifecycle stays with the
// caller; Cache has no Close.
func New[V any](store Store, opts ...Option) *Cache[V] {
	c := config{defaultTTL: 5 * time.Minute}
	for _, o := range opts {
		o(&c)
	}
	var m Marshaler[V]
	if c.marshaler != nil {
		m = c.marshaler.(Marshaler[V]) // set by WithMarshaler[V]; V matches New[V]
	} else {
		m = jsonMarshaler[V]{}
	}
	return &Cache[V]{store: store, prefix: c.prefix, defaultTTL: c.defaultTTL, marshaler: m}
}

func (c *Cache[V]) key(k string) string { return c.prefix + k }

// Get returns the value for key or ErrNotFound.
func (c *Cache[V]) Get(ctx context.Context, key string) (V, error) {
	var zero V
	data, err := c.store.Get(ctx, c.key(key))
	if err != nil {
		return zero, err
	}
	return c.marshaler.Unmarshal(data)
}

// Set stores v under key. ttl == 0 uses the configured default; ttl < 0 never
// expires.
func (c *Cache[V]) Set(ctx context.Context, key string, v V, ttl time.Duration) error {
	data, err := c.marshaler.Marshal(v)
	if err != nil {
		return err
	}
	if ttl == 0 {
		ttl = c.defaultTTL
	}
	return c.store.Set(ctx, c.key(key), data, ttl)
}

// Delete removes key.
func (c *Cache[V]) Delete(ctx context.Context, key string) error {
	return c.store.Delete(ctx, c.key(key))
}

// Has reports whether key exists and is unexpired.
func (c *Cache[V]) Has(ctx context.Context, key string) (bool, error) {
	return c.store.Has(ctx, c.key(key))
}

// Clear removes every key under this cache's prefix. With an empty prefix it
// clears the whole Store.
func (c *Cache[V]) Clear(ctx context.Context) error {
	return c.store.DeletePrefix(ctx, c.prefix)
}

// GetOrSet returns the cached value or computes it via fn on a miss,
// deduplicating concurrent misses so fn runs once per key. The value is cached
// best-effort with the TTL fn returns; load errors are not cached.
func (c *Cache[V]) GetOrSet(ctx context.Context, key string, fn func(context.Context) (V, time.Duration, error)) (V, error) {
	if v, err := c.Get(ctx, key); err == nil {
		return v, nil
	}
	v, _, err := c.sf.Do(ctx, c.key(key), func(ctx context.Context) (V, error) {
		val, ttl, ferr := fn(ctx)
		if ferr != nil {
			var zero V
			return zero, ferr
		}
		_ = c.Set(ctx, key, val, ttl)
		return val, nil
	})
	return v, err
}
```

- [ ] **Step 4: Create `cache/doc.go`**

```go
// Package cache is a typed cache over a pluggable byte-level Store. Build a
// Store (in-memory or, via cache/redis, Redis) and wrap it with a typed facade.
// The facade never owns the Store's lifecycle.
//
//	store := cache.NewMemoryStore(cache.WithMaxEntries(10_000))
//	defer store.Close()
//
//	users := cache.New[User](store, cache.WithPrefix("users:"), cache.WithDefaultTTL(30*time.Minute))
//	u, err := users.GetOrSet(ctx, id, func(ctx context.Context) (User, time.Duration, error) {
//	    u, err := repo.Load(ctx, id)
//	    return u, 5 * time.Minute, err
//	})
package cache
```

- [ ] **Step 5: Run the test, verify it passes**

Run: `just test ./cache/...`
Expected: PASS.

- [ ] **Step 6: Format and commit**

```bash
just fmt ./cache/...
git add cache/cache.go cache/doc.go cache/cache_test.go
git commit -m "feat(cache): typed Cache[V] facade with GetOrSet stampede protection"
```

---

### Task 8: `cache/redis` — Redis `Store` adapter

**Files:**
- Create: `cache/redis/redis.go`
- Create: `cache/redis/doc.go`
- Test: `cache/redis/redis_test.go`

**Interfaces:**
- Consumes: `cache.Store`, `cache.ErrNotFound`, `cache.New`, `cache.WithPrefix` (Tasks 6–7); forge `redis.IsNil` and `goredis.UniversalClient`.
- Produces: `NewStore(client goredis.UniversalClient) cache.Store`.

- [ ] **Step 1: Write the failing test** — `cache/redis/redis_test.go`

```go
package redis_test

import (
	"context"
	"os"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/cache"
	cacheredis "github.com/dmitrymomot/forge/cache/redis"
)

func testClient(t *testing.T) goredis.UniversalClient {
	t.Helper()
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL not set")
	}
	opt, err := goredis.ParseURL(url)
	require.NoError(t, err)
	c := goredis.NewClient(opt)
	require.NoError(t, c.Ping(context.Background()).Err())
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestRedisStoreRoundTripAndScopedClear(t *testing.T) {
	client := testClient(t)
	store := cacheredis.NewStore(client)

	users := cache.New[string](store, cache.WithPrefix("test:cache:users:"))
	other := cache.New[string](store, cache.WithPrefix("test:cache:other:"))

	require.NoError(t, users.Set(t.Context(), "1", "alice", time.Minute))
	require.NoError(t, other.Set(t.Context(), "1", "keep", time.Minute))

	v, err := users.Get(t.Context(), "1")
	require.NoError(t, err)
	assert.Equal(t, "alice", v)

	require.NoError(t, users.Clear(t.Context()))
	_, err = users.Get(t.Context(), "1")
	assert.ErrorIs(t, err, cache.ErrNotFound)

	kept, err := other.Get(t.Context(), "1") // scoped clear left other's prefix alone
	require.NoError(t, err)
	assert.Equal(t, "keep", kept)
}
```

- [ ] **Step 2: Run the test, verify it fails (or skips without Redis)**

Run: `just test ./cache/redis/...`
Expected: FAIL to compile — `undefined: cacheredis.NewStore` (once implemented, SKIP when `TEST_REDIS_URL` is unset).

- [ ] **Step 3: Write the adapter** — `cache/redis/redis.go`

```go
// Package redis provides a Redis-backed cache.Store. Construct a client (e.g.
// via forge/redis.Open), pass it to NewStore, and wrap the result with a typed
// cache.Cache. The client's lifecycle stays with the caller.
package redis

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/dmitrymomot/forge/cache"
	forgeredis "github.com/dmitrymomot/forge/redis"
)

type store struct {
	client goredis.UniversalClient
}

// NewStore returns a cache.Store backed by client. Store.Close is a no-op: the
// caller owns the client and must close it.
func NewStore(client goredis.UniversalClient) cache.Store {
	return &store{client: client}
}

func (s *store) Get(ctx context.Context, key string) ([]byte, error) {
	b, err := s.client.Get(ctx, key).Bytes()
	if forgeredis.IsNil(err) {
		return nil, cache.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (s *store) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	var exp time.Duration // ttl <= 0 -> 0 -> no expiry in Redis
	if ttl > 0 {
		exp = ttl
	}
	return s.client.Set(ctx, key, val, exp).Err()
}

func (s *store) Delete(ctx context.Context, key string) error {
	return s.client.Del(ctx, key).Err()
}

func (s *store) Has(ctx context.Context, key string) (bool, error) {
	n, err := s.client.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *store) DeletePrefix(ctx context.Context, prefix string) error {
	var cursor uint64
	for {
		keys, next, err := s.client.Scan(ctx, cursor, prefix+"*", 100).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := s.client.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}

// Close is a no-op; the caller owns the client's lifecycle.
func (s *store) Close() error { return nil }
```

- [ ] **Step 4: Create `cache/redis/doc.go`**

```go
// Package redis provides a Redis-backed cache.Store.
//
//	client := redis.Open(ctx, redis.WithConfig(cfg)) // forge/redis; caller closes it
//	defer client.Close()
//
//	store := cacheredis.NewStore(client)
//	sessions := cache.New[Session](store, cache.WithPrefix("sess:"))
//
// Store.Close is a no-op — close the underlying client yourself. Clear on a
// typed cache issues SCAN + DEL over the cache's prefix (never FLUSHDB).
package redis
```

- [ ] **Step 5: Run the test, verify it passes (or skips)**

Run: `just test ./cache/redis/...`
Expected: PASS (SKIP when `TEST_REDIS_URL` is unset; PASS against a real Redis when set).

- [ ] **Step 6: Format and commit**

```bash
just fmt ./cache/redis/...
git add cache/redis/
git commit -m "feat(cache/redis): Redis-backed Store adapter (caller-owned client)"
```

---

### Finalization

- [ ] **Step 1: Run the full linter**

Run: `just lint`
Expected: clean. `modernize`, `nilaway`, `betteralign`, and `golangci-lint` must pass. Common fixes: run `just fmt ./...` to apply `betteralign` field reordering and `goimports`; add nil guards if `nilaway` flags a path.

- [ ] **Step 2: Run the whole suite with the race detector**

Run: `just test ./backoff/... ./retry/... ./singleflight/... ./parallel/... ./circuitbreaker/... ./cache/...`
Expected: all PASS, no race warnings. (The `cache/redis` integration test SKIPs without `TEST_REDIS_URL`.)

- [ ] **Step 3: Commit any lint/format fixups**

```bash
git add -A
git commit -m "chore: fmt and lint fixups for resilience/caching bundle"
```

---

## Notes for the implementer

- **TDD rhythm:** every task is test-first — write the test, watch it fail, implement, watch it pass, commit. Don't batch.
- **Black-box only:** tests live in `<pkg>_test` and touch only exported API. Don't reach into unexported state.
- **`t.Context()`** (Go 1.24+) gives a per-test context cancelled at test end; use it instead of `context.Background()`.
- **Determinism:** anything time-based (`circuitbreaker`, cache expiry) is tested with `clock.NewMock(time.Now())` + `Advance`. `retry` uses a 1ms `backoff.Constant` — never a real fake-clock sleep.
- **Do not** add an env-loadable `Config`, a background service, `errors.Join` to `parallel`'s default mode, or typed eviction callbacks — all explicit non-goals in the spec.
