# auth/magiclink Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `auth/magiclink` — signed, TTL'd, optionally single-use magic links (passwordless login, invites, verify/unsubscribe) over `crypto/token` and the `resilience/cache.Store` seam.

**Architecture:** A thin generic `Manager[T]` wraps the consumer payload in an internal envelope, delegates sign/encrypt/TTL/purpose to `token.Codec[envelope[T]]`, and adds single-use redemption (atomic `SetNX`), tenant-scope binding (fail-closed hook), and URL construction. No HTTP surface. Spec: `docs/superpowers/specs/2026-07-13-auth-magiclink-design.md`.

**Tech Stack:** Go 1.26, `crypto/token`, `crypto/keyset`, `crypto/secret`, `core/clock`, `resilience/cache` (Store seam only), testify.

## Global Constraints

- Work ONLY in the current branch (`dm/wonderful-yonath-6b5184`); never switch.
- All files live in `auth/magiclink/` — single package, no subpackages.
- Black-box tests only: test files declare `package magiclink_test`.
- After changing files run `just fmt ./auth/magiclink` (package-path form — the single-file form trips a betteralign quirk). Run `just lint` at the end of the final task.
- Test command: `just test ./auth/magiclink` (runs with `-race -cover`).
- Runtime errors must be single-line: wrap with `fmt.Errorf("%w: %w", Sentinel, err)`, never `errors.Join` (Join is newline-separated; it is only acceptable for constructor option-error collection, mirroring `crypto/token`).
- Go 1.26 idioms: `for range n` loops, `b.Loop()` in benchmarks (modernize in `just lint` enforces both).
- No Claude/AI attribution in any commit message.
- Struct fields are added to `Manager`/`config` only in the task that first reads them (the `unused` linter flags never-read unexported fields).
- Note: `ctx` params on `Issue`/`Peek`/`Redeem` are unused until Task 3 wires the scope hook. Default golangci linters do not flag unused params — keep the signatures as written; do not underscore them.

---

### Task 1: Errors, options, constructors, stateless core

**Files:**
- Create: `auth/magiclink/errors.go`
- Create: `auth/magiclink/options.go`
- Create: `auth/magiclink/magiclink.go`
- Create: `auth/magiclink/magiclink_test.go`

**Interfaces:**
- Consumes: `token.New[T](key, opts...)`, `token.FromKeyset[T](ks, opts...)`, `token.WithTTL/WithPurpose/WithClock/WithEncrypt`, `token.ErrExpired`, `clock.Clock`, `clock.System()`, `secret.Box`, `keyset.Keyset`.
- Produces (later tasks rely on these exact signatures):
  - `func New[T any](key []byte, purpose string, opts ...Option) (*Manager[T], error)`
  - `func FromKeyset[T any](ks *keyset.Keyset, purpose string, opts ...Option) (*Manager[T], error)`
  - `func (m *Manager[T]) Issue(ctx context.Context, payload T) (string, error)`
  - `func (m *Manager[T]) Peek(ctx context.Context, link string) (T, error)`
  - `func (m *Manager[T]) Redeem(ctx context.Context, link string) (T, error)`
  - `func (m *Manager[T]) verify(ctx context.Context, link string) (envelope[T], error)` (unexported, extended by Tasks 2–3)
  - `type Option func(*config)`; `newConfig(purpose string, opts ...Option) (*config, error)` with fields `clk`, `box`, `ttl`, `errs`
  - Sentinels: `ErrInvalid`, `ErrExpired`, `ErrUsed`, `ErrScopeMismatch`, `ErrStore`

- [ ] **Step 1: Write the failing tests**

Create `auth/magiclink/magiclink_test.go`:

```go
package magiclink_test

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/magiclink"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/crypto/secret"
)

type loginClaims struct {
	UserID string `json:"uid"`
}

var testKey = []byte("0123456789abcdef0123456789abcdef")

func TestNewValidation(t *testing.T) {
	_, err := magiclink.New[loginClaims](testKey, "")
	require.Error(t, err, "empty purpose must be rejected")

	_, err = magiclink.New[loginClaims](testKey, "login", magiclink.WithTTL(0))
	require.Error(t, err, "zero TTL must be rejected")

	_, err = magiclink.New[loginClaims](testKey, "login", magiclink.WithTTL(-time.Minute))
	require.Error(t, err, "negative TTL must be rejected")

	_, err = magiclink.New[loginClaims](testKey, "login", magiclink.WithClock(nil))
	require.Error(t, err, "nil clock must be rejected")

	_, err = magiclink.New[loginClaims](testKey, "login", magiclink.WithEncrypt(nil))
	require.Error(t, err, "nil box must be rejected")

	_, err = magiclink.New[loginClaims](nil, "login")
	require.Error(t, err, "empty key must be rejected")
}

func TestStatelessRoundTrip(t *testing.T) {
	m, err := magiclink.New[loginClaims](testKey, "login")
	require.NoError(t, err)

	link, err := m.Issue(context.Background(), loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	got, err := m.Peek(context.Background(), link)
	require.NoError(t, err)
	assert.Equal(t, "u_1", got.UserID)

	// Stateless Redeem is verify-only and multi-use by design.
	got, err = m.Redeem(context.Background(), link)
	require.NoError(t, err)
	assert.Equal(t, "u_1", got.UserID)

	got, err = m.Redeem(context.Background(), link)
	require.NoError(t, err)
	assert.Equal(t, "u_1", got.UserID)
}

func TestExpired(t *testing.T) {
	clk := clock.NewMock(time.Now())
	m, err := magiclink.New[loginClaims](testKey, "login",
		magiclink.WithTTL(15*time.Minute), magiclink.WithClock(clk))
	require.NoError(t, err)

	link, err := m.Issue(context.Background(), loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	clk.Advance(16 * time.Minute)

	_, err = m.Peek(context.Background(), link)
	assert.ErrorIs(t, err, magiclink.ErrExpired)
	_, err = m.Redeem(context.Background(), link)
	assert.ErrorIs(t, err, magiclink.ErrExpired)
}

func TestInvalidTokens(t *testing.T) {
	m, err := magiclink.New[loginClaims](testKey, "login")
	require.NoError(t, err)

	link, err := m.Issue(context.Background(), loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	// Tampered body: flip a character in the first segment.
	tampered := "A" + link[1:]
	if tampered == link {
		tampered = "B" + link[1:]
	}
	for _, bad := range []string{"", "garbage", "a.b", tampered} {
		_, err = m.Redeem(context.Background(), bad)
		assert.ErrorIs(t, err, magiclink.ErrInvalid, "input %q", bad)
	}
}

func TestCrossPurposeRejected(t *testing.T) {
	login, err := magiclink.New[loginClaims](testKey, "login")
	require.NoError(t, err)
	unsub, err := magiclink.New[loginClaims](testKey, "unsubscribe")
	require.NoError(t, err)

	link, err := login.Issue(context.Background(), loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	_, err = unsub.Redeem(context.Background(), link)
	assert.ErrorIs(t, err, magiclink.ErrInvalid)
}

func TestEncryptedPayloadHidden(t *testing.T) {
	box, err := secret.New(testKey)
	require.NoError(t, err)
	m, err := magiclink.New[loginClaims](testKey, "login", magiclink.WithEncrypt(box))
	require.NoError(t, err)

	link, err := m.Issue(context.Background(), loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	// Without encryption the base64 body decodes to plaintext JSON
	// containing the payload; with WithEncrypt it must not.
	body, _, ok := strings.Cut(link, ".")
	require.True(t, ok)
	raw, err := base64.RawURLEncoding.DecodeString(body)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "u_1")

	got, err := m.Redeem(context.Background(), link)
	require.NoError(t, err)
	assert.Equal(t, "u_1", got.UserID)
}

func TestFromKeysetRotation(t *testing.T) {
	old, err := keyset.New(keyset.WithPrimary(1, testKey))
	require.NoError(t, err)
	mOld, err := magiclink.FromKeyset[loginClaims](old, "login")
	require.NoError(t, err)

	link, err := mOld.Issue(context.Background(), loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	rotated, err := keyset.New(
		keyset.WithPrimary(2, []byte("fedcba9876543210fedcba9876543210")),
		keyset.WithRetired(1, testKey),
	)
	require.NoError(t, err)
	mNew, err := magiclink.FromKeyset[loginClaims](rotated, "login")
	require.NoError(t, err)

	// Link signed under the retired key still verifies after rotation.
	got, err := mNew.Redeem(context.Background(), link)
	require.NoError(t, err)
	assert.Equal(t, "u_1", got.UserID)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `just test ./auth/magiclink`
Expected: FAIL — compile error (`package magiclink` does not exist yet).

- [ ] **Step 3: Write the implementation**

Create `auth/magiclink/errors.go`:

```go
package magiclink

import "errors"

// Sentinel errors returned by Peek and Redeem. All are errors.Is-matchable;
// runtime wrapping keeps the underlying cause inspectable for logs.
var (
	// ErrInvalid is returned when a link is malformed, its signature does not
	// verify, or it was issued for a different purpose.
	ErrInvalid = errors.New("magiclink: invalid link")
	// ErrExpired is returned when the link's TTL has passed.
	ErrExpired = errors.New("magiclink: link expired")
	// ErrUsed is returned when a single-use link has already been redeemed.
	ErrUsed = errors.New("magiclink: link already used")
	// ErrScopeMismatch is returned when a scoped link is verified outside the
	// tenant scope it was issued in.
	ErrScopeMismatch = errors.New("magiclink: scope mismatch")
	// ErrStore is returned when the single-use store fails; redemption fails
	// closed.
	ErrStore = errors.New("magiclink: store operation failed")
)
```

Create `auth/magiclink/options.go`:

```go
package magiclink

import (
	"errors"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/crypto/secret"
	"github.com/dmitrymomot/forge/crypto/token"
)

type config struct {
	clk  clock.Clock
	box  *secret.Box
	errs []error
	ttl  time.Duration
}

// Option configures New/FromKeyset.
type Option func(*config)

func newConfig(purpose string, opts ...Option) (*config, error) {
	c := &config{clk: clock.System(), ttl: 15 * time.Minute}
	for _, o := range opts {
		o(c)
	}
	if purpose == "" {
		c.errs = append(c.errs, errors.New("magiclink: empty purpose"))
	}
	if c.ttl <= 0 {
		c.errs = append(c.errs, errors.New("magiclink: ttl must be positive"))
	}
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	return c, nil
}

// codecOptions translates the resolved config into crypto/token options.
func (c *config) codecOptions(purpose string) []token.Option {
	opts := []token.Option{
		token.WithTTL(c.ttl),
		token.WithPurpose(purpose),
		token.WithClock(c.clk),
	}
	if c.box != nil {
		opts = append(opts, token.WithEncrypt(c.box))
	}
	return opts
}

// WithTTL sets the link lifetime (default 15m). Links always expire: a
// non-positive d is a constructor error.
func WithTTL(d time.Duration) Option { return func(c *config) { c.ttl = d } }

// WithClock sets the time source (default clock.System()). A nil clock is
// rejected.
func WithClock(ck clock.Clock) Option {
	return func(c *config) {
		if ck == nil {
			c.errs = append(c.errs, errors.New("magiclink: nil clock"))
			return
		}
		c.clk = ck
	}
}

// WithEncrypt encrypts the payload (not just signs it), hiding PII from the
// URL. A nil box is rejected.
func WithEncrypt(box *secret.Box) Option {
	return func(c *config) {
		if box == nil {
			c.errs = append(c.errs, errors.New("magiclink: nil box"))
			return
		}
		c.box = box
	}
}
```

Create `auth/magiclink/magiclink.go`:

```go
package magiclink

import (
	"context"
	"errors"
	"fmt"

	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/crypto/token"
)

// envelope wraps the consumer payload inside the signed token.
type envelope[T any] struct {
	Payload T `json:"pld"`
}

// Manager issues and redeems magic-link tokens carrying a payload of type T.
type Manager[T any] struct {
	codec *token.Codec[envelope[T]]
}

// New builds a single-key Manager. Purpose is required: two managers with the
// same key but different purposes never accept each other's links.
func New[T any](key []byte, purpose string, opts ...Option) (*Manager[T], error) {
	c, err := newConfig(purpose, opts...)
	if err != nil {
		return nil, err
	}
	codec, err := token.New[envelope[T]](key, c.codecOptions(purpose)...)
	if err != nil {
		return nil, err
	}
	return &Manager[T]{codec: codec}, nil
}

// FromKeyset builds a rotation-aware Manager (signs under the primary key,
// verifies links signed under any version).
func FromKeyset[T any](ks *keyset.Keyset, purpose string, opts ...Option) (*Manager[T], error) {
	c, err := newConfig(purpose, opts...)
	if err != nil {
		return nil, err
	}
	codec, err := token.FromKeyset[envelope[T]](ks, c.codecOptions(purpose)...)
	if err != nil {
		return nil, err
	}
	return &Manager[T]{codec: codec}, nil
}

// Issue creates a signed link token for payload.
func (m *Manager[T]) Issue(ctx context.Context, payload T) (string, error) {
	return m.codec.Issue(envelope[T]{Payload: payload})
}

// Peek verifies a link without consuming it. Serve it on GET so email
// scanners that prefetch links cannot burn them.
func (m *Manager[T]) Peek(ctx context.Context, link string) (T, error) {
	var zero T
	env, err := m.verify(ctx, link)
	if err != nil {
		return zero, err
	}
	return env.Payload, nil
}

// Redeem verifies a link and, when a store is configured, atomically consumes
// it. Without a store it is verify-only and multi-use.
func (m *Manager[T]) Redeem(ctx context.Context, link string) (T, error) {
	var zero T
	env, err := m.verify(ctx, link)
	if err != nil {
		return zero, err
	}
	return env.Payload, nil
}

// verify parses the token and maps crypto/token errors to package sentinels.
// Signature verification runs before any store I/O.
func (m *Manager[T]) verify(ctx context.Context, link string) (envelope[T], error) {
	env, err := m.codec.Parse(link)
	if err != nil {
		return envelope[T]{}, mapTokenErr(err)
	}
	return env, nil
}

func mapTokenErr(err error) error {
	if errors.Is(err, token.ErrExpired) {
		return fmt.Errorf("%w: %w", ErrExpired, err)
	}
	return fmt.Errorf("%w: %w", ErrInvalid, err)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `just test ./auth/magiclink`
Expected: PASS (all 7 tests).

- [ ] **Step 5: Format and commit**

```bash
just fmt ./auth/magiclink
git add auth/magiclink/
git commit -m "feat(magiclink): stateless issue/peek/redeem core over crypto/token"
```

---

### Task 2: Single-use redemption (WithStore)

**Files:**
- Modify: `auth/magiclink/options.go` (add `store` field + `WithStore`)
- Modify: `auth/magiclink/magiclink.go` (Manager fields, storeKey, Peek/Redeem store logic)
- Modify: `auth/magiclink/magiclink_test.go` (append tests)

**Interfaces:**
- Consumes: Task 1's `Manager[T]`, `verify`, sentinels; `cache.Store`, `cache.WithTTL`, `cache.WithSetNonExist`, `cache.ErrExists`, `cache.NewMemoryStore`.
- Produces:
  - `WithStore(s cache.Store) Option`
  - `func (m *Manager[T]) storeKey(link string) string` (unexported) — key format `magiclink:<purpose>:<base64url(sha256(link))>`
  - `Manager` gains fields `store cache.Store`, `purpose string`, `ttl time.Duration` (set in both constructors).

- [ ] **Step 1: Write the failing tests**

Append to `auth/magiclink/magiclink_test.go` (add imports `errors`, `sync`, `sync/atomic`, and `github.com/dmitrymomot/forge/resilience/cache`):

```go
func newMemStore(t *testing.T) cache.Store {
	t.Helper()
	s := cache.NewMemoryStore()
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSingleUseRedeem(t *testing.T) {
	m, err := magiclink.New[loginClaims](testKey, "login", magiclink.WithStore(newMemStore(t)))
	require.NoError(t, err)

	link, err := m.Issue(context.Background(), loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	got, err := m.Redeem(context.Background(), link)
	require.NoError(t, err)
	assert.Equal(t, "u_1", got.UserID)

	_, err = m.Redeem(context.Background(), link)
	assert.ErrorIs(t, err, magiclink.ErrUsed)
}

func TestPeekDoesNotConsume(t *testing.T) {
	m, err := magiclink.New[loginClaims](testKey, "login", magiclink.WithStore(newMemStore(t)))
	require.NoError(t, err)

	link, err := m.Issue(context.Background(), loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	_, err = m.Peek(context.Background(), link)
	require.NoError(t, err)
	_, err = m.Peek(context.Background(), link)
	require.NoError(t, err, "Peek must be repeatable")

	_, err = m.Redeem(context.Background(), link)
	require.NoError(t, err, "Redeem must still succeed after Peek")

	_, err = m.Peek(context.Background(), link)
	assert.ErrorIs(t, err, magiclink.ErrUsed, "Peek after Redeem reports used")
}

func TestConcurrentRedeemSingleWinner(t *testing.T) {
	m, err := magiclink.New[loginClaims](testKey, "login", magiclink.WithStore(newMemStore(t)))
	require.NoError(t, err)

	link, err := m.Issue(context.Background(), loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	const n = 32
	var wins, used atomic.Int32
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := m.Redeem(context.Background(), link)
			switch {
			case err == nil:
				wins.Add(1)
			case errors.Is(err, magiclink.ErrUsed):
				used.Add(1)
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, int32(1), wins.Load(), "exactly one redeem wins")
	assert.Equal(t, int32(n-1), used.Load(), "all others see ErrUsed")
}

type failingStore struct{ err error }

func (f failingStore) Get(context.Context, string) ([]byte, error) { return nil, f.err }
func (f failingStore) Set(context.Context, string, []byte, ...cache.SetOption) error {
	return f.err
}
func (f failingStore) Delete(context.Context, string) error      { return f.err }
func (f failingStore) Has(context.Context, string) (bool, error) { return false, f.err }
func (f failingStore) DeletePrefix(context.Context, string) error {
	return f.err
}
func (f failingStore) Close() error { return nil }

func TestStoreFailureFailsClosed(t *testing.T) {
	m, err := magiclink.New[loginClaims](testKey, "login",
		magiclink.WithStore(failingStore{err: errors.New("boom")}))
	require.NoError(t, err)

	link, err := m.Issue(context.Background(), loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	_, err = m.Redeem(context.Background(), link)
	assert.ErrorIs(t, err, magiclink.ErrStore)
	_, err = m.Peek(context.Background(), link)
	assert.ErrorIs(t, err, magiclink.ErrStore)
}

func TestJunkRejectedBeforeStore(t *testing.T) {
	// Signature check precedes store I/O: junk must yield ErrInvalid even
	// when every store call would fail.
	m, err := magiclink.New[loginClaims](testKey, "login",
		magiclink.WithStore(failingStore{err: errors.New("boom")}))
	require.NoError(t, err)

	_, err = m.Redeem(context.Background(), "garbage.token")
	assert.ErrorIs(t, err, magiclink.ErrInvalid)
}

func TestWithStoreNilRejected(t *testing.T) {
	_, err := magiclink.New[loginClaims](testKey, "login", magiclink.WithStore(nil))
	require.Error(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `just test ./auth/magiclink`
Expected: FAIL — compile error: `undefined: magiclink.WithStore`.

- [ ] **Step 3: Write the implementation**

In `auth/magiclink/options.go`: add `store cache.Store` to `config`, add the import `"github.com/dmitrymomot/forge/resilience/cache"`, and append:

```go
// WithStore enables single-use redemption: Redeem atomically claims each link
// and returns ErrUsed on replay. A nil store is rejected. The bundled LRU
// memory store can evict live keys; production single-use needs cache/redis
// or another durable Store.
func WithStore(s cache.Store) Option {
	return func(c *config) {
		if s == nil {
			c.errs = append(c.errs, errors.New("magiclink: nil store"))
			return
		}
		c.store = s
	}
}
```

In `auth/magiclink/magiclink.go`: add imports `"crypto/sha256"`, `"encoding/base64"`, `"time"`, `"github.com/dmitrymomot/forge/resilience/cache"`. Extend the Manager struct and both constructors:

```go
// Manager issues and redeems magic-link tokens carrying a payload of type T.
type Manager[T any] struct {
	codec   *token.Codec[envelope[T]]
	store   cache.Store
	purpose string
	ttl     time.Duration
}
```

In both `New` and `FromKeyset`, replace the return line with:

```go
	return &Manager[T]{codec: codec, store: c.store, purpose: purpose, ttl: c.ttl}, nil
```

Replace `Peek` and `Redeem` bodies and add `storeKey`:

```go
// Peek verifies a link without consuming it. Serve it on GET so email
// scanners that prefetch links cannot burn them. With a store configured it
// reports ErrUsed for already-redeemed links (best-effort; the consuming
// Redeem is authoritative).
func (m *Manager[T]) Peek(ctx context.Context, link string) (T, error) {
	var zero T
	env, err := m.verify(ctx, link)
	if err != nil {
		return zero, err
	}
	if m.store != nil {
		used, err := m.store.Has(ctx, m.storeKey(link))
		if err != nil {
			return zero, fmt.Errorf("%w: %w", ErrStore, err)
		}
		if used {
			return zero, ErrUsed
		}
	}
	return env.Payload, nil
}

// Redeem verifies a link and, when a store is configured, atomically consumes
// it: the first call wins, replays return ErrUsed, store failures fail closed
// with ErrStore. Without a store it is verify-only and multi-use.
func (m *Manager[T]) Redeem(ctx context.Context, link string) (T, error) {
	var zero T
	env, err := m.verify(ctx, link)
	if err != nil {
		return zero, err
	}
	if m.store != nil {
		err := m.store.Set(ctx, m.storeKey(link), []byte{1},
			cache.WithTTL(m.ttl), cache.WithSetNonExist())
		switch {
		case errors.Is(err, cache.ErrExists):
			return zero, ErrUsed
		case err != nil:
			return zero, fmt.Errorf("%w: %w", ErrStore, err)
		}
	}
	return env.Payload, nil
}

// storeKey derives the single-use claim key. The token hash is globally
// unique (each token carries a random nonce), so scope is not needed here.
func (m *Manager[T]) storeKey(link string) string {
	sum := sha256.Sum256([]byte(link))
	return "magiclink:" + m.purpose + ":" + base64.RawURLEncoding.EncodeToString(sum[:])
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `just test ./auth/magiclink`
Expected: PASS, race-clean (the concurrent test runs under `-race`).

- [ ] **Step 5: Format and commit**

```bash
just fmt ./auth/magiclink
git add auth/magiclink/
git commit -m "feat(magiclink): single-use redemption via cache.Store SetNX claim"
```

---

### Task 3: Tenant scope binding (WithScope)

**Files:**
- Modify: `auth/magiclink/options.go` (add `scopeFn` + `WithScope`)
- Modify: `auth/magiclink/magiclink.go` (envelope.Scope, resolveScope, Issue stamping, verify matching)
- Modify: `auth/magiclink/magiclink_test.go` (append tests)

**Interfaces:**
- Consumes: Task 1's `verify`, `envelope`; Task 2's Manager shape.
- Produces:
  - `WithScope(fn func(context.Context) (string, error)) Option`
  - `envelope` gains a `Scope string` field with json tag `scp,omitempty`
  - `func (m *Manager[T]) resolveScope(ctx context.Context) (string, error)` (unexported)
  - `Manager` gains field `scopeFn func(context.Context) (string, error)`.

- [ ] **Step 1: Write the failing tests**

Append to `auth/magiclink/magiclink_test.go`:

```go
type tenantKey struct{}

func tenantCtx(scope string) context.Context {
	return context.WithValue(context.Background(), tenantKey{}, scope)
}

func tenantScope(ctx context.Context) (string, error) {
	v, _ := ctx.Value(tenantKey{}).(string)
	return v, nil
}

func TestScopeMatrix(t *testing.T) {
	m, err := magiclink.New[loginClaims](testKey, "invite", magiclink.WithScope(tenantScope))
	require.NoError(t, err)

	// Global link (issued without tenant in ctx): valid everywhere.
	global, err := m.Issue(context.Background(), loginClaims{UserID: "u_1"})
	require.NoError(t, err)
	_, err = m.Redeem(tenantCtx("acme"), global)
	require.NoError(t, err, "global link redeems inside a tenant")
	_, err = m.Redeem(context.Background(), global)
	require.NoError(t, err, "global link redeems globally")

	// Scoped link: valid only in the exact tenant it was issued in.
	scoped, err := m.Issue(tenantCtx("acme"), loginClaims{UserID: "u_1"})
	require.NoError(t, err)
	_, err = m.Peek(tenantCtx("acme"), scoped)
	require.NoError(t, err, "Peek applies the same scope rule")
	_, err = m.Redeem(tenantCtx("acme"), scoped)
	require.NoError(t, err)
	_, err = m.Redeem(tenantCtx("globex"), scoped)
	assert.ErrorIs(t, err, magiclink.ErrScopeMismatch)
	_, err = m.Redeem(context.Background(), scoped)
	assert.ErrorIs(t, err, magiclink.ErrScopeMismatch, "scoped link is not global")
}

func TestScopeHookErrorPropagates(t *testing.T) {
	hookErr := errors.New("no tenant in ctx")
	m, err := magiclink.New[loginClaims](testKey, "invite",
		magiclink.WithScope(func(ctx context.Context) (string, error) {
			if v, ok := ctx.Value(tenantKey{}).(string); ok {
				return v, nil
			}
			return "", hookErr
		}))
	require.NoError(t, err)

	_, err = m.Issue(context.Background(), loginClaims{UserID: "u_1"})
	assert.ErrorIs(t, err, hookErr, "hook error aborts issuance")

	link, err := m.Issue(tenantCtx("acme"), loginClaims{UserID: "u_1"})
	require.NoError(t, err)
	_, err = m.Redeem(context.Background(), link)
	assert.ErrorIs(t, err, hookErr, "hook error aborts redemption")
}

func TestScopedTokenOnUnscopedManagerRejected(t *testing.T) {
	scoped, err := magiclink.New[loginClaims](testKey, "invite", magiclink.WithScope(tenantScope))
	require.NoError(t, err)
	plain, err := magiclink.New[loginClaims](testKey, "invite")
	require.NoError(t, err)

	link, err := scoped.Issue(tenantCtx("acme"), loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	// Config drift fails closed: no hook means ctx scope is always "".
	_, err = plain.Redeem(context.Background(), link)
	assert.ErrorIs(t, err, magiclink.ErrScopeMismatch)
}

func TestWithScopeNilRejected(t *testing.T) {
	_, err := magiclink.New[loginClaims](testKey, "invite", magiclink.WithScope(nil))
	require.Error(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `just test ./auth/magiclink`
Expected: FAIL — compile error: `undefined: magiclink.WithScope`.

- [ ] **Step 3: Write the implementation**

In `auth/magiclink/options.go`: add `scopeFn func(context.Context) (string, error)` to `config`, add the `"context"` import, and append:

```go
// WithScope binds links to a tenant scope resolved from ctx (forge-wide
// multi-tenancy hook). Issue stamps the resolved scope into the token;
// Peek/Redeem recompute it and fail closed on mismatch. An empty resolved
// scope means a global link, valid in any tenant context; a hook that wants
// to forbid global issuance returns an error when ctx lacks a tenant. A nil
// hook is rejected.
func WithScope(fn func(context.Context) (string, error)) Option {
	return func(c *config) {
		if fn == nil {
			c.errs = append(c.errs, errors.New("magiclink: nil scope hook"))
			return
		}
		c.scopeFn = fn
	}
}
```

In `auth/magiclink/magiclink.go`:

Extend envelope:

```go
// envelope wraps the consumer payload inside the signed token.
type envelope[T any] struct {
	Payload T      `json:"pld"`
	Scope   string `json:"scp,omitempty"`
}
```

Add `scopeFn func(context.Context) (string, error)` to the Manager struct, and in both constructors extend the return to include `scopeFn: c.scopeFn`:

```go
	return &Manager[T]{codec: codec, store: c.store, scopeFn: c.scopeFn, purpose: purpose, ttl: c.ttl}, nil
```

Replace `Issue` and `verify`, add `resolveScope`:

```go
// Issue creates a signed link token for payload. With a scope hook configured
// the resolved scope is stamped into the token; a hook error aborts issuance.
func (m *Manager[T]) Issue(ctx context.Context, payload T) (string, error) {
	scope, err := m.resolveScope(ctx)
	if err != nil {
		return "", err
	}
	return m.codec.Issue(envelope[T]{Payload: payload, Scope: scope})
}

// verify parses the token, maps crypto/token errors to package sentinels, and
// enforces scope. Signature verification runs before any store I/O.
func (m *Manager[T]) verify(ctx context.Context, link string) (envelope[T], error) {
	env, err := m.codec.Parse(link)
	if err != nil {
		return envelope[T]{}, mapTokenErr(err)
	}
	scope, err := m.resolveScope(ctx)
	if err != nil {
		return envelope[T]{}, err
	}
	if env.Scope != "" && env.Scope != scope {
		return envelope[T]{}, ErrScopeMismatch
	}
	return env, nil
}

func (m *Manager[T]) resolveScope(ctx context.Context) (string, error) {
	if m.scopeFn == nil {
		return "", nil
	}
	return m.scopeFn(ctx)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `just test ./auth/magiclink`
Expected: PASS.

- [ ] **Step 5: Format and commit**

```bash
just fmt ./auth/magiclink
git add auth/magiclink/
git commit -m "feat(magiclink): fail-closed tenant scope binding via WithScope hook"
```

---

### Task 4: URL construction (IssueURL, WithBaseURL, WithParam)

**Files:**
- Modify: `auth/magiclink/options.go` (add `baseURL`/`param` + options)
- Modify: `auth/magiclink/magiclink.go` (Manager fields, IssueURL)
- Modify: `auth/magiclink/magiclink_test.go` (append tests)

**Interfaces:**
- Consumes: Task 3's `Issue`.
- Produces:
  - `WithBaseURL(u string) Option` (validated at construction), `WithParam(name string) Option` (default `"token"`)
  - `func (m *Manager[T]) IssueURL(ctx context.Context, base string, payload T) (string, error)` — per-call base wins; empty base falls back to WithBaseURL; both empty is an error.

- [ ] **Step 1: Write the failing tests**

Append to `auth/magiclink/magiclink_test.go` (add import `net/url`):

```go
func TestIssueURLDefaultBase(t *testing.T) {
	m, err := magiclink.New[loginClaims](testKey, "login",
		magiclink.WithBaseURL("https://app.example.com/auth/verify"))
	require.NoError(t, err)

	u, err := m.IssueURL(context.Background(), "", loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	parsed, err := url.Parse(u)
	require.NoError(t, err)
	assert.Equal(t, "https", parsed.Scheme)
	assert.Equal(t, "app.example.com", parsed.Host)
	assert.Equal(t, "/auth/verify", parsed.Path)

	link := parsed.Query().Get("token")
	require.NotEmpty(t, link)
	got, err := m.Redeem(context.Background(), link)
	require.NoError(t, err)
	assert.Equal(t, "u_1", got.UserID)
}

func TestIssueURLPerCallBaseWins(t *testing.T) {
	m, err := magiclink.New[loginClaims](testKey, "invite",
		magiclink.WithBaseURL("https://app.example.com/join"))
	require.NoError(t, err)

	u, err := m.IssueURL(context.Background(), "https://acme.example.com/join",
		loginClaims{UserID: "u_1"})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(u, "https://acme.example.com/join?token="), u)
}

func TestIssueURLNoBaseErrors(t *testing.T) {
	m, err := magiclink.New[loginClaims](testKey, "login")
	require.NoError(t, err)

	_, err = m.IssueURL(context.Background(), "", loginClaims{UserID: "u_1"})
	require.Error(t, err)
}

func TestIssueURLPreservesExistingQuery(t *testing.T) {
	m, err := magiclink.New[loginClaims](testKey, "login")
	require.NoError(t, err)

	u, err := m.IssueURL(context.Background(),
		"https://app.example.com/verify?lang=de", loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	parsed, err := url.Parse(u)
	require.NoError(t, err)
	assert.Equal(t, "de", parsed.Query().Get("lang"))
	assert.NotEmpty(t, parsed.Query().Get("token"))
}

func TestWithParamRename(t *testing.T) {
	m, err := magiclink.New[loginClaims](testKey, "login", magiclink.WithParam("t"))
	require.NoError(t, err)

	u, err := m.IssueURL(context.Background(), "https://app.example.com/verify",
		loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	parsed, err := url.Parse(u)
	require.NoError(t, err)
	assert.NotEmpty(t, parsed.Query().Get("t"))
	assert.Empty(t, parsed.Query().Get("token"))
}

func TestURLOptionValidation(t *testing.T) {
	_, err := magiclink.New[loginClaims](testKey, "login", magiclink.WithParam(""))
	require.Error(t, err, "empty param name must be rejected")

	_, err = magiclink.New[loginClaims](testKey, "login", magiclink.WithBaseURL(""))
	require.Error(t, err, "empty base URL must be rejected")

	_, err = magiclink.New[loginClaims](testKey, "login", magiclink.WithBaseURL("://bad"))
	require.Error(t, err, "unparsable base URL must be rejected")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `just test ./auth/magiclink`
Expected: FAIL — compile error: `undefined: magiclink.WithBaseURL`.

- [ ] **Step 3: Write the implementation**

In `auth/magiclink/options.go`: add `baseURL string` and `param string` to `config`, add the `"fmt"` and `"net/url"` imports, initialize `param: "token"` in `newConfig`'s initial struct literal:

```go
	c := &config{clk: clock.System(), ttl: 15 * time.Minute, param: "token"}
```

Append options:

```go
// WithBaseURL sets the default base used by IssueURL when its base argument
// is empty — the single-domain convenience. Must be a non-empty, parsable
// URL.
func WithBaseURL(u string) Option {
	return func(c *config) {
		if u == "" {
			c.errs = append(c.errs, errors.New("magiclink: empty base URL"))
			return
		}
		if _, err := url.Parse(u); err != nil {
			c.errs = append(c.errs, fmt.Errorf("magiclink: invalid base URL: %w", err))
			return
		}
		c.baseURL = u
	}
}

// WithParam sets the query parameter name IssueURL appends the token under
// (default "token"). Empty is rejected.
func WithParam(name string) Option {
	return func(c *config) {
		if name == "" {
			c.errs = append(c.errs, errors.New("magiclink: empty param name"))
			return
		}
		c.param = name
	}
}
```

In `auth/magiclink/magiclink.go`: add `"net/url"` import; add `baseURL string` and `param string` fields to Manager; extend both constructors' return:

```go
	return &Manager[T]{
		codec:   codec,
		store:   c.store,
		scopeFn: c.scopeFn,
		purpose: purpose,
		baseURL: c.baseURL,
		param:   c.param,
		ttl:     c.ttl,
	}, nil
```

Add `IssueURL`:

```go
// IssueURL issues a link token and appends it as a query parameter to base
// (multi-tenant/white-label callers pass the tenant's base per call). An
// empty base falls back to WithBaseURL; both empty is an error. Existing
// query parameters on the base are preserved.
func (m *Manager[T]) IssueURL(ctx context.Context, base string, payload T) (string, error) {
	if base == "" {
		base = m.baseURL
	}
	if base == "" {
		return "", errors.New("magiclink: no base URL")
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("magiclink: invalid base URL: %w", err)
	}
	link, err := m.Issue(ctx, payload)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set(m.param, link)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `just test ./auth/magiclink`
Expected: PASS.

- [ ] **Step 5: Format and commit**

```bash
just fmt ./auth/magiclink
git add auth/magiclink/
git commit -m "feat(magiclink): IssueURL with per-call base for white-label domains"
```

---

### Task 5: doc.go, benchmarks, roadmap entry removal, final verification

**Files:**
- Create: `auth/magiclink/doc.go`
- Create: `auth/magiclink/bench_test.go`
- Modify: `docs/packages.md` (delete the auth/magiclink roadmap entry)

**Interfaces:**
- Consumes: the complete public API from Tasks 1–4 (`New`, `FromKeyset`, all options, `Issue`, `IssueURL`, `Peek`, `Redeem`, all sentinels) and test fixtures `loginClaims`/`testKey` from `magiclink_test.go`.
- Produces: nothing new — documentation, benchmarks, and roadmap hygiene.

- [ ] **Step 1: Write doc.go**

Create `auth/magiclink/doc.go`:

```go
// Package magiclink issues and redeems signed, TTL'd, optionally single-use
// links: passwordless login, team invites, email verification, and
// unsubscribe flows. It builds on crypto/token for the token format and adds
// single-use redemption over the resilience/cache Store seam, tenant-scope
// binding, and URL construction. It does not send email — delivery is the
// caller's channel.
//
// Stateless by default: without WithStore a link verifies until its TTL
// expires and may be redeemed repeatedly (fine for unsubscribe and verify
// flows where replay is harmless). WithStore makes Redeem atomically
// single-use. The bundled LRU memory store can evict live keys under
// pressure; production single-use needs cache/redis or another durable
// Store.
//
// Email scanners (Outlook SafeLinks and friends) prefetch links before the
// user clicks. Serve GET with Peek — it verifies without consuming — and
// consume with Redeem on an explicit POST:
//
//	type LoginClaims struct {
//		UserID string `json:"uid"`
//	}
//
//	links, err := magiclink.New[LoginClaims](key, "login",
//		magiclink.WithTTL(15*time.Minute),
//		magiclink.WithStore(store), // single-use; omit for stateless links
//		magiclink.WithBaseURL("https://app.example.com/auth/verify"),
//	)
//
//	// Request: build the link and deliver it yourself.
//	url, err := links.IssueURL(ctx, "", LoginClaims{UserID: "u_1"})
//
//	// GET /auth/verify?token=... — show a confirm button, do not consume.
//	claims, err := links.Peek(r.Context(), r.URL.Query().Get("token"))
//
//	// POST /auth/verify — consume; a second redeem returns ErrUsed.
//	claims, err = links.Redeem(r.Context(), r.FormValue("token"))
//
// Errors are matched with errors.Is: ErrInvalid (malformed, bad signature,
// wrong purpose), ErrExpired, ErrUsed, ErrScopeMismatch, and ErrStore (store
// failure; redemption fails closed).
//
// Multi-tenant apps bind links to a tenant with WithScope: Issue stamps the
// scope resolved from ctx into the token, Peek/Redeem recompute it and fail
// closed on mismatch. A link issued with an empty scope is global and
// redeems in any tenant context — a hook that wants to forbid global
// issuance returns an error when ctx lacks a tenant. White-label callers
// pass the tenant's domain as IssueURL's base argument:
//
//	invites, err := magiclink.New[InviteClaims](key, "invite",
//		magiclink.WithStore(store),
//		magiclink.WithScope(func(ctx context.Context) (string, error) {
//			return tenant.FromContext(ctx), nil // "" = global link
//		}),
//	)
//	url, err := invites.IssueURL(ctx, "https://acme.example.com/join",
//		InviteClaims{Email: "new@hire.com", Role: "admin"})
package magiclink
```

- [ ] **Step 2: Write benchmarks**

Create `auth/magiclink/bench_test.go`:

```go
package magiclink_test

import (
	"context"
	"testing"

	"github.com/dmitrymomot/forge/auth/magiclink"
	"github.com/dmitrymomot/forge/resilience/cache"
)

func BenchmarkIssue(b *testing.B) {
	m, err := magiclink.New[loginClaims](testKey, "bench")
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := m.Issue(ctx, loginClaims{UserID: "u_1"}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPeek(b *testing.B) {
	m, err := magiclink.New[loginClaims](testKey, "bench")
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	link, err := m.Issue(ctx, loginClaims{UserID: "u_1"})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := m.Peek(ctx, link); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRedeemSingleUse(b *testing.B) {
	store := cache.NewMemoryStore()
	defer func() { _ = store.Close() }()
	m, err := magiclink.New[loginClaims](testKey, "bench", magiclink.WithStore(store))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		link, err := m.Issue(ctx, loginClaims{UserID: "u_1"})
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if _, err := m.Redeem(ctx, link); err != nil {
			b.Fatal(err)
		}
	}
}
```

- [ ] **Step 3: Run benchmarks**

Run: `just bench ./auth/magiclink`
Expected: three benchmarks complete with ns/op and allocs/op reported. Record the numbers — they go in the PR description (repo rule: every package ships benchmarks with a post-benchmark optimization pass; before/after numbers in the PR). If allocs/op looks pathological (hundreds per op), investigate before committing; JSON+HMAC costs some allocation, so single-digit-to-low-tens is acceptable here.

- [ ] **Step 4: Delete the roadmap entry**

The catalog lists only unbuilt packages (design.md: "the moment a package ships, delete its entry"). In `docs/packages.md`, delete the **auth/magiclink** block — the heading, body, deps line, and its trailing `---` separator (currently around lines 679–688):

```markdown
**auth/magiclink**

Signed, TTL'd, single-use links over `crypto/token`: passwordless login,
team invites (role/tenant claims as a documented example), verify and
unsubscribe links. Stateless by default; `WithStore` for single-use
redemption. Does not send email.

Deps: `crypto/token`, `resilience/cache`.

---
```

Leave the unrelated mention of `magiclink` near line 87 (another package's "Not `magiclink`..." contrast line) untouched.

- [ ] **Step 5: Full verification**

```bash
just fmt ./auth/magiclink
just lint
just test ./auth/magiclink
```

Expected: fmt clean, lint reports no issues for `auth/magiclink` (pre-existing issues elsewhere in the repo, if any, are out of scope), all tests pass race-clean.

- [ ] **Step 6: Commit**

```bash
git add auth/magiclink/ docs/packages.md
git commit -m "docs(magiclink): package docs, benchmarks; drop shipped roadmap entry"
```
