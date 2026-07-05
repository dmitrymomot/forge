# Resilience Seam Bundle — Design Spec

> Date: 2026-07-04 · Status: awaiting approval · Ships as one PR.
> Unblocks: `web/httpclient`, and the `session`/`idempotency`/`otp`/`lockout` line.

Three additions across three shipped `resilience/` packages. This spec contains
complete, compilable reference implementations for every changed or new symbol —
no sketches. Black-box tests only; options pattern (never builders); `errors.Is`
sentinels; minimal deps.

## Locked decisions (from brainstorming)

1. **cache**: no new `Store` method. `Set` gains variadic `...SetOption`; TTL and
   NX are options. NX-conflict is the `ErrExists` sentinel (consistent with
   `ErrNotFound`-on-miss). `SetOptions` is **exported** because `cache/redis`
   applies options across the package boundary. `Cache[V]` mirrors the seam.
2. **redis**: use `SetArgs{Mode:"NX"}` (`SET … NX`), not the (now-internally-fixed
   but still legacy) `SetNX` helper. `redis.Nil` → `ErrExists`.
3. **circuitbreaker**: `Group` = lazy per-key breakers with **idle-TTL eviction,
   any state**, done **opportunistically on `Do`** (no goroutine, no `Close`) —
   see rationale below. Ships **both** `Middleware(*Breaker,…)` and
   `GroupMiddleware(*Group, KeyFunc,…)`. Open error carries a dynamic
   `RetryAfter()`.
4. **retry**: honor a `RetryAfterError` interface as a delay **floor**
   (`max(backoff, hint)`), uncapped by the backoff ceiling; `ctx` still bounds.
5. **Layering**: `resilience/` stays free of `web/` imports. The breaker
   middleware uses a small `net/http`-only status recorder rather than importing
   `web/middleware.WrapWriter`. (Tradeoff: ~30 duplicated lines vs. preserving the
   resilience-is-foundational dependency direction, since `web/httpclient` imports
   `resilience`. Flagged for override.)

### Why `Group` evicts opportunistically, not via a janitor

`clock.Clock` exposes only `Now()` — no ticker. A background sweeper would run on
real wall-time, so a `Mock` clock couldn't drive it and eviction wouldn't be
black-box testable deterministically; it would also add a goroutine + `Close()`
lifecycle to a primitive.

Instead, `Group.breaker(key)` (the get-or-create path shared by `Do` and the
middleware) runs an eviction scan at most once per `sweepInterval` of the injected
clock, gated by a `lastSweep` timestamp. Properties:

- **Bounds memory under the only condition that grows the map** — a steady stream
  of new keys. Each such call passes through the gated scan. No traffic ⇒ no
  growth ⇒ nothing to reclaim.
- **Zero idle cost, no goroutine, no `Close()`.**
- **No thundering herd**: eviction is by *idle time*, and any active key refreshes
  `lastAccess` on every call, so a key under load is never idle and never evicted.
  Only zero-traffic keys are dropped — forgetting one is harmless.
- **Deterministic tests**: inject a `Mock` clock via `WithBreakerOptions(WithClock(m))`;
  `m.Advance(...)` + one `Do` triggers the scan; assert via `Group.Len()`.

## Design invariants — no global state, no boilerplate

Guaranteed for every symbol in this bundle; enforced in review and by the tests
below.

**No global or shared mutable state.**
- No `init()`, no package-level singletons, no default global instances, no
  registries. Every stateful object (`Cache`, `Store`, `Breaker`, `Group`,
  `Retrier`) is explicitly constructed and injected; two instances never share
  hidden state.
- The only package-level vars are immutable error sentinels (`ErrExists`,
  `ErrOpen`, unexported `errDownstreamFailure`) and stateless funcs
  (`defaultOpenResponder`, `KeyByHost`) — matching the existing `ErrNotFound`
  idiom.
- Nothing is smuggled through `context`; the open-circuit delay reaches a custom
  responder as an explicit `OpenResponder` parameter.

**Every feature works on zero-config defaults — no wiring boilerplate.**
- `store.Set(ctx, k, v, cache.WithSetNonExist())` — atomic claim, nothing to set up.
- `circuitbreaker.New().Do(...)`, `NewGroup().Do(...)`, `Middleware(b)(h)`,
  `GroupMiddleware(g, KeyByHost)(h)`, `GroupKey(g, "checkout")(h)` — all usable
  with no options; defaults are sensible (503 + Retry-After, status ≥ 500 =
  failure, 10m idle TTL, 5m cache TTL).
- The **retry ↔ breaker link is structural**: `openError` satisfies
  `retry.RetryAfterError` by shape, so `retry` honors the delay with **no import,
  no registration, no adapter** — `circuitbreaker` never imports `retry`. The
  same holds for a future `httpclient` 429/503 error.

**The only two "you must supply this" points are essential inputs, not boilerplate.**
1. A custom `Store` implementer writes one line — `cache.ApplySetOptions(opts...)`
   — to read the options. Unavoidable for functional options crossing a package
   boundary; both shipped backends already do it.
2. `GroupMiddleware` takes a `KeyFunc` because a per-request keying strategy is
   required information; `KeyByHost` ships for the common case. When you know the
   route at mount time, `GroupKey(g, "checkout")` sets the key explicitly at the
   wrap site — no request-derived key at all.

---

## Component 1 — `resilience/cache`

### 1a. `errors.go` — add one sentinel

```go
	// ErrExists is returned by Set with WithSetNonExist when the key is present.
	ErrExists = errors.New("cache: entry already exists")
```

### 1b. `store.go` — options replace the `ttl` parameter

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

### 1c. `memory.go` — `Set` becomes options-aware (atomic under the existing mutex)

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

### 1d. `redis/redis.go` — `SET … NX` via `SetArgs`, no legacy `SETNX`

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

### 1e. `cache.go` — facade mirrors the seam

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

`GetOrSet`'s internal write preserves its "fn returns 0 ⇒ default TTL" semantics
by only emitting `WithTTL` for a non-zero duration:

```go
	v, _, err = c.sf.Do(ctx, c.key(key), func(ctx context.Context) (V, error) {
		val, ttl, ferr := fn(ctx)
		if ferr != nil {
			var zero V
			return zero, ferr
		}
		var opts []SetOption
		if ttl != 0 {
			opts = append(opts, WithTTL(ttl))
		}
		_ = c.Set(ctx, key, val, opts...)
		return val, nil
	})
```

### 1f. Facade TTL semantics (unchanged behavior, new spelling)

| Caller writes | Store receives | Effect |
|---|---|---|
| `c.Set(ctx,k,v)` | `WithTTL(defaultTTL)` | default (5m) |
| `c.Set(ctx,k,v, WithTTL(d>0))` | `WithTTL(d)` | expires after `d` |
| `c.Set(ctx,k,v, WithTTL(0))` / `WithTTL(<0)` | `WithTTL(<=0)` | never expires |
| `c.Set(ctx,k,v, WithSetNonExist())` | `WithTTL(default)+NX` | claim w/ default TTL |

### 1g. `doc.go` — runnable example addition

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

---

## Component 2 — `resilience/circuitbreaker`

### 2a. `errors.go` — dynamic open error carrying `RetryAfter`

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

> **Behavior change:** `Do` now returns `*openError` instead of the bare `ErrOpen`
> value. `errors.Is(err, ErrOpen)` still holds. Any consumer/test using `err ==
> ErrOpen` must switch to `errors.Is` (the sanctioned check). `doc.go` updated to
> say "an error matching ErrOpen".

### 2b. `circuitbreaker.go` — shared config builder, remaining-time in `before`, public `RetryAfter`

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

### 2c. `group.go` — new file, complete

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
// touched this call is never evicted this call (an active key always survives).
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

### 2d. `http.go` — new file, complete (both middlewares, `net/http` only)

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
// key bypasses the breaker. Use this when you know the route at mount time:
//
//	mux.Handle("/checkout", circuitbreaker.GroupKey(g, "checkout")(checkoutHandler))
//	mux.Handle("/search",   circuitbreaker.GroupKey(g, "search")(searchHandler))
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
// them when the underlying writer lacks them. This mirrors web/middleware's
// recorder convention.
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

### 2e. `doc.go` — add a runnable `Example` mounting `Middleware` and showing `Group`.

---

## Component 3 — `resilience/retry`

### 3a. `errors.go` — new file with the contract interface

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

### 3b. `retry.go` — apply the floor in the `Do` loop

Only the delay computation changes (replacing the single `time.NewTimer` line):

```go
		// Wait per the backoff, raised to any server/breaker-stated floor.
		wait := r.cfg.backoff.Next(attempt)
		if ra, ok := errors.AsType[RetryAfterError](err); ok {
			if hint := ra.RetryAfter(); hint > wait {
				wait = hint
			}
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
```

`errors.AsType[RetryAfterError]` walks the unwrap chain and returns the first
error whose dynamic type implements `RetryAfter()` (Go 1.26 `asType` does a
type-assert to the interface `E`). `retryIf` and `maxAttempts` are untouched —
the hint changes only *how long* a retry waits, never *whether* it happens.

---

## Testing plan (black-box, `_test` packages)

**cache** (`cache_test`, `redis_test`):
- `WithSetNonExist` on absent key → stored, `ok`; on present live key → `ErrExists`,
  original value intact; on **expired** key → overwrites (mock clock advance).
- `WithTTL` expiry vs. no-option no-expiry vs. negative → never; via memory mock clock.
- Facade: default TTL applied when no `WithTTL`; explicit `WithTTL` overrides;
  `GetOrSet` still uses default when `fn` returns `0`.
- Concurrency: N goroutines `WithSetNonExist` same key → exactly one `ok`
  (race detector).
- redis path gated behind the existing redis test build tag/skip.

**circuitbreaker** (`circuitbreaker_test`):
- `openError`: `errors.Is(err, ErrOpen)` true; `RetryAfter()` > 0 while open and
  shrinks as the mock clock advances; 0 once half-open/closed.
- `Group`: lazy create (`Len` grows); shared options honored; idle eviction —
  advance mock clock past `idleTTL`, one `Do` on another key triggers the scan,
  assert `Len` drops and the idle key’s `State` is `StateClosed`; an *active* key
  survives; `State`/`Len` do not themselves cause eviction.
- `Middleware`: closed→passes through, 5xx trips after threshold; open→503 +
  `Retry-After` header without calling next; custom `WithFailurePredicate` and
  `WithOpenResponder`; a handler flushing via `http.ResponseController` still
  works through the recorder's `Unwrap`.
- `GroupMiddleware`: distinct keys → independent breakers; empty key → bypass;
  `KeyByHost` routes by `r.Host`.
- `GroupKey`: the fixed key selects one breaker from the group across requests;
  two mounts with different keys are independent; empty key → bypass; the wrapped
  breaker trips/opens like `Middleware`.
- Invariants: no `init`/singleton/registry in any file; a feature exercised with
  zero options behaves per its documented defaults.

**retry** (`retry_test`):
- Error implementing `RetryAfterError` with hint > backoff → waits ≥ hint
  (mock/tolerance); hint < backoff → backoff wins; wrapped hint found through
  `fmt.Errorf("%w")`; `ctx` cancel still returns promptly during a long hint.

## File inventory

```
resilience/cache/errors.go        + ErrExists
resilience/cache/store.go         Store.Set signature; +SetOptions/SetOption/WithTTL/WithSetNonExist/ApplySetOptions
resilience/cache/memory.go        Set(...SetOption)
resilience/cache/cache.go         Cache[V].Set(...SetOption); GetOrSet internal write
resilience/cache/redis/redis.go   Set(...SetOption) via SetArgs NX
resilience/cache/doc.go           NX/claim example
resilience/circuitbreaker/errors.go        +openError
resilience/circuitbreaker/circuitbreaker.go newConfig; before() remaining; +RetryAfter()
resilience/circuitbreaker/group.go          NEW
resilience/circuitbreaker/http.go           NEW
resilience/circuitbreaker/doc.go            Group + Middleware example
resilience/retry/errors.go        NEW (RetryAfterError)
resilience/retry/retry.go         delay floor in Do loop
```

## Non-goals (this PR)

- No metrics/observability hooks beyond existing `WithOnStateChange`.
- No per-key eviction knobs beyond `WithIdleTTL` + `WithSweepInterval`.
- No `RetryAfter` cap option (ctx/deadline bounds the wait).
- No `httpclient` (this only unblocks it).
- No `web/middleware` import from `resilience/` (see decision 5).

## Post-merge doc sync

Tick the four work items in `docs/packages.md` "Shipped-package work items"
(cache SetNX, circuitbreaker Group + middleware, retry RetryAfter) once merged.
