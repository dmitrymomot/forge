# Resilience Seam Bundle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add three seam features across the shipped `resilience/` packages — an options-based `cache.Store.Set` with an atomic NX claim, a keyed `circuitbreaker.Group` plus HTTP middleware, and a `retry` delay floor — shipped as one PR that unblocks `web/httpclient`.

**Architecture:** Each feature is an additive/edit change to an existing package. `cache.Set` moves TTL+NX into `...SetOption`, resolved via an exported `SetOptions` so the `cache/redis` sibling can read them. `circuitbreaker` gains a keyed `Group` (opportunistic idle-TTL eviction, no goroutine) and a `net/http`-only middleware; its open error carries a dynamic `RetryAfter()`. `retry` honors any error implementing `RetryAfterError` as a per-attempt delay floor — satisfied structurally by the breaker error, with no import between the two.

**Tech Stack:** Go 1.26, `github.com/redis/go-redis/v9` (v9.21.0), `core/clock`, `resilience/backoff`, `resilience/singleflight`. Full design + reference code: [2026-07-04-resilience-seam-bundle-design.md](../specs/2026-07-04-resilience-seam-bundle-design.md).

## Global Constraints

- **Go 1.26.** `errors.AsType[E error]` is available and works with interface `E`.
- **Black-box tests only.** Test files use package `<pkg>_test` and exercise only exported API. White-box only to assert unexported state (not needed here).
- **Options pattern, never builders.** `type XOption func(*config)`; no fluent/builder chains.
- **No global or shared mutable state.** No `init()`, no singletons, no registries, no default global instances. Only immutable error sentinels and stateless funcs may be package-level.
- **`resilience/` imports no `web/` package.** The breaker middleware uses `net/http` only.
- **Run `just fmt <file>.go` after editing each Go file; run `just lint` after the final task.**
- **Redis backend behavior:** use `goredis.SetArgs{Mode:"NX"}` (SET…NX), not the legacy `SetNX`; map `forgeredis.IsNil` → `cache.ErrExists`.
- **Commit style:** `type(scope): summary`. No Claude attribution/co-author trailers anywhere.

---

### Task 1: cache — options-based `Set` with atomic NX claim

Flips `Store.Set(…, ttl)` to `Set(…, ...SetOption)` and folds NX in. The interface change breaks `memory`, the `Cache[V]` facade, **and** the `cache/redis` sibling at once, so all land together for a compiling, green commit.

**Files:**
- Modify: `resilience/cache/errors.go` (+`ErrExists`)
- Modify: `resilience/cache/store.go` (interface signature; +`SetOptions`/`SetOption`/`WithTTL`/`WithSetNonExist`/`ApplySetOptions`)
- Modify: `resilience/cache/memory.go` (`Set` body)
- Modify: `resilience/cache/cache.go` (`Cache[V].Set`, `GetOrSet` internal write)
- Modify: `resilience/cache/redis/redis.go` (`Set` body)
- Test: `resilience/cache/cache_test.go`, `resilience/cache/memory_test.go`, `resilience/cache/redis/redis_test.go` — **migrate every existing `Set(…, ttl)` call site** to the options form and the `errStore` stub's signature, then **add** NX/TTL cases. These files use testify (`require`/`assert`) and `t.Context()`; match that style.

**Interfaces:**
- Produces:
  - `cache.SetOptions struct { TTL time.Duration; OnlyIfNew bool }`
  - `cache.SetOption func(*SetOptions)`
  - `cache.WithTTL(d time.Duration) SetOption`
  - `cache.WithSetNonExist() SetOption`
  - `cache.ApplySetOptions(opts ...SetOption) SetOptions`
  - `cache.ErrExists error`
  - `cache.Store.Set(ctx context.Context, key string, val []byte, opts ...SetOption) error`
  - `(*cache.Cache[V]).Set(ctx context.Context, key string, v V, opts ...SetOption) error`
- Consumes: nothing from other tasks.

- [ ] **Step 1a: Migrate existing `Set(…, ttl)` call sites to options**

These stale 4-arg calls will not compile against the new signature, so rewrite them. Mapping (applies to both `Store` and `Cache[V]` calls): `, 0)` → `)` (drop the arg); `, -1)` → `, cache.WithTTL(-1))`; `, D)` with `D>0` → `, cache.WithTTL(D))`.

In `resilience/cache/cache_test.go`:
- L22 `c.Set(t.Context(), "en", "hello", 0)` → `c.Set(t.Context(), "en", "hello")`
- L42 `a.Set(t.Context(), "k", "AAA", -1)` → `a.Set(t.Context(), "k", "AAA", cache.WithTTL(-1))`
- L43 `b.Set(t.Context(), "k", "BBB", -1)` → `b.Set(t.Context(), "k", "BBB", cache.WithTTL(-1))`
- L136 `c.Set(t.Context(), "k", "hi", -1)` → `c.Set(t.Context(), "k", "hi", cache.WithTTL(-1))`
- L107 the `errStore` stub — change its `Set` method signature so it still satisfies `Store`:

```go
func (errStore) Set(context.Context, string, []byte, ...cache.SetOption) error { return nil }
```

In `resilience/cache/memory_test.go` (all `s.Set(...)`): `, 0)` → `)` at L18 and L86; `, 30*time.Second)` → `, cache.WithTTL(30*time.Second))` at L33; `, time.Millisecond)` → `, cache.WithTTL(time.Millisecond))` at L94; `, -1)` → `, cache.WithTTL(-1))` at L44, L55, L56, L58, L70, L71, L72.

In `resilience/cache/redis/redis_test.go` (all facade `.Set(...)`): `, time.Minute)` → `, cache.WithTTL(time.Minute))` at L38, L39, L64, L65.

- [ ] **Step 1b: Add failing black-box tests (testify style, matching the files)**

Append to `resilience/cache/memory_test.go` (add `"sync"` and `"sync/atomic"` to its imports):

```go
func TestMemoryStoreSetNonExistClaimsOnce(t *testing.T) {
	s := cache.NewMemoryStore()
	defer func() { _ = s.Close() }()

	require.NoError(t, s.Set(t.Context(), "k", []byte("first"), cache.WithSetNonExist()))
	assert.ErrorIs(t, s.Set(t.Context(), "k", []byte("second"), cache.WithSetNonExist()), cache.ErrExists)

	got, _ := s.Get(t.Context(), "k")
	assert.Equal(t, []byte("first"), got) // original not overwritten
}

func TestMemoryStoreSetNonExistReclaimsExpired(t *testing.T) {
	clk := clock.NewMock(time.Now())
	s := cache.NewMemoryStore(cache.WithClock(clk))
	defer func() { _ = s.Close() }()

	require.NoError(t, s.Set(t.Context(), "k", []byte("old"), cache.WithSetNonExist(), cache.WithTTL(time.Second)))
	clk.Advance(2 * time.Second) // entry now expired
	require.NoError(t, s.Set(t.Context(), "k", []byte("new"), cache.WithSetNonExist()))
	got, _ := s.Get(t.Context(), "k")
	assert.Equal(t, []byte("new"), got)
}

func TestMemoryStoreSetNonExistConcurrent(t *testing.T) {
	s := cache.NewMemoryStore()
	defer func() { _ = s.Close() }()

	var wins atomic.Int32
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			if err := s.Set(t.Context(), "k", []byte("v"), cache.WithSetNonExist()); err == nil {
				wins.Add(1)
			}
		})
	}
	wg.Wait()
	assert.Equal(t, int32(1), wins.Load()) // exactly one claim wins
}
```

Append to `resilience/cache/cache_test.go` (its imports already include `time` and testify):

```go
func TestFacadeSetNonExistReturnsErrExists(t *testing.T) {
	store := cache.NewMemoryStore()
	defer func() { _ = store.Close() }()
	c := cache.New[string](store)

	require.NoError(t, c.Set(t.Context(), "k", "first", cache.WithSetNonExist()))
	assert.ErrorIs(t, c.Set(t.Context(), "k", "second", cache.WithSetNonExist()), cache.ErrExists)
}

func TestApplySetOptions(t *testing.T) {
	o := cache.ApplySetOptions(cache.WithTTL(5*time.Second), cache.WithSetNonExist())
	assert.Equal(t, 5*time.Second, o.TTL)
	assert.True(t, o.OnlyIfNew)

	z := cache.ApplySetOptions()
	assert.Zero(t, z.TTL)
	assert.False(t, z.OnlyIfNew)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./resilience/cache/... 2>&1 | head`
Expected: FAIL to build — the migrated call sites and new tests reference `cache.WithTTL`/`cache.WithSetNonExist`/`cache.ErrExists`/`cache.ApplySetOptions`, none defined yet.

- [ ] **Step 3: Add `ErrExists` to `errors.go`**

Inside the existing `var ( … )` block in `resilience/cache/errors.go`, add:

```go
	// ErrExists is returned by Set with WithSetNonExist when the key is present.
	ErrExists = errors.New("cache: entry already exists")
```

- [ ] **Step 4: Add option types + flip the interface in `store.go`**

Replace the `Store` interface doc comment and `Set` line, and append the option types. The `Store` block becomes:

```go
// Store is a byte-level key/value backend with optional per-entry TTL.
// Implementations are standalone instances whose lifecycle (background
// goroutines, connections) is owned by the caller via Close.
//
// Set is configured with SetOption values: WithTTL sets an expiry (no option
// means no expiry), WithSetNonExist makes the write conditional and returns
// ErrExists when the key already exists. Implementations resolve options with
// ApplySetOptions so every backend behaves identically.
type Store interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, val []byte, opts ...SetOption) error
	Delete(ctx context.Context, key string) error
	Has(ctx context.Context, key string) (bool, error)
	DeletePrefix(ctx context.Context, prefix string) error
	Close() error
}

// SetOptions holds the resolved settings for one Set call. It is exported
// because Store implementations in sibling packages (e.g. cache/redis) apply
// SetOption values and read the result. The zero value means: never expire,
// overwrite an existing key.
type SetOptions struct {
	// TTL expires the entry after the duration. A value <= 0 means no expiry.
	TTL time.Duration
	// OnlyIfNew stores the value only when the key is absent or expired; on a
	// live key Set writes nothing and returns ErrExists.
	OnlyIfNew bool
}

// SetOption configures a single Set call.
type SetOption func(*SetOptions)

// WithTTL expires the written entry after d. Without it the entry never
// expires; a non-positive d is equivalent to omitting the option.
func WithTTL(d time.Duration) SetOption { return func(o *SetOptions) { o.TTL = d } }

// WithSetNonExist stores the value only if the key is absent or expired. On a
// live key Set writes nothing and returns ErrExists. Combined with WithTTL it
// is an atomic claim-with-lease (set-if-absent + expiry).
func WithSetNonExist() SetOption { return func(o *SetOptions) { o.OnlyIfNew = true } }

// ApplySetOptions resolves opts into a SetOptions so every backend applies the
// options identically.
func ApplySetOptions(opts ...SetOption) SetOptions {
	var o SetOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}
```

- [ ] **Step 5: Update `memory.go` `Set`**

Replace the whole `func (m *memoryStore) Set(...)` with:

```go
func (m *memoryStore) Set(_ context.Context, key string, val []byte, opts ...SetOption) error {
	o := ApplySetOptions(opts...)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	now := m.cfg.clk.Now()
	if o.OnlyIfNew {
		if e, ok := m.items[key]; ok && (e.expires.IsZero() || now.Before(e.expires)) {
			return ErrExists
		}
	}
	var expires time.Time
	if o.TTL > 0 {
		expires = now.Add(o.TTL)
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
```

- [ ] **Step 6: Update `cache.go` facade `Set` and `GetOrSet`**

Replace `func (c *Cache[V]) Set(...)` with:

```go
// Set stores v under key. Without a WithTTL option the cache's default TTL
// applies; WithTTL overrides it (a non-positive duration means no expiry).
// WithSetNonExist makes the write conditional, returning ErrExists when the key
// is already present.
func (c *Cache[V]) Set(ctx context.Context, key string, v V, opts ...SetOption) error {
	data, err := c.marshaler.Marshal(v)
	if err != nil {
		return err
	}
	// Prepend the default TTL so any caller WithTTL (including a non-positive
	// "never expire") overrides it; a fresh slice leaves the caller's untouched.
	opts = append([]SetOption{WithTTL(c.defaultTTL)}, opts...)
	return c.store.Set(ctx, c.key(key), data, opts...)
}
```

In `GetOrSet`, replace the inner `_ = c.Set(ctx, key, val, ttl)` write with:

```go
		var opts []SetOption
		if ttl != 0 {
			opts = append(opts, WithTTL(ttl))
		}
		_ = c.Set(ctx, key, val, opts...)
```

- [ ] **Step 7: Update `redis/redis.go` `Set`**

Replace `func (s *store) Set(...)` with:

```go
func (s *store) Set(ctx context.Context, key string, val []byte, opts ...cache.SetOption) error {
	o := cache.ApplySetOptions(opts...)
	var ttl time.Duration
	if o.TTL > 0 {
		ttl = o.TTL // SetArgs treats TTL <= 0 as "no expiry"
	}
	if o.OnlyIfNew {
		// SET key val NX [PX ttl] — the modern replacement for SETNX. On a
		// present key the server returns a null reply, surfaced as redis.Nil.
		err := s.client.SetArgs(ctx, key, val, goredis.SetArgs{Mode: "NX", TTL: ttl}).Err()
		if forgeredis.IsNil(err) {
			return cache.ErrExists
		}
		return err
	}
	return s.client.Set(ctx, key, val, ttl).Err()
}
```

- [ ] **Step 8: Format, build, test**

Run: `just fmt resilience/cache/store.go resilience/cache/memory.go resilience/cache/cache.go resilience/cache/errors.go resilience/cache/redis/redis.go resilience/cache/cache_test.go`
Run: `go build ./... && go test ./resilience/cache/... -race`
Expected: build OK; all cache tests PASS (redis integration tests skip without a live server; the redis package compiles).

- [ ] **Step 9: Commit**

```bash
git add resilience/cache/
git commit -m "feat(resilience/cache): options-based Set with atomic WithSetNonExist claim"
```

---

### Task 2: retry — honor `RetryAfterError` as a delay floor

**Files:**
- Create: `resilience/retry/errors.go`
- Modify: `resilience/retry/retry.go` (delay computation in `Do`)
- Test: `resilience/retry/retry_test.go` (black-box; add cases)

**Interfaces:**
- Produces: `retry.RetryAfterError interface { error; RetryAfter() time.Duration }`
- Consumes: nothing. (Satisfied structurally later by `circuitbreaker`'s open error — no import.)

- [ ] **Step 1: Write failing black-box tests**

Append to `resilience/retry/retry_test.go`:

```go
type retryAfterErr struct{ d time.Duration }

func (e retryAfterErr) Error() string              { return "slow down" }
func (e retryAfterErr) RetryAfter() time.Duration  { return e.d }

func TestRetryAfterRaisesDelayToFloor(t *testing.T) {
	var attempts int
	start := time.Now()
	err := retry.Do(context.Background(), func(context.Context) error {
		attempts++
		if attempts < 2 {
			return retryAfterErr{d: 120 * time.Millisecond}
		}
		return nil
	},
		retry.WithMaxAttempts(2),
		retry.WithBackoff(backoff.Constant(1*time.Millisecond)), // far below the hint
	)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("waited %v, want >= ~120ms (the RetryAfter floor)", elapsed)
	}
}

func TestRetryAfterHonoredThroughWrappedError(t *testing.T) {
	var attempts int
	start := time.Now()
	_ = retry.Do(context.Background(), func(context.Context) error {
		attempts++
		if attempts < 2 {
			return fmt.Errorf("wrapped: %w", retryAfterErr{d: 120 * time.Millisecond})
		}
		return nil
	}, retry.WithMaxAttempts(2), retry.WithBackoff(backoff.Constant(1*time.Millisecond)))
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("waited %v, want >= ~120ms via wrapped hint", elapsed)
	}
}

func TestRetryAfterBelowBackoffKeepsBackoff(t *testing.T) {
	var attempts int
	start := time.Now()
	_ = retry.Do(context.Background(), func(context.Context) error {
		attempts++
		if attempts < 2 {
			return retryAfterErr{d: 1 * time.Millisecond} // below backoff
		}
		return nil
	}, retry.WithMaxAttempts(2), retry.WithBackoff(backoff.Constant(80*time.Millisecond)))
	if elapsed := time.Since(start); elapsed < 60*time.Millisecond {
		t.Fatalf("waited %v, want >= ~80ms (backoff wins)", elapsed)
	}
}
```

Ensure imports include `context`, `fmt`, `time`, `github.com/dmitrymomot/forge/resilience/backoff`, `github.com/dmitrymomot/forge/resilience/retry`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./resilience/retry/ -run TestRetryAfter 2>&1 | head`
Expected: FAIL — the floor is not applied yet, so `TestRetryAfterRaisesDelayToFloor` waits ~1ms and the assertion fails.

- [ ] **Step 3: Create `resilience/retry/errors.go`**

```go
package retry

import "time"

// RetryAfterError is implemented by errors that carry a minimum delay before
// the next attempt — e.g. an HTTP 429/503 with a Retry-After header, or a
// circuitbreaker open error. Retrier.Do treats the reported duration as a
// floor: it waits at least this long before the next attempt, even beyond the
// backoff ceiling. The context deadline still bounds the total.
type RetryAfterError interface {
	error
	RetryAfter() time.Duration
}
```

- [ ] **Step 4: Apply the floor in `retry.go` `Do`**

Replace the line `timer := time.NewTimer(r.cfg.backoff.Next(attempt))` with:

```go
		// Wait per the backoff, raised to any server/breaker-stated floor.
		wait := r.cfg.backoff.Next(attempt)
		if ra, ok := errors.AsType[RetryAfterError](err); ok {
			if hint := ra.RetryAfter(); hint > wait {
				wait = hint
			}
		}
		timer := time.NewTimer(wait)
```

(`errors` and `time` are already imported in `retry.go`.)

- [ ] **Step 5: Format, test**

Run: `just fmt resilience/retry/errors.go resilience/retry/retry.go resilience/retry/retry_test.go`
Run: `go test ./resilience/retry/ -race`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add resilience/retry/
git commit -m "feat(resilience/retry): honor RetryAfterError as a per-attempt delay floor"
```

---

### Task 3: circuitbreaker — dynamic open error + `RetryAfter()`

Introduces `openError` (unwraps to `ErrOpen`, carries a delay) and exposes `Breaker.RetryAfter()`. Refactors config building into `newConfig` for reuse by `Group` (Task 4).

**Files:**
- Modify: `resilience/circuitbreaker/errors.go` (+`openError`)
- Modify: `resilience/circuitbreaker/circuitbreaker.go` (`newConfig`, `New`, `before`, +`RetryAfter`)
- Test: `resilience/circuitbreaker/circuitbreaker_test.go` (black-box; append cases, reusing the package's existing `fail`/`ok`/`errBoom` helpers and testify `assert`; existing `ErrOpen` checks already use `assert.ErrorIs`, so nothing to migrate)

**Interfaces:**
- Produces:
  - `circuitbreaker.ErrOpen` (unchanged sentinel; now matched via `errors.Is`)
  - unexported `openError` implementing `Error()`, `Unwrap() error`, `RetryAfter() time.Duration`
  - `(*circuitbreaker.Breaker).RetryAfter() time.Duration`
  - unexported `newConfig(opts ...Option) config` (used by Task 4)
- Consumes: nothing.

- [ ] **Step 1: Write failing black-box tests**

Append to `resilience/circuitbreaker/circuitbreaker_test.go`:

```go
func TestOpenErrorIsMatchableAndCarriesRetryAfter(t *testing.T) {
	b := circuitbreaker.New(
		circuitbreaker.WithFailureThreshold(1),
		circuitbreaker.WithOpenTimeout(30*time.Second),
	)
	_ = b.Do(t.Context(), fail) // trips open after one failure
	err := b.Do(t.Context(), ok) // rejected; ok is not run

	assert.ErrorIs(t, err, circuitbreaker.ErrOpen)
	var ra interface{ RetryAfter() time.Duration }
	assert.ErrorAs(t, err, &ra)
	d := ra.RetryAfter()
	assert.Greater(t, d, time.Duration(0))
	assert.LessOrEqual(t, d, 30*time.Second)
}

func TestRetryAfterShrinksAndZeroesOut(t *testing.T) {
	clk := clock.NewMock(time.Now())
	b := circuitbreaker.New(
		circuitbreaker.WithFailureThreshold(1),
		circuitbreaker.WithOpenTimeout(30*time.Second),
		circuitbreaker.WithClock(clk),
	)
	_ = b.Do(t.Context(), fail)

	assert.Equal(t, 30*time.Second, b.RetryAfter())
	clk.Advance(10 * time.Second)
	assert.Equal(t, 20*time.Second, b.RetryAfter())
	clk.Advance(30 * time.Second) // past the open window
	assert.Equal(t, time.Duration(0), b.RetryAfter())
}
```

These reuse the existing package-level `fail`/`ok`/testify `assert` from `circuitbreaker_test.go` — do not redeclare them.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./resilience/circuitbreaker/ -run 'TestOpenError|TestRetryAfter' 2>&1 | head`
Expected: FAIL — `b.RetryAfter` undefined.

- [ ] **Step 3: Replace `errors.go`**

```go
package circuitbreaker

import (
	"errors"
	"time"
)

// ErrOpen is the sentinel a rejected call matches with errors.Is. The concrete
// error returned by Do also reports a suggested retry delay via RetryAfter.
var ErrOpen = errors.New("circuitbreaker: circuit open")

// openError is returned when the breaker rejects a call. It unwraps to ErrOpen
// (so errors.Is(err, ErrOpen) holds) and reports the delay until the next
// probe, satisfying retry.RetryAfterError and feeding the HTTP middleware's
// Retry-After header.
type openError struct{ retryAfter time.Duration }

func (e *openError) Error() string             { return ErrOpen.Error() }
func (e *openError) Unwrap() error             { return ErrOpen }
func (e *openError) RetryAfter() time.Duration { return e.retryAfter }
```

- [ ] **Step 4: Refactor `circuitbreaker.go` — `newConfig`, `New`, `before`, `RetryAfter`**

Replace the existing `func New(...)` with:

```go
// newConfig applies opts over the defaults. Shared by New and Group.
func newConfig(opts ...Option) config {
	c := config{threshold: 5, openTimeout: 30 * time.Second, halfOpenMax: 1, clk: clock.System()}
	for _, o := range opts {
		o(&c)
	}
	return c
}

// New builds a Breaker from options.
func New(opts ...Option) *Breaker {
	return &Breaker{cfg: newConfig(opts...), state: StateClosed}
}
```

Replace the existing `func (b *Breaker) before()` with:

```go
func (b *Breaker) before() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case StateOpen:
		elapsed := b.cfg.clk.Now().Sub(b.openedAt)
		if elapsed < b.cfg.openTimeout {
			return &openError{retryAfter: b.cfg.openTimeout - elapsed}
		}
		b.transition(StateHalfOpen)
		b.halfOpenIn = 1
		return nil
	case StateHalfOpen:
		if b.halfOpenIn >= b.cfg.halfOpenMax {
			return &openError{retryAfter: 0} // a probe is in flight; retry shortly
		}
		b.halfOpenIn++
		return nil
	default:
		return nil
	}
}
```

Add this method (next to `State()`):

```go
// RetryAfter reports how long until the breaker would admit a probe call, or 0
// when it is not open.
func (b *Breaker) RetryAfter() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state != StateOpen {
		return 0
	}
	if remaining := b.cfg.openTimeout - b.cfg.clk.Now().Sub(b.openedAt); remaining > 0 {
		return remaining
	}
	return 0
}
```

- [ ] **Step 5: Verify no `== ErrOpen` equality checks remain**

Run: `grep -rn "== circuitbreaker.ErrOpen\|== ErrOpen" resilience/circuitbreaker/`
Expected: no matches — the existing tests already use `assert.ErrorIs(..., circuitbreaker.ErrOpen)` (which keeps working since `openError` unwraps to `ErrOpen`). If a hit does appear, change `err == ErrOpen` to `errors.Is(err, ErrOpen)`.

- [ ] **Step 6: Format, test**

Run: `just fmt resilience/circuitbreaker/errors.go resilience/circuitbreaker/circuitbreaker.go resilience/circuitbreaker/circuitbreaker_test.go`
Run: `go test ./resilience/circuitbreaker/ -race`
Expected: PASS (existing + new).

- [ ] **Step 7: Commit**

```bash
git add resilience/circuitbreaker/errors.go resilience/circuitbreaker/circuitbreaker.go resilience/circuitbreaker/circuitbreaker_test.go
git commit -m "feat(resilience/circuitbreaker): open error carries RetryAfter; add Breaker.RetryAfter"
```

---

### Task 4: circuitbreaker — keyed `Group` with idle-TTL eviction

**Files:**
- Create: `resilience/circuitbreaker/group.go`
- Test: `resilience/circuitbreaker/group_test.go` (black-box)

**Interfaces:**
- Consumes (from Task 3): `newConfig`, `New`, `*Breaker`, `Breaker.State`, `Breaker.RetryAfter`, `State`, `StateClosed`, `Option`, `WithClock`.
- Produces:
  - `circuitbreaker.Group` (struct)
  - `circuitbreaker.GroupOption func(*groupConfig)`
  - `WithBreakerOptions(opts ...Option) GroupOption`
  - `WithIdleTTL(d time.Duration) GroupOption`
  - `WithSweepInterval(d time.Duration) GroupOption`
  - `NewGroup(opts ...GroupOption) *Group`
  - `(*Group).Do(ctx, key string, fn func(context.Context) error) error`
  - `(*Group).State(key string) State`
  - `(*Group).Len() int`
  - unexported `(*Group).breaker(key string) *Breaker` (used by Task 5)

- [ ] **Step 1: Write failing black-box tests**

Create `resilience/circuitbreaker/group_test.go`. It is package `circuitbreaker_test`, so it reuses the package-level `fail`/`ok` helpers and testify `assert` already declared in `circuitbreaker_test.go` — do not redeclare them:

```go
package circuitbreaker_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/circuitbreaker"
)

func TestGroupLazyCreateAndIndependence(t *testing.T) {
	g := circuitbreaker.NewGroup(circuitbreaker.WithBreakerOptions(
		circuitbreaker.WithFailureThreshold(1),
	))
	assert.Equal(t, 0, g.Len())

	_ = g.Do(t.Context(), "a", fail) // trips "a"
	_ = g.Do(t.Context(), "b", ok)   // healthy "b"

	assert.Equal(t, 2, g.Len())
	assert.Equal(t, circuitbreaker.StateOpen, g.State("a"))
	assert.Equal(t, circuitbreaker.StateClosed, g.State("b"))
	assert.Equal(t, circuitbreaker.StateClosed, g.State("never-seen"))
}

func TestGroupEvictsIdleBreakers(t *testing.T) {
	clk := clock.NewMock(time.Now())
	g := circuitbreaker.NewGroup(
		circuitbreaker.WithBreakerOptions(circuitbreaker.WithClock(clk)),
		circuitbreaker.WithIdleTTL(time.Minute),
		circuitbreaker.WithSweepInterval(time.Second),
	)
	_ = g.Do(t.Context(), "a", ok) // create "a" at t0
	clk.Advance(2 * time.Minute)
	_ = g.Do(t.Context(), "b", ok) // sweep interval elapsed -> "a" idle > 1m -> evicted

	assert.Equal(t, 1, g.Len())
	assert.Equal(t, circuitbreaker.StateClosed, g.State("a")) // evicted -> reads Closed
}

func TestGroupActiveKeySurvivesSweep(t *testing.T) {
	clk := clock.NewMock(time.Now())
	g := circuitbreaker.NewGroup(
		circuitbreaker.WithBreakerOptions(
			circuitbreaker.WithClock(clk),
			circuitbreaker.WithFailureThreshold(2),
		),
		circuitbreaker.WithIdleTTL(time.Minute),
		circuitbreaker.WithSweepInterval(time.Second),
	)
	_ = g.Do(t.Context(), "a", fail) // a: 1 failure of 2 -> still Closed
	clk.Advance(90 * time.Second)
	// breaker() refreshes a's last-access BEFORE the sweep, so the ORIGINAL a
	// survives and its SECOND failure trips it. A recreated breaker would be
	// back to one failure and stay Closed — so StateOpen proves preservation.
	_ = g.Do(t.Context(), "a", fail)
	assert.Equal(t, circuitbreaker.StateOpen, g.State("a"),
		"active key's breaker must be preserved across the sweep")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./resilience/circuitbreaker/ -run TestGroup 2>&1 | head`
Expected: FAIL to build — `circuitbreaker.NewGroup` undefined.

- [ ] **Step 3: Create `group.go`**

```go
package circuitbreaker

import (
	"context"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

type groupConfig struct {
	breakerOpts   []Option
	idleTTL       time.Duration
	sweepInterval time.Duration
}

// GroupOption configures a Group.
type GroupOption func(*groupConfig)

// WithBreakerOptions sets the options applied to every per-key breaker the
// Group creates (including WithClock, which the Group reuses for eviction).
func WithBreakerOptions(opts ...Option) GroupOption {
	return func(c *groupConfig) { c.breakerOpts = opts }
}

// WithIdleTTL evicts a breaker after it has gone this long with no Do call,
// regardless of state (default 10m). Active traffic refreshes a breaker's
// last-access time, so a breaker in use is never evicted. Non-positive ignored.
func WithIdleTTL(d time.Duration) GroupOption {
	return func(c *groupConfig) {
		if d > 0 {
			c.idleTTL = d
		}
	}
}

// WithSweepInterval sets the minimum clock gap between eviction scans
// (default 1m). Scans run during Do, at most once per interval, so an idle
// Group does no work and holds no goroutine. Non-positive ignored.
func WithSweepInterval(d time.Duration) GroupOption {
	return func(c *groupConfig) {
		if d > 0 {
			c.sweepInterval = d
		}
	}
}

type groupEntry struct {
	breaker    *Breaker
	lastAccess time.Time
}

// Group manages breakers keyed by string, creating each on first use and
// sharing one option set. Breakers with no Do call for longer than the idle TTL
// are evicted opportunistically during Do, so an unbounded key space cannot
// leak memory and no background goroutine is needed. Safe for concurrent use.
type Group struct {
	entries   map[string]*groupEntry
	clk       clock.Clock
	lastSweep time.Time
	cfg       groupConfig
	mu        sync.Mutex
}

// NewGroup builds a Group. Configure per-key breakers with WithBreakerOptions
// and eviction with WithIdleTTL / WithSweepInterval.
func NewGroup(opts ...GroupOption) *Group {
	gc := groupConfig{idleTTL: 10 * time.Minute, sweepInterval: time.Minute}
	for _, o := range opts {
		o(&gc)
	}
	clk := newConfig(gc.breakerOpts...).clk // reuse the breaker clock
	return &Group{
		entries:   make(map[string]*groupEntry),
		clk:       clk,
		lastSweep: clk.Now(),
		cfg:       gc,
	}
}

// Do runs fn under key's breaker, creating it on first use. It returns an error
// matching ErrOpen when that breaker is open.
func (g *Group) Do(ctx context.Context, key string, fn func(context.Context) error) error {
	return g.breaker(key).Do(ctx, fn)
}

// State reports the state of key's breaker, or StateClosed if key has no
// breaker (never called, or evicted). Querying state is not traffic: it neither
// refreshes last-access nor triggers a sweep.
func (g *Group) State(key string) State {
	g.mu.Lock()
	defer g.mu.Unlock()
	if e, ok := g.entries[key]; ok {
		return e.breaker.State()
	}
	return StateClosed
}

// Len reports the number of live breakers (for tests and metrics).
func (g *Group) Len() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.entries)
}

// breaker returns key's breaker, creating it on first use. It refreshes the
// requested key's last-access time BEFORE running the eviction scan, so a key
// touched this call is never evicted this call (an in-use key always survives).
// The scan runs at most once per sweep interval. Shared by Do and the HTTP
// middleware.
func (g *Group) breaker(key string) *Breaker {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.clk.Now()
	e, ok := g.entries[key]
	if ok {
		e.lastAccess = now // refresh before the sweep so an active key survives
	}
	if now.Sub(g.lastSweep) >= g.cfg.sweepInterval {
		g.sweepLocked(now)
		g.lastSweep = now
	}
	if ok {
		return e.breaker
	}
	b := New(g.cfg.breakerOpts...)
	g.entries[key] = &groupEntry{breaker: b, lastAccess: now}
	return b
}

// sweepLocked drops entries idle beyond idleTTL. Caller holds g.mu.
func (g *Group) sweepLocked(now time.Time) {
	cutoff := now.Add(-g.cfg.idleTTL)
	for k, e := range g.entries {
		if e.lastAccess.Before(cutoff) {
			delete(g.entries, k)
		}
	}
}
```

- [ ] **Step 4: Format, test**

Run: `just fmt resilience/circuitbreaker/group.go resilience/circuitbreaker/group_test.go`
Run: `go test ./resilience/circuitbreaker/ -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add resilience/circuitbreaker/group.go resilience/circuitbreaker/group_test.go
git commit -m "feat(resilience/circuitbreaker): keyed Group with opportunistic idle-TTL eviction"
```

---

### Task 5: circuitbreaker — HTTP middleware (`net/http` only)

**Files:**
- Create: `resilience/circuitbreaker/http.go`
- Test: `resilience/circuitbreaker/http_test.go` (black-box)

**Interfaces:**
- Consumes: `*Breaker`, `*Group`, `Breaker.Do`, `(*Group).breaker`, `openError`.
- Produces:
  - `KeyFunc func(*http.Request) string`
  - `KeyByHost(*http.Request) string`
  - `OpenResponder func(http.ResponseWriter, *http.Request, time.Duration)`
  - `MiddlewareOption func(*middlewareConfig)`
  - `WithFailurePredicate(func(status int) bool) MiddlewareOption`
  - `WithOpenResponder(OpenResponder) MiddlewareOption`
  - `Middleware(b *Breaker, opts ...MiddlewareOption) func(http.Handler) http.Handler`
  - `GroupMiddleware(g *Group, key KeyFunc, opts ...MiddlewareOption) func(http.Handler) http.Handler`
  - `GroupKey(g *Group, key string, opts ...MiddlewareOption) func(http.Handler) http.Handler`

- [ ] **Step 1: Write failing black-box tests**

Create `resilience/circuitbreaker/http_test.go`:

```go
package circuitbreaker_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/resilience/circuitbreaker"
)

// runMW serves one request through mw(h) and returns the recorder.
func runMW(mw func(http.Handler) http.Handler, h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mw(h).ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

// status returns a handler that writes the given status code.
func status(code int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) })
}

func TestMiddlewarePassesThroughWhenClosed(t *testing.T) {
	rec := runMW(circuitbreaker.Middleware(circuitbreaker.New()), status(200), "GET", "/")
	assert.Equal(t, 200, rec.Code)
}

func TestMiddlewareTripsAndReturns503WithRetryAfter(t *testing.T) {
	b := circuitbreaker.New(circuitbreaker.WithFailureThreshold(1), circuitbreaker.WithOpenTimeout(30*time.Second))
	mw := circuitbreaker.Middleware(b)

	assert.Equal(t, 500, runMW(mw, status(500), "GET", "/").Code) // downstream 500 reaches client
	rec := runMW(mw, status(500), "GET", "/")                     // breaker now open
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("Retry-After"))
}

func TestMiddlewareCustomFailurePredicate(t *testing.T) {
	b := circuitbreaker.New(circuitbreaker.WithFailureThreshold(1))
	mw := circuitbreaker.Middleware(b, circuitbreaker.WithFailurePredicate(func(s int) bool { return s == 429 }))

	_ = runMW(mw, status(429), "GET", "/") // 429 counts as failure -> opens
	assert.Equal(t, http.StatusServiceUnavailable, runMW(mw, status(429), "GET", "/").Code)
}

func TestMiddlewareCustomOpenResponder(t *testing.T) {
	b := circuitbreaker.New(circuitbreaker.WithFailureThreshold(1))
	mw := circuitbreaker.Middleware(b, circuitbreaker.WithOpenResponder(
		func(w http.ResponseWriter, _ *http.Request, _ time.Duration) { w.WriteHeader(http.StatusTooManyRequests) }))

	_ = runMW(mw, status(500), "GET", "/")
	assert.Equal(t, http.StatusTooManyRequests, runMW(mw, status(500), "GET", "/").Code)
}

func TestGroupKeyStaticKey(t *testing.T) {
	g := circuitbreaker.NewGroup(circuitbreaker.WithBreakerOptions(circuitbreaker.WithFailureThreshold(1)))
	mw := circuitbreaker.GroupKey(g, "checkout")

	_ = runMW(mw, status(500), "GET", "/")
	assert.Equal(t, http.StatusServiceUnavailable, runMW(mw, status(500), "GET", "/").Code)
	assert.Equal(t, circuitbreaker.StateOpen, g.State("checkout"))
}

func TestGroupKeyEmptyBypasses(t *testing.T) {
	g := circuitbreaker.NewGroup()
	assert.Equal(t, 204, runMW(circuitbreaker.GroupKey(g, ""), status(204), "GET", "/").Code)
	assert.Equal(t, 0, g.Len())
}

func TestGroupMiddlewareKeyByHostIndependent(t *testing.T) {
	g := circuitbreaker.NewGroup(circuitbreaker.WithBreakerOptions(circuitbreaker.WithFailureThreshold(1)))
	mw := circuitbreaker.GroupMiddleware(g, circuitbreaker.KeyByHost)

	trip := func(host string) int {
		rec := httptest.NewRecorder()
		mw(status(500)).ServeHTTP(rec, httptest.NewRequest("GET", "http://"+host+"/", nil))
		return rec.Code
	}
	_ = trip("a.example")
	_ = trip("a.example")                   // a.example now open
	assert.Equal(t, 500, trip("b.example")) // b.example independent -> its 500 reaches client
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./resilience/circuitbreaker/ -run 'TestMiddleware|TestGroupKey|TestGroupMiddleware' 2>&1 | head`
Expected: FAIL to build — `circuitbreaker.Middleware` undefined.

- [ ] **Step 3: Create `http.go`**

```go
package circuitbreaker

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"
)

// KeyFunc selects the breaker key for a request in GroupMiddleware. Returning
// an empty string skips the breaker for that request.
type KeyFunc func(*http.Request) string

// KeyByHost keys a Group breaker by request Host — the common reverse-proxy
// case, so GroupMiddleware needs no custom key code. Any func(*http.Request)
// string works for other strategies.
func KeyByHost(r *http.Request) string { return r.Host }

// OpenResponder writes the response when the circuit is open. retryAfter is the
// suggested delay before retrying (0 if unknown).
type OpenResponder func(w http.ResponseWriter, r *http.Request, retryAfter time.Duration)

type middlewareConfig struct {
	isFailure func(status int) bool
	onOpen    OpenResponder
}

// MiddlewareOption configures Middleware, GroupMiddleware, and GroupKey.
type MiddlewareOption func(*middlewareConfig)

// WithFailurePredicate classifies a downstream response status as a breaker
// failure. Default: status >= 500. A nil predicate is ignored.
func WithFailurePredicate(fn func(status int) bool) MiddlewareOption {
	return func(c *middlewareConfig) {
		if fn != nil {
			c.isFailure = fn
		}
	}
}

// WithOpenResponder overrides the response written when the circuit is open.
// The default writes 503 with a Retry-After header and a plain-text body. A nil
// responder is ignored.
func WithOpenResponder(fn OpenResponder) MiddlewareOption {
	return func(c *middlewareConfig) {
		if fn != nil {
			c.onOpen = fn
		}
	}
}

func newMiddlewareConfig(opts ...MiddlewareOption) middlewareConfig {
	c := middlewareConfig{
		isFailure: func(status int) bool { return status >= 500 },
		onOpen:    defaultOpenResponder,
	}
	for _, o := range opts {
		o(&c)
	}
	return c
}

// Middleware guards next with b. When the circuit is open it responds via the
// open responder (503 + Retry-After by default) without calling next. When the
// circuit is closed it calls next; a response whose status the failure
// predicate matches is recorded as a breaker failure, while the response itself
// still reaches the client unchanged. The returned value is assignable to
// web/middleware.Middleware.
func Middleware(b *Breaker, opts ...MiddlewareOption) func(http.Handler) http.Handler {
	cfg := newMiddlewareConfig(opts...)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			serve(b, cfg, next, w, r)
		})
	}
}

// GroupMiddleware guards next with a per-request breaker chosen by key from g.
// A request whose key is empty bypasses the breaker. Otherwise identical to
// Middleware.
func GroupMiddleware(g *Group, key KeyFunc, opts ...MiddlewareOption) func(http.Handler) http.Handler {
	cfg := newMiddlewareConfig(opts...)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			k := key(r)
			if k == "" {
				next.ServeHTTP(w, r)
				return
			}
			serve(g.breaker(k), cfg, next, w, r)
		})
	}
}

// GroupKey guards next with the breaker named key from g — a fixed key chosen
// at wrap time, so a specific route handler gets its own breaker within one
// managed Group (shared options, unified eviction and introspection). An empty
// key bypasses the breaker.
func GroupKey(g *Group, key string, opts ...MiddlewareOption) func(http.Handler) http.Handler {
	cfg := newMiddlewareConfig(opts...)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}
			serve(g.breaker(key), cfg, next, w, r)
		})
	}
}

// errDownstreamFailure signals to the breaker that the guarded handler produced
// a failure status. It never leaves the middleware.
var errDownstreamFailure = errors.New("circuitbreaker: downstream failure")

func serve(b *Breaker, cfg middlewareConfig, next http.Handler, w http.ResponseWriter, r *http.Request) {
	rec := &statusRecorder{ResponseWriter: w}
	err := b.Do(r.Context(), func(ctx context.Context) error {
		next.ServeHTTP(rec, r.WithContext(ctx))
		if cfg.isFailure(rec.status()) {
			return errDownstreamFailure
		}
		return nil
	})
	if err == nil || errors.Is(err, errDownstreamFailure) {
		return // downstream already wrote its response (success or a matched failure)
	}
	var oe *openError
	if errors.As(err, &oe) {
		cfg.onOpen(w, r, oe.retryAfter)
	}
}

func defaultOpenResponder(w http.ResponseWriter, _ *http.Request, retryAfter time.Duration) {
	if secs := retryAfterSeconds(retryAfter); secs > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(secs))
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte("service unavailable: circuit open\n"))
}

// retryAfterSeconds rounds a positive delay up to whole seconds (Retry-After is
// second-granular). Returns 0 for a non-positive delay so the header is omitted.
func retryAfterSeconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	secs := int(d / time.Second)
	if d%time.Second != 0 {
		secs++
	}
	return secs
}

// statusRecorder captures the response status for failure classification while
// passing writes through unchanged. It exposes the underlying writer via Unwrap
// so http.ResponseController reaches optional interfaces (Flusher for
// SSE/streaming, Hijacker, SetWriteDeadline, …) without falsely advertising
// them when the underlying writer lacks them.
type statusRecorder struct {
	http.ResponseWriter
	code  int
	wrote bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.wrote {
		return
	}
	r.code = code
	r.wrote = true
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(b)
}

// status returns the committed status, or 200 when the handler wrote a body or
// nothing without an explicit WriteHeader (net/http's implicit 200).
func (r *statusRecorder) status() int {
	if !r.wrote {
		return http.StatusOK
	}
	return r.code
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
```

- [ ] **Step 4: Format, build (assert no web import), test**

Run: `just fmt resilience/circuitbreaker/http.go resilience/circuitbreaker/http_test.go`
Run: `go test ./resilience/circuitbreaker/ -race`
Run: `go list -deps ./resilience/circuitbreaker/ | grep -c 'forge/web' || true`
Expected: tests PASS; the `grep -c` prints `0` (no `web/` dependency).

- [ ] **Step 5: Commit**

```bash
git add resilience/circuitbreaker/http.go resilience/circuitbreaker/http_test.go
git commit -m "feat(resilience/circuitbreaker): HTTP middleware (single, group, static key)"
```

---

### Task 6: docs, package-set sync, and final verification

**Files:**
- Modify: `resilience/cache/doc.go` (NX/claim example)
- Modify: `resilience/circuitbreaker/doc.go` (Group + Middleware example)
- Modify: `resilience/retry/doc.go` (RetryAfter note)
- (`docs/packages.md` is deliberately NOT edited here — see Step 4.)

**Interfaces:** none produced/consumed; documentation + verification only.

- [ ] **Step 1: Add the cache claim example to `resilience/cache/doc.go`**

Append inside the package doc comment (above `package cache`):

```go
// Claim a one-shot key (idempotency / session start):
//
//	err := store.Set(ctx, "idem:"+key, marker, cache.WithSetNonExist(), cache.WithTTL(24*time.Hour))
//	switch {
//	case errors.Is(err, cache.ErrExists):
//		// lost the claim — replay or reject
//	case err != nil:
//		// real failure
//	default:
//		// won the claim — do the work exactly once
//	}
```

- [ ] **Step 2: Add a runnable `Example` to circuitbreaker docs**

Append to `resilience/circuitbreaker/doc.go` (or create `example_test.go` in package `circuitbreaker_test` if `doc.go` holds only the package comment — prefer a runnable `Example` in a test file so it is compiled):

Create `resilience/circuitbreaker/example_test.go`:

```go
package circuitbreaker_test

import (
	"net/http"

	"github.com/dmitrymomot/forge/resilience/circuitbreaker"
)

func ExampleGroupKey() {
	g := circuitbreaker.NewGroup()
	mux := http.NewServeMux()
	checkout := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})
	mux.Handle("/checkout", circuitbreaker.GroupKey(g, "checkout")(checkout))
	// Output:
}
```

- [ ] **Step 3: Add the retry note to `resilience/retry/doc.go`**

Append inside the package doc comment:

```go
// An error implementing RetryAfterError (RetryAfter() time.Duration) raises the
// wait before the next attempt to at least the reported duration — e.g. an HTTP
// 429/503 Retry-After or a circuitbreaker open error is honored automatically.
```

- [ ] **Step 4: (Post-merge, NOT in this PR) note the shipped work items**

Do **not** edit `docs/packages.md` in this PR. Per the spec's "Post-merge doc sync", once this merges, remove these three now-completed rows from the "Shipped-package work items" table (they are no longer pending):
- the `resilience/circuitbreaker` "Keyed `Group` + HTTP middleware adapter" row
- the `resilience/retry` "Honor `RetryAfter()` errors as the delay floor" row
- the `resilience/cache` "Atomic SetNX in the Store contract" row

Keeping `packages.md` out of this PR also avoids sweeping the earlier, already-committed cache/postgres change. No action in this step.

- [ ] **Step 5: Format, full race suite, lint**

Run: `just fmt resilience/cache/doc.go resilience/circuitbreaker/doc.go resilience/circuitbreaker/example_test.go resilience/retry/doc.go`
Run: `go test ./resilience/... -race`
Run: `just lint`
Expected: all tests PASS; lint clean (fix any findings before committing — e.g. `modernize` may rewrite `new(...)`/loop forms; re-run until clean).

- [ ] **Step 6: Commit**

```bash
git add resilience/cache/doc.go resilience/circuitbreaker/doc.go resilience/circuitbreaker/example_test.go resilience/retry/doc.go
git commit -m "docs(resilience): claim/Group/RetryAfter examples"
```

---

## Notes for the implementer

- **`clock.WithClock` reuse:** `WithClock` is a `circuitbreaker.Option`. Pass it to a `Group` via `WithBreakerOptions(WithClock(mock))`; `NewGroup` derives the group's own clock from those breaker options, so one mock drives both breakers and eviction.
- **Why the interface flip is one task:** changing `Store.Set` breaks `memory`, `Cache[V]`, and `cache/redis` at once; splitting them would leave `go build ./...` broken between commits.
- **`errDownstreamFailure` never escapes:** it is the sentinel `fn` returns so the breaker counts a 5xx; `serve` swallows it (the real downstream response is already written).
- **Eviction determinism:** the sweep reads the injected clock, and the janitor is opportunistic (runs inside `Do`), so `clock.Mock` + `Advance` + one `Do` deterministically triggers eviction — no `time.Sleep` in tests.
- **`docs/packages.md` is intentionally untouched by this plan.** The cache/postgres removal was already committed separately (`da9be85`); the shipped-work-item tick is a post-merge follow-up (Task 6, Step 4). This keeps the working tree clean so no plan commit sweeps unrelated hunks.
