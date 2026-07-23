# auth/session core — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `auth/session` core package — a durable per-visitor bucket with a namespaced payload, sliding + absolute expiry, step-up elevation, an in-memory store, and one automatic commit per request.

**Architecture:** Two entry points. `session.New` returns a `Manager` that owns lifecycle and storage and knows nothing about HTTP. `session.Middleware` owns the request layer: extract → load → validate → policies → context → commit-at-first-byte. The core defines the `Store`, `Transport`, and `Policy` interfaces; every implementation except the in-memory store ships in a sibling package that imports `session` and uses only its exported API.

**Tech Stack:** Go 1.26, stdlib `testing` (no testify), `core/id`, `core/ctxkey`, `core/clock`, `web/middleware`, `web/problem`, `auth/guard`, `auth/access`, `ops/logger`, `ops/supervisor`.

**Source spec:** `docs/superpowers/specs/2026-07-22-auth-session-design.md` (commit `ccec82e`).

**Scope of this plan:** PR 1 of 7 — the core package only. Transports, policies, and the pg/mongo/kv/cookie stores each get their own plan, written after this one lands. They are the proof that the seams are sufficient; do not add them here.

## Global Constraints

- Module path `github.com/dmitrymomot/forge`; Go 1.26.
- Package layout per `docs/design.md`: `doc.go` (runnable example) · `config.go` · `options.go` (`type Option func(*config)`, **never** builders) · `errors.go` (`errors.Is`-matchable single-line sentinels) · impl.
- Black-box tests only: every test file is `package session_test`. The single exception is `namespace_internal_test.go` for the raw-payload passthrough primitive.
- Default optional `*slog.Logger` is `logger.NewNope()`, never `slog.Default()`.
- No reflection, no service containers, no magic. Values via parameters, not context (context carries request-scoped reads only).
- `bench_test.go` is required, with a post-benchmark optimization pass and before/after numbers in the PR.
- Run `just fmt ./auth/session/...` after file changes and `just lint` when the task is done. `betteralign -apply` exits 3 when it rewrites files — `just fmt` "failing" that way means it did its job.
- Never hand-wrap prose in doc comments or commit bodies.
- No Claude attribution in any commit message.
- Work only on the current branch `dm/auth-session-requirements-c2d06a`. Never switch or create branches.

---

## File Structure

| File | Responsibility |
|---|---|
| `auth/session/errors.go` | Sentinels: `ErrNotFound`, `ErrExpired`, `ErrUnsupported`, `ErrNoEmbed`, `ErrNoSession`, `ErrNoStore`, config errors |
| `auth/session/config.go` | `Config`, `DefaultConfig`, `Validate` |
| `auth/session/options.go` | `Option` + `WithStore`, `WithIdle`, `WithMaxTTL`, `WithRememberIdle`, `WithRememberMaxTTL`, `WithTouch`, `WithClock`, `WithLogger` |
| `auth/session/store.go` | `Record`, `Store`, `Toucher`, `UserIndex`, `Expirer`, `Digest` |
| `auth/session/session.go` | `Session` + accessors + raw payload / dirty tracking |
| `auth/session/namespace.go` | `Namespace[T]`, the name registry, lazy decode, encode-on-commit |
| `auth/session/memory.go` | `MemoryStore` — full capability set, for tests and dev |
| `auth/session/manager.go` | `Manager`: `New`, `Load`, `Save`, `Authenticate`, `Rotate`, `Destroy`, `Elevate`, `Rebind`, expiry |
| `auth/session/devices.go` | `ListByUser`, `Revoke`, `LogoutOthers`, `DeleteByUser`, `DeleteExpired` |
| `auth/session/context.go` | `Info`, the ctx keys, `FromContext`, `For`, `MustFor`, `LogExtractor` |
| `auth/session/transport.go` | `Transport` interface |
| `auth/session/policy.go` | `Policy`, `Deny`, `Revoke`, their error types |
| `auth/session/writer.go` | `commitWriter` — commit at first byte, `Hijack`, `FlushError` |
| `auth/session/middleware.go` | `Middleware` + `MiddlewareOption`s |
| `auth/session/guard.go` | `Extractor`, `Verifier`, `WithIdentity` |
| `auth/session/elevation.go` | `RequireElevation` → `access.Decider` |
| `auth/session/reaper.go` | `Reaper` → `supervisor.Service` |
| `auth/session/storetest/storetest.go` | Conformance suite every store driver runs |
| `auth/session/doc.go` | Package doc + runnable example |
| `auth/session/bench_test.go` | Benchmarks |

---

### Task 1: Errors, config, options, store seam

**Files:**
- Create: `auth/session/errors.go`, `auth/session/config.go`, `auth/session/options.go`, `auth/session/store.go`
- Test: `auth/session/config_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `Config{Idle, MaxTTL, RememberIdle, RememberMax, Touch time.Duration}`, `DefaultConfig() Config`, `(Config) Validate() error`; `Option func(*options)`; `Record` struct; `Store`, `Toucher`, `UserIndex`, `Expirer` interfaces; `Digest(token string) string`; sentinels `ErrNotFound`, `ErrExpired`, `ErrUnsupported`, `ErrNoEmbed`, `ErrNoSession`, `ErrNoStore`, `ErrBadIdle`, `ErrBadMaxTTL`, `ErrBadTouch`, `ErrTouchUnsupported`.

- [ ] **Step 1: Write the failing test**

`auth/session/config_test.go`:

```go
package session_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/session"
)

func TestDefaultConfigIsValid(t *testing.T) {
	if err := session.DefaultConfig().Validate(); err != nil {
		t.Fatalf("DefaultConfig().Validate() = %v, want nil", err)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*session.Config)
		want error
	}{
		{"zero idle", func(c *session.Config) { c.Idle = 0 }, session.ErrBadIdle},
		{"negative idle", func(c *session.Config) { c.Idle = -time.Second }, session.ErrBadIdle},
		{"negative maxttl", func(c *session.Config) { c.MaxTTL = -time.Second }, session.ErrBadMaxTTL},
		{"maxttl below idle", func(c *session.Config) { c.Idle = 48 * time.Hour; c.MaxTTL = time.Hour }, session.ErrBadMaxTTL},
		{"zero remember idle", func(c *session.Config) { c.RememberIdle = 0 }, session.ErrBadIdle},
		{"negative touch", func(c *session.Config) { c.Touch = -time.Second }, session.ErrBadTouch},
		{"touch above idle", func(c *session.Config) { c.Touch = 48 * time.Hour }, session.ErrBadTouch},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := session.DefaultConfig()
			tc.mut(&cfg)
			if err := cfg.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate() = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestConfigZeroMaxTTLMeansNoCap(t *testing.T) {
	cfg := session.DefaultConfig()
	cfg.MaxTTL = 0
	cfg.RememberMax = 0
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() with zero caps = %v, want nil (0 means no absolute cap)", err)
	}
}

func TestDigestIsStableAndNotTheToken(t *testing.T) {
	const token = "s_abc123"
	d1, d2 := session.Digest(token), session.Digest(token)
	if d1 != d2 {
		t.Fatalf("Digest not stable: %q != %q", d1, d2)
	}
	if d1 == token {
		t.Fatal("Digest returned the raw token; the raw token must never be persisted")
	}
	if session.Digest("other") == d1 {
		t.Fatal("Digest collided across distinct tokens")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./auth/session/...`
Expected: FAIL — `no required module provides package github.com/dmitrymomot/forge/auth/session`.

- [ ] **Step 3: Write minimal implementation**

`auth/session/errors.go`:

```go
package session

import "errors"

var (
	// ErrNotFound means no record exists for the presented token.
	ErrNotFound = errors.New("session: not found")
	// ErrExpired means the record exists but its deadline has passed.
	ErrExpired = errors.New("session: expired")
	// ErrUnsupported means the configured Store lacks the capability this call needs.
	ErrUnsupported = errors.New("session: store capability unsupported")
	// ErrNoEmbed means a Transport can read a token but cannot write one back.
	ErrNoEmbed = errors.New("session: transport cannot embed")
	// ErrNoSession means no session is present in the context.
	ErrNoSession = errors.New("session: no session in context")
	// ErrNoStore means New was called without WithStore.
	ErrNoStore = errors.New("session: no store configured")
	// ErrAnonymous means the call requires an authenticated session.
	ErrAnonymous = errors.New("session: session is anonymous")

	// ErrBadIdle means Idle or RememberIdle is not positive.
	ErrBadIdle = errors.New("session: idle ttl must be positive")
	// ErrBadMaxTTL means an absolute lifetime is negative or below its idle ttl.
	ErrBadMaxTTL = errors.New("session: max ttl must be zero or above the idle ttl")
	// ErrBadTouch means Touch is negative or exceeds the idle ttl.
	ErrBadTouch = errors.New("session: touch interval must be zero or within the idle ttl")
	// ErrTouchUnsupported means Touch is configured but the Store has no Toucher.
	ErrTouchUnsupported = errors.New("session: touch configured but store does not implement Toucher")
)
```

`auth/session/config.go`:

```go
package session

import "time"

// Config holds the expiry policy. A zero MaxTTL or RememberMax means no
// absolute cap: the session lives until logout or the idle timeout.
type Config struct {
	Idle         time.Duration `env:"SESSION_IDLE"`
	MaxTTL       time.Duration `env:"SESSION_MAX_TTL"`
	RememberIdle time.Duration `env:"SESSION_REMEMBER_IDLE"`
	RememberMax  time.Duration `env:"SESSION_REMEMBER_MAX_TTL"`
	Touch        time.Duration `env:"SESSION_TOUCH"`
}

// DefaultConfig returns a 24h idle / 7d absolute session, a 30d idle /
// 1y absolute remembered session, and a 5-minute touch interval.
func DefaultConfig() Config {
	return Config{
		Idle:         24 * time.Hour,
		MaxTTL:       7 * 24 * time.Hour,
		RememberIdle: 30 * 24 * time.Hour,
		RememberMax:  365 * 24 * time.Hour,
		Touch:        5 * time.Minute,
	}
}

// Validate reports whether the Config is usable.
func (c Config) Validate() error {
	if c.Idle <= 0 || c.RememberIdle <= 0 {
		return ErrBadIdle
	}
	if c.MaxTTL < 0 || (c.MaxTTL > 0 && c.MaxTTL < c.Idle) {
		return ErrBadMaxTTL
	}
	if c.RememberMax < 0 || (c.RememberMax > 0 && c.RememberMax < c.RememberIdle) {
		return ErrBadMaxTTL
	}
	if c.Touch < 0 || c.Touch > c.Idle {
		return ErrBadTouch
	}
	return nil
}
```

`auth/session/store.go`:

```go
package session

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"time"

	"github.com/dmitrymomot/forge/core/id"
)

// Record is the stored shape of a session: the first-class columns plus an
// opaque payload. Stores never interpret Payload and never see a Session.
type Record struct {
	CreatedAt   time.Time
	ExpiresAt   time.Time
	LastSeenAt  time.Time
	ElevatedAt  time.Time
	UserID      string
	IP          string
	UserAgent   string
	Fingerprint string
	Payload     []byte
	ID          id.UUID
	Remembered  bool
}

// Store is the minimum a backend must implement. Implementations must be safe
// for concurrent use and must never persist the raw token — key on Digest.
type Store interface {
	// Load returns the record for token, or ErrNotFound.
	Load(ctx context.Context, token string) (Record, error)
	// Save writes rec and returns the token the client should present next.
	// Server-side stores echo token back; a stateless store returns its fresh
	// encoding of rec, which is what lets both satisfy this interface.
	Save(ctx context.Context, token string, rec Record) (string, error)
	// Delete removes the record for token. Deleting an absent record is not an error.
	Delete(ctx context.Context, token string) error
}

// Toucher is the optional metadata-only refresh capability.
type Toucher interface {
	Touch(ctx context.Context, token string, lastSeenAt, expiresAt time.Time) error
}

// UserIndex is the optional per-user index behind device management.
type UserIndex interface {
	ListByUser(ctx context.Context, userID string) ([]Record, error)
	// DeleteByUser removes every record for userID except those in keep.
	DeleteByUser(ctx context.Context, userID string, keep ...id.UUID) error
	// DeleteOne removes sessionID only if it belongs to userID.
	DeleteOne(ctx context.Context, userID string, sessionID id.UUID) error
}

// Expirer is the optional bulk reaping capability. Stores whose backend expires
// records natively (a Mongo TTL index, a Redis key TTL) do not implement it.
type Expirer interface {
	DeleteExpired(ctx context.Context, now time.Time) (int, error)
}

// Digest maps a raw token to the value a store may persist. Drivers call this
// so the hashing rule lives in one place.
func Digest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
```

`auth/session/options.go`:

```go
package session

import (
	"log/slog"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

type options struct {
	store  Store
	clock  clock.Clock
	logger *slog.Logger
	cfg    Config
}

// Option configures the Manager. Options are applied over the Config passed to
// New, so an explicit option always wins over an env-loaded value.
type Option func(*options)

// WithStore sets the backing store. Required.
func WithStore(s Store) Option { return func(o *options) { o.store = s } }

// WithIdle sets the sliding idle timeout.
func WithIdle(d time.Duration) Option { return func(o *options) { o.cfg.Idle = d } }

// WithMaxTTL sets the absolute lifetime no activity extends. Zero means no cap.
func WithMaxTTL(d time.Duration) Option { return func(o *options) { o.cfg.MaxTTL = d } }

// WithRememberIdle sets the sliding idle timeout for remembered sessions.
func WithRememberIdle(d time.Duration) Option { return func(o *options) { o.cfg.RememberIdle = d } }

// WithRememberMaxTTL sets the absolute lifetime for remembered sessions. Zero means no cap.
func WithRememberMaxTTL(d time.Duration) Option { return func(o *options) { o.cfg.RememberMax = d } }

// WithTouch sets the metadata-only refresh interval: a request whose deadline
// moved by less than this does not write to the store. Zero disables touching,
// which makes every request a full save.
func WithTouch(d time.Duration) Option { return func(o *options) { o.cfg.Touch = d } }

// WithClock injects a clock for deterministic tests.
func WithClock(c clock.Clock) Option { return func(o *options) { o.clock = c } }

// WithLogger sets the logger. Defaults to logger.NewNope().
func WithLogger(l *slog.Logger) Option { return func(o *options) { o.logger = l } }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `just test ./auth/session/...`
Expected: PASS — 4 tests, all subtests green.

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./auth/session/...
just lint
git add auth/session/errors.go auth/session/config.go auth/session/options.go auth/session/store.go auth/session/config_test.go
git commit -m "feat(session): config, options, error sentinels, and the store seam"
```

---

### Task 2: Session value and payload dirty-tracking

**Files:**
- Create: `auth/session/session.go`
- Test: `auth/session/session_test.go`

**Interfaces:**
- Consumes: `Record` (Task 1).
- Produces: `Session` with accessors `ID() id.UUID`, `UserID() string`, `Token() string`, `CreatedAt/ExpiresAt/LastSeenAt/ElevatedAt() time.Time`, `IP/UserAgent/Fingerprint() string`, `Remembered() bool`, `ElevatedWithin(time.Duration) bool`, `IsNew() bool`; unexported `newSession(rec Record, token string, isNew bool) *Session`, `(*Session).raw() map[string]json.RawMessage`, `(*Session).markDirty(name string, v any)`, `(*Session).encode() error`, `(*Session).isDirty() bool`, `(*Session).record() Record`.

A `Session` is request-scoped and **not safe for concurrent use** — that is a documented property, not an oversight; adding a mutex would cost every accessor for a case that does not occur in a handler.

- [ ] **Step 1: Write the failing test**

`auth/session/session_test.go`:

```go
package session_test

import (
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/core/clock"
)

func TestElevatedWithin(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	clk := clock.NewMock(now)

	mgr := newTestManager(t, session.WithClock(clk))
	sess := mustStart(t, mgr)

	if sess.ElevatedWithin(time.Minute) {
		t.Fatal("a fresh anonymous session must not be elevated")
	}

	if err := mgr.Authenticate(t.Context(), sess, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if !sess.ElevatedWithin(time.Minute) {
		t.Fatal("Authenticate must stamp ElevatedAt")
	}

	clk.Advance(2 * time.Minute)
	if sess.ElevatedWithin(time.Minute) {
		t.Fatal("elevation must expire once the window has passed")
	}
	if !sess.ElevatedWithin(5 * time.Minute) {
		t.Fatal("a wider window must still accept the same stamp")
	}
}

func TestAnonymousSessionIsNewAndEmpty(t *testing.T) {
	mgr := newTestManager(t)
	sess := mustStart(t, mgr)

	if !sess.IsNew() {
		t.Fatal("a session that has never been saved must report IsNew")
	}
	if sess.UserID() != "" {
		t.Fatalf("UserID() = %q, want empty for anonymous", sess.UserID())
	}
	if sess.ID().IsZero() {
		t.Fatal("a session must carry an ID before it is saved")
	}
}
```

Add the shared helpers in the same file:

```go
func newTestManager(t *testing.T, opts ...session.Option) *session.Manager {
	t.Helper()
	base := []session.Option{session.WithStore(session.NewMemoryStore())}
	mgr, err := session.New(session.DefaultConfig(), append(base, opts...)...)
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	return mgr
}

func mustStart(t *testing.T, mgr *session.Manager) *session.Session {
	t.Helper()
	return mgr.Start()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./auth/session/...`
Expected: FAIL — `undefined: session.NewMemoryStore`, `undefined: session.New`, `undefined: session.Manager`. That is expected; Tasks 3–5 supply them. Comment out `session_test.go` contents is **not** acceptable — instead, verify the compile error names exactly those three symbols and move on.

- [ ] **Step 3: Write minimal implementation**

`auth/session/session.go`:

```go
package session

import (
	"encoding/json"
	"time"

	"github.com/dmitrymomot/forge/core/id"
)

// Session is one visitor's record plus its decoded payload. It is
// request-scoped and NOT safe for concurrent use: a handler that fans out to
// goroutines must serialize access itself.
type Session struct {
	rec     Record
	raw     map[string]json.RawMessage // lazily parsed from rec.Payload
	cache   map[string]any             // decoded namespace values
	dirty   map[string]struct{}
	token   string
	parsed  bool
	isNew   bool
	deleted bool
}

func newSession(rec Record, token string, isNew bool) *Session {
	return &Session{rec: rec, token: token, isNew: isNew}
}

// ID returns the stable session id. It survives token rotation.
func (s *Session) ID() id.UUID { return s.rec.ID }

// UserID returns the bound principal, or "" for an anonymous session.
func (s *Session) UserID() string { return s.rec.UserID }

// Token returns the credential the client should present next. It changes on
// every rotation, so read it after Authenticate, not before.
func (s *Session) Token() string { return s.token }

// CreatedAt returns when the session was first minted.
func (s *Session) CreatedAt() time.Time { return s.rec.CreatedAt }

// ExpiresAt returns the effective deadline: the lesser of the idle and
// absolute limits.
func (s *Session) ExpiresAt() time.Time { return s.rec.ExpiresAt }

// LastSeenAt returns the last time a request carried this session.
func (s *Session) LastSeenAt() time.Time { return s.rec.LastSeenAt }

// ElevatedAt returns when identity was last re-proved. Zero means never.
func (s *Session) ElevatedAt() time.Time { return s.rec.ElevatedAt }

// IP returns the client address pinned when the session was created.
func (s *Session) IP() string { return s.rec.IP }

// UserAgent returns the user agent pinned when the session was created.
func (s *Session) UserAgent() string { return s.rec.UserAgent }

// Fingerprint returns the device fingerprint hash pinned at creation.
func (s *Session) Fingerprint() string { return s.rec.Fingerprint }

// Remembered reports whether this session uses the remember-me deadlines.
// Transports read it to choose persistent versus session-scoped storage.
func (s *Session) Remembered() bool { return s.rec.Remembered }

// IsNew reports whether the session has never been persisted.
func (s *Session) IsNew() bool { return s.isNew }

// Authenticated reports whether a principal is bound.
func (s *Session) Authenticated() bool { return s.rec.UserID != "" }

// parse materializes the raw namespace map on first payload access.
func (s *Session) parse() error {
	if s.parsed {
		return nil
	}
	s.parsed = true
	s.raw = make(map[string]json.RawMessage)
	if len(s.rec.Payload) == 0 {
		return nil
	}
	return json.Unmarshal(s.rec.Payload, &s.raw)
}

// markDirty records a namespace write. Only dirty namespaces re-encode on save.
func (s *Session) markDirty(name string, v any) {
	if s.cache == nil {
		s.cache = make(map[string]any, 1)
	}
	if s.dirty == nil {
		s.dirty = make(map[string]struct{}, 1)
	}
	s.cache[name] = v
	s.dirty[name] = struct{}{}
}

func (s *Session) isDirty() bool { return len(s.dirty) > 0 }

// encode folds dirty namespaces back into the payload. Namespaces the process
// never touched keep their original bytes, so a plugin's keys survive a save by
// a handler that has never heard of that plugin.
func (s *Session) encode() error {
	if !s.isDirty() {
		return nil
	}
	if err := s.parse(); err != nil {
		return err
	}
	for name := range s.dirty {
		v, ok := s.cache[name]
		if !ok || v == nil {
			delete(s.raw, name)
			continue
		}
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		s.raw[name] = b
	}
	if len(s.raw) == 0 {
		s.rec.Payload = nil
		clear(s.dirty)
		return nil
	}
	b, err := json.Marshal(s.raw)
	if err != nil {
		return err
	}
	s.rec.Payload = b
	clear(s.dirty)
	return nil
}

func (s *Session) record() Record { return s.rec }
```

Add the elevation accessor, which needs the manager's clock — it takes the comparison time from the caller to stay clock-free:

```go
// ElevatedWithin reports whether identity was re-proved within d of now.
// The manager stamps now via elevatedNow so tests can drive a mock clock.
func (s *Session) ElevatedWithin(d time.Duration) bool {
	if s.rec.ElevatedAt.IsZero() || d <= 0 {
		return false
	}
	return s.now().Sub(s.rec.ElevatedAt) < d
}
```

with the clock threaded onto the session at construction:

```go
// in Session
now func() time.Time

// in newSession
func newSession(rec Record, token string, isNew bool, now func() time.Time) *Session {
	return &Session{rec: rec, token: token, isNew: isNew, now: now}
}
```

- [ ] **Step 4: Run test to verify it compiles**

Run: `go build ./auth/session/`
Expected: PASS (the package builds; `session_test.go` still fails to compile until Task 5).

- [ ] **Step 5: Commit**

```bash
just fmt ./auth/session/...
git add auth/session/session.go auth/session/session_test.go
git commit -m "feat(session): Session value with lazy payload parsing and dirty tracking"
```

---

### Task 3: Typed payload namespaces

**Files:**
- Create: `auth/session/namespace.go`
- Test: `auth/session/namespace_test.go`, `auth/session/namespace_internal_test.go`

**Interfaces:**
- Consumes: `Session.parse`, `Session.markDirty`, `Session.raw` (Task 2).
- Produces: `NewNamespace[T any](name string) *Namespace[T]`, `(*Namespace[T]).Get(*Session) (T, error)`, `(*Namespace[T]).Set(*Session, T)`, `(*Namespace[T]).Clear(*Session)`, `(*Namespace[T]).Name() string`.

- [ ] **Step 1: Write the failing test**

`auth/session/namespace_test.go`:

```go
package session_test

import (
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/auth/session"
)

type cartData struct {
	Items []string `json:"items"`
}

type prefsData struct {
	Theme string `json:"theme"`
}

var (
	nsCart  = session.NewNamespace[cartData]("test.cart")
	nsPrefs = session.NewNamespace[prefsData]("test.prefs")
)

func TestNamespaceRoundTrip(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()

	got, err := nsCart.Get(sess)
	if err != nil {
		t.Fatalf("Get on empty session: %v", err)
	}
	if len(got.Items) != 0 {
		t.Fatalf("empty session returned %v, want zero value", got)
	}

	nsCart.Set(sess, cartData{Items: []string{"sku-1"}})
	if err := mgr.Save(t.Context(), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := mgr.Load(t.Context(), sess.Token())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, err = nsCart.Get(reloaded)
	if err != nil {
		t.Fatalf("Get after reload: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0] != "sku-1" {
		t.Fatalf("round trip lost data: %+v", got)
	}
}

func TestUnknownNamespaceSurvivesSave(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()

	nsCart.Set(sess, cartData{Items: []string{"sku-1"}})
	nsPrefs.Set(sess, prefsData{Theme: "dark"})
	if err := mgr.Save(t.Context(), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// A second process touches only "cart" and never mentions "prefs".
	reloaded, err := mgr.Load(t.Context(), sess.Token())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	nsCart.Set(reloaded, cartData{Items: []string{"sku-2"}})
	if err := mgr.Save(t.Context(), reloaded); err != nil {
		t.Fatalf("Save: %v", err)
	}

	final, err := mgr.Load(t.Context(), reloaded.Token())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	prefs, err := nsPrefs.Get(final)
	if err != nil {
		t.Fatalf("Get prefs: %v", err)
	}
	if prefs.Theme != "dark" {
		t.Fatalf("untouched namespace was dropped: %+v", prefs)
	}
}

func TestNamespaceDecodeFailureIsAnError(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()

	// Write a shape the typed namespace cannot decode.
	nsPrefs.Set(sess, prefsData{Theme: "dark"})
	if err := mgr.Save(t.Context(), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, err := mgr.Load(t.Context(), sess.Token())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	mismatched := session.NewNamespace[[]int]("test.prefs.mismatch")
	mismatched.Set(reloaded, []int{1, 2})
	if err := mgr.Save(t.Context(), reloaded); err != nil {
		t.Fatalf("Save: %v", err)
	}

	final, err := mgr.Load(t.Context(), reloaded.Token())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wrongType := session.NewNamespace[prefsData]("test.prefs.mismatch")
	if _, err := wrongType.Get(final); err == nil {
		t.Fatal("Get must return an error on a decode failure, never a zero value")
	}
}

func TestDuplicateNamespacePanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("registering a duplicate namespace name must panic at init")
		}
		if !strings.Contains(toString(r), "test.cart") {
			t.Fatalf("panic message %v must name the colliding namespace", r)
		}
	}()
	_ = session.NewNamespace[cartData]("test.cart")
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if e, ok := v.(error); ok {
		return e.Error()
	}
	return ""
}
```

`auth/session/namespace_internal_test.go` — the one sanctioned white-box file, pinning the raw passthrough primitive:

```go
package session

import (
	"encoding/json"
	"testing"
)

func TestEncodePreservesUnknownRawKeys(t *testing.T) {
	rec := Record{Payload: []byte(`{"known":{"a":1},"unknown":{"b":2}}`)}
	s := newSession(rec, "tok", false, nil)

	s.markDirty("known", map[string]int{"a": 9})
	if err := s.encode(); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var out map[string]json.RawMessage
	if err := json.Unmarshal(s.record().Payload, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(out["unknown"]) != `{"b":2}` {
		t.Fatalf("unknown key mutated: %s", out["unknown"])
	}
	if string(out["known"]) != `{"a":9}` {
		t.Fatalf("known key not updated: %s", out["known"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./auth/session/ -run TestEncodePreservesUnknownRawKeys`
Expected: FAIL — `undefined: newSession` has 3 args (Task 2 added the clock parameter) or `undefined: markDirty`.

- [ ] **Step 3: Write minimal implementation**

`auth/session/namespace.go`:

```go
package session

import (
	"encoding/json"
	"fmt"
	"sync"
)

var (
	registryMu sync.Mutex
	registry   = make(map[string]struct{})
)

// Namespace is a typed, independently-owned slice of the session payload. An
// app and its plugins each declare their own; they coexist without collisions,
// and a namespace nobody reads costs no JSON work.
type Namespace[T any] struct {
	name string
}

// NewNamespace declares a namespace. Call it once, at package scope. A
// duplicate name panics: a collision is a programming error, and discovering
// it in production means one owner silently overwriting another's data.
func NewNamespace[T any](name string) *Namespace[T] {
	if name == "" {
		panic("session: namespace name must not be empty")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := registry[name]; dup {
		panic(fmt.Sprintf("session: namespace %q is already registered", name))
	}
	registry[name] = struct{}{}
	return &Namespace[T]{name: name}
}

// Name returns the namespace's key in the payload.
func (n *Namespace[T]) Name() string { return n.name }

// Get decodes this namespace, caching the result for the rest of the request.
// A namespace that has never been written returns the zero value and a nil
// error; one that fails to decode returns the error, never a zero value, so
// corrupt or schema-drifted data fails closed.
func (n *Namespace[T]) Get(s *Session) (T, error) {
	var zero T
	if s == nil {
		return zero, ErrNoSession
	}
	if v, ok := s.cache[n.name]; ok {
		typed, ok := v.(T)
		if !ok {
			return zero, fmt.Errorf("session: namespace %q cached as %T", n.name, v)
		}
		return typed, nil
	}
	if err := s.parse(); err != nil {
		return zero, fmt.Errorf("session: payload decode: %w", err)
	}
	b, ok := s.raw[n.name]
	if !ok || len(b) == 0 {
		return zero, nil
	}
	var out T
	if err := json.Unmarshal(b, &out); err != nil {
		return zero, fmt.Errorf("session: namespace %q decode: %w", n.name, err)
	}
	if s.cache == nil {
		s.cache = make(map[string]any, 1)
	}
	s.cache[n.name] = out
	return out, nil
}

// Set stores v and marks only this namespace dirty.
func (n *Namespace[T]) Set(s *Session, v T) { s.markDirty(n.name, v) }

// Clear removes this namespace from the payload.
func (n *Namespace[T]) Clear(s *Session) { s.markDirty(n.name, nil) }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `just test ./auth/session/`
Expected: PASS — namespace round trip, unknown-key survival, decode-failure error, duplicate panic, and the internal raw-passthrough test.

- [ ] **Step 5: Commit**

```bash
just fmt ./auth/session/...
just lint
git add auth/session/namespace.go auth/session/namespace_test.go auth/session/namespace_internal_test.go
git commit -m "feat(session): typed payload namespaces with lazy decode and raw passthrough"
```

---

### Task 4: In-memory store and the conformance suite

**Files:**
- Create: `auth/session/memory.go`, `auth/session/storetest/storetest.go`
- Test: `auth/session/memory_test.go`

**Interfaces:**
- Consumes: `Record`, `Store`, `Toucher`, `UserIndex`, `Expirer`, `Digest` (Task 1).
- Produces: `NewMemoryStore() *MemoryStore` implementing all four interfaces; `storetest.Run(t *testing.T, newStore func(*testing.T) session.Store)` — the suite every driver runs, which detects capabilities and skips the sections a store does not claim.

- [ ] **Step 1: Write the failing test**

`auth/session/memory_test.go`:

```go
package session_test

import (
	"testing"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/auth/session/storetest"
)

func TestMemoryStoreConformance(t *testing.T) {
	storetest.Run(t, func(t *testing.T) session.Store {
		return session.NewMemoryStore()
	})
}
```

`auth/session/storetest/storetest.go` — the suite itself is the deliverable, so write it before the store:

```go
// Package storetest is the conformance suite every session.Store driver runs.
// It exercises the required interface, then each optional capability the store
// claims, skipping the rest.
package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/core/id"
)

// Run executes the full suite against stores produced by newStore.
func Run(t *testing.T, newStore func(*testing.T) session.Store) {
	t.Helper()
	t.Run("LoadMissing", func(t *testing.T) { testLoadMissing(t, newStore(t)) })
	t.Run("SaveLoadDelete", func(t *testing.T) { testSaveLoadDelete(t, newStore(t)) })
	t.Run("SaveReturnsToken", func(t *testing.T) { testSaveReturnsToken(t, newStore(t)) })
	t.Run("DeleteMissingIsNotAnError", func(t *testing.T) { testDeleteMissing(t, newStore(t)) })
	t.Run("Toucher", func(t *testing.T) { testToucher(t, newStore(t)) })
	t.Run("UserIndex", func(t *testing.T) { testUserIndex(t, newStore(t)) })
	t.Run("Expirer", func(t *testing.T) { testExpirer(t, newStore(t)) })
}

func rec(userID string, expiresAt time.Time) session.Record {
	now := time.Now().UTC().Truncate(time.Millisecond)
	return session.Record{
		ID:         id.NewUUID(),
		UserID:     userID,
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  expiresAt,
		Payload:    []byte(`{"k":{"v":1}}`),
	}
}

func testLoadMissing(t *testing.T, st session.Store) {
	if _, err := st.Load(context.Background(), "nope"); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("Load(missing) = %v, want ErrNotFound", err)
	}
}

func testSaveLoadDelete(t *testing.T, st session.Store) {
	ctx := context.Background()
	want := rec("u1", time.Now().Add(time.Hour))

	tok, err := st.Save(ctx, "tok-1", want)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := st.Load(ctx, tok)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ID != want.ID || got.UserID != want.UserID {
		t.Fatalf("Load returned %+v, want id=%v user=%q", got, want.ID, want.UserID)
	}
	if string(got.Payload) != string(want.Payload) {
		t.Fatalf("payload round trip: got %s want %s", got.Payload, want.Payload)
	}

	if err := st.Delete(ctx, tok); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := st.Load(ctx, tok); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("Load after Delete = %v, want ErrNotFound", err)
	}
}

func testSaveReturnsToken(t *testing.T, st session.Store) {
	tok, err := st.Save(context.Background(), "tok-2", rec("u1", time.Now().Add(time.Hour)))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if tok == "" {
		t.Fatal("Save must return the token the client should present next")
	}
}

func testDeleteMissing(t *testing.T, st session.Store) {
	if err := st.Delete(context.Background(), "never-existed"); err != nil {
		t.Fatalf("Delete(missing) = %v, want nil", err)
	}
}

func testToucher(t *testing.T, st session.Store) {
	tc, ok := st.(session.Toucher)
	if !ok {
		t.Skip("store does not implement Toucher")
	}
	ctx := context.Background()
	r := rec("u1", time.Now().Add(time.Hour))
	tok, err := st.Save(ctx, "tok-3", r)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	newSeen := r.LastSeenAt.Add(10 * time.Minute)
	newExp := r.ExpiresAt.Add(10 * time.Minute)
	if err := tc.Touch(ctx, tok, newSeen, newExp); err != nil {
		t.Fatalf("Touch: %v", err)
	}

	got, err := st.Load(ctx, tok)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !got.ExpiresAt.Equal(newExp) {
		t.Fatalf("Touch did not move ExpiresAt: got %v want %v", got.ExpiresAt, newExp)
	}
	if string(got.Payload) != string(r.Payload) {
		t.Fatal("Touch must be metadata-only and must not disturb the payload")
	}
}

func testUserIndex(t *testing.T, st session.Store) {
	ix, ok := st.(session.UserIndex)
	if !ok {
		t.Skip("store does not implement UserIndex")
	}
	ctx := context.Background()
	a, b, other := rec("u1", time.Now().Add(time.Hour)), rec("u1", time.Now().Add(time.Hour)), rec("u2", time.Now().Add(time.Hour))
	for tok, r := range map[string]session.Record{"ta": a, "tb": b, "tc": other} {
		if _, err := st.Save(ctx, tok, r); err != nil {
			t.Fatalf("Save %s: %v", tok, err)
		}
	}

	list, err := ix.ListByUser(ctx, "u1")
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListByUser returned %d records, want 2", len(list))
	}

	if err := ix.DeleteOne(ctx, "u2", a.ID); err != nil {
		t.Fatalf("DeleteOne cross-user: %v", err)
	}
	if list, _ := ix.ListByUser(ctx, "u1"); len(list) != 2 {
		t.Fatal("DeleteOne must refuse a session that belongs to another user")
	}

	if err := ix.DeleteByUser(ctx, "u1", b.ID); err != nil {
		t.Fatalf("DeleteByUser: %v", err)
	}
	list, err = ix.ListByUser(ctx, "u1")
	if err != nil {
		t.Fatalf("ListByUser after keep-list delete: %v", err)
	}
	if len(list) != 1 || list[0].ID != b.ID {
		t.Fatalf("keep-list not honored: %+v", list)
	}
	if others, _ := ix.ListByUser(ctx, "u2"); len(others) != 1 {
		t.Fatal("DeleteByUser must not touch another user's sessions")
	}
}

func testExpirer(t *testing.T, st session.Store) {
	ex, ok := st.(session.Expirer)
	if !ok {
		t.Skip("store does not implement Expirer")
	}
	ctx := context.Background()
	past := rec("u1", time.Now().Add(-time.Hour))
	future := rec("u1", time.Now().Add(time.Hour))
	if _, err := st.Save(ctx, "expired", past); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := st.Save(ctx, "live", future); err != nil {
		t.Fatalf("Save: %v", err)
	}

	n, err := ex.DeleteExpired(ctx, time.Now())
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if n != 1 {
		t.Fatalf("DeleteExpired removed %d, want 1", n)
	}
	if _, err := st.Load(ctx, "live"); err != nil {
		t.Fatalf("DeleteExpired removed a live session: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./auth/session/ -run TestMemoryStoreConformance`
Expected: FAIL — `undefined: session.NewMemoryStore`.

- [ ] **Step 3: Write minimal implementation**

`auth/session/memory.go`:

```go
package session

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/core/id"
)

// MemoryStore is an in-process Store for tests and development. It implements
// every optional capability, so the conformance suite exercises all of them.
// It is safe for concurrent use and holds everything in memory: restarting the
// process logs everyone out.
type MemoryStore struct {
	byDigest map[string]Record
	mu       sync.RWMutex
}

var (
	_ Store     = (*MemoryStore)(nil)
	_ Toucher   = (*MemoryStore)(nil)
	_ UserIndex = (*MemoryStore)(nil)
	_ Expirer   = (*MemoryStore)(nil)
)

// NewMemoryStore returns an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byDigest: make(map[string]Record)}
}

// Load implements Store.
func (m *MemoryStore) Load(_ context.Context, token string) (Record, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rec, ok := m.byDigest[Digest(token)]
	if !ok {
		return Record{}, ErrNotFound
	}
	return cloneRecord(rec), nil
}

// Save implements Store. The token is echoed back: this is a server-side store,
// so the client's credential does not change unless the manager rotates it.
func (m *MemoryStore) Save(_ context.Context, token string, rec Record) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byDigest[Digest(token)] = cloneRecord(rec)
	return token, nil
}

// Delete implements Store.
func (m *MemoryStore) Delete(_ context.Context, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.byDigest, Digest(token))
	return nil
}

// Touch implements Toucher: metadata only, payload untouched.
func (m *MemoryStore) Touch(_ context.Context, token string, lastSeenAt, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := Digest(token)
	rec, ok := m.byDigest[d]
	if !ok {
		return ErrNotFound
	}
	rec.LastSeenAt = lastSeenAt
	rec.ExpiresAt = expiresAt
	m.byDigest[d] = rec
	return nil
}

// ListByUser implements UserIndex, newest first.
func (m *MemoryStore) ListByUser(_ context.Context, userID string) ([]Record, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []Record
	for _, rec := range m.byDigest {
		if rec.UserID == userID {
			out = append(out, cloneRecord(rec))
		}
	}
	slices.SortFunc(out, func(a, b Record) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return out, nil
}

// DeleteByUser implements UserIndex, preserving the keep list.
func (m *MemoryStore) DeleteByUser(_ context.Context, userID string, keep ...id.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for d, rec := range m.byDigest {
		if rec.UserID == userID && !slices.Contains(keep, rec.ID) {
			delete(m.byDigest, d)
		}
	}
	return nil
}

// DeleteOne implements UserIndex. It removes every record carrying sessionID —
// rotation can leave more than one digest pointing at the same session — and
// only when the record belongs to userID.
func (m *MemoryStore) DeleteOne(_ context.Context, userID string, sessionID id.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for d, rec := range m.byDigest {
		if rec.UserID == userID && rec.ID == sessionID {
			delete(m.byDigest, d)
		}
	}
	return nil
}

// DeleteExpired implements Expirer.
func (m *MemoryStore) DeleteExpired(_ context.Context, now time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for d, rec := range m.byDigest {
		if !rec.ExpiresAt.IsZero() && !rec.ExpiresAt.After(now) {
			delete(m.byDigest, d)
			n++
		}
	}
	return n, nil
}

// cloneRecord copies the payload so a caller mutating the returned slice cannot
// corrupt the stored record.
func cloneRecord(rec Record) Record {
	rec.Payload = slices.Clone(rec.Payload)
	return rec
}
```

Drop the `maps` import — only `slices`, `sync`, `time`, `context`, and `core/id` are used.

- [ ] **Step 4: Run test to verify it passes**

Run: `just test ./auth/session/ -run TestMemoryStoreConformance -v`
Expected: PASS — all seven subtests, none skipped (MemoryStore claims every capability).

- [ ] **Step 5: Commit**

```bash
just fmt ./auth/session/...
just lint
git add auth/session/memory.go auth/session/storetest/storetest.go auth/session/memory_test.go
git commit -m "feat(session): in-memory store and the store conformance suite"
```

---

### Task 5: Manager — construction, capability detection, load and save

**Files:**
- Create: `auth/session/manager.go`
- Test: `auth/session/manager_test.go`

**Interfaces:**
- Consumes: `Config`, `Option`, `Store` and capability interfaces (Task 1); `Session`, `newSession` (Task 2).
- Produces: `type Manager struct`, `New(cfg Config, opts ...Option) (*Manager, error)`, `(*Manager).Start() *Session`, `(*Manager).Load(ctx, token string) (*Session, error)`, `(*Manager).Save(ctx, *Session) error`, `(*Manager).deadline(rec Record, now time.Time) time.Time`, `(*Manager).touchDue(rec Record, now time.Time) bool`, `(*Manager).now() time.Time`.

- [ ] **Step 1: Write the failing test**

`auth/session/manager_test.go`:

```go
package session_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/core/clock"
)

// noTouchStore implements Store but not Toucher.
type noTouchStore struct{ *session.MemoryStore }

func TestNewRequiresStore(t *testing.T) {
	if _, err := session.New(session.DefaultConfig()); !errors.Is(err, session.ErrNoStore) {
		t.Fatalf("New without WithStore = %v, want ErrNoStore", err)
	}
}

func TestNewRejectsTouchWithoutToucher(t *testing.T) {
	_, err := session.New(session.DefaultConfig(),
		session.WithStore(noTouchStore{session.NewMemoryStore()}),
		session.WithTouch(time.Minute),
	)
	if !errors.Is(err, session.ErrTouchUnsupported) {
		t.Fatalf("New = %v, want ErrTouchUnsupported — a configured option whose capability is missing is a boot error", err)
	}
}

func TestStartCostsNoStorage(t *testing.T) {
	store := session.NewMemoryStore()
	mgr, err := session.New(session.DefaultConfig(), session.WithStore(store))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	sess := mgr.Start()
	if _, err := store.Load(t.Context(), sess.Token()); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("Start must not write a row; the row is minted on first save")
	}
}

func TestSaveThenLoad(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()
	nsCart.Set(sess, cartData{Items: []string{"a"}})

	if err := mgr.Save(t.Context(), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if sess.IsNew() {
		t.Fatal("a saved session must no longer report IsNew")
	}

	got, err := mgr.Load(t.Context(), sess.Token())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ID() != sess.ID() {
		t.Fatalf("Load returned session %v, want %v", got.ID(), sess.ID())
	}
}

func TestLoadExpiredIsErrExpired(t *testing.T) {
	clk := clock.NewMock(time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC))
	mgr := newTestManager(t, session.WithClock(clk), session.WithIdle(time.Hour), session.WithMaxTTL(0))

	sess := mgr.Start()
	if err := mgr.Save(t.Context(), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	clk.Advance(2 * time.Hour)
	if _, err := mgr.Load(t.Context(), sess.Token()); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("Load after idle timeout = %v, want ErrExpired", err)
	}
}

func TestAbsoluteLifetimeCapsSliding(t *testing.T) {
	start := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	clk := clock.NewMock(start)
	mgr := newTestManager(t, session.WithClock(clk),
		session.WithIdle(time.Hour), session.WithMaxTTL(90*time.Minute))

	sess := mgr.Start()
	if err := mgr.Save(t.Context(), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// 30 minutes in, sliding would reach start+90m; the cap is start+90m too.
	clk.Advance(30 * time.Minute)
	loaded, err := mgr.Load(t.Context(), sess.Token())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := mgr.Save(t.Context(), loaded); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if want := start.Add(90 * time.Minute); !loaded.ExpiresAt().Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v (capped by MaxTTL)", loaded.ExpiresAt(), want)
	}

	// Past the cap, no amount of activity revives it.
	clk.Advance(61 * time.Minute)
	if _, err := mgr.Load(t.Context(), loaded.Token()); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("Load past the absolute lifetime = %v, want ErrExpired", err)
	}
}

func TestZeroMaxTTLNeverCaps(t *testing.T) {
	start := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	clk := clock.NewMock(start)
	mgr := newTestManager(t, session.WithClock(clk),
		session.WithIdle(time.Hour), session.WithMaxTTL(0))

	sess := mgr.Start()
	if err := mgr.Save(t.Context(), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Stay active well past any absolute lifetime; sliding alone must keep it alive.
	for range 100 {
		clk.Advance(30 * time.Minute)
		loaded, err := mgr.Load(t.Context(), sess.Token())
		if err != nil {
			t.Fatalf("Load during continuous activity: %v", err)
		}
		if err := mgr.Save(t.Context(), loaded); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./auth/session/ -run TestNewRequiresStore`
Expected: FAIL — `undefined: session.New`.

- [ ] **Step 3: Write minimal implementation**

`auth/session/manager.go`:

```go
package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/ops/logger"
)

// tokenBytes is the entropy of a minted session token.
const tokenBytes = 32

// Manager owns session lifecycle and storage. It knows nothing about HTTP:
// no requests, no transports, no policies. Safe for concurrent use.
type Manager struct {
	store   Store
	toucher Toucher   // nil when the store lacks the capability
	index   UserIndex // nil when the store lacks the capability
	expirer Expirer   // nil when the store lacks the capability
	clk     clock.Clock
	log     *slog.Logger
	cfg     Config
}

// New builds a Manager. Store capabilities are detected once, here — a missing
// capability that a configured option requires is a boot error, not a surprise
// at the first request.
func New(cfg Config, opts ...Option) (*Manager, error) {
	o := options{cfg: cfg, clock: clock.System(), logger: logger.NewNope()}
	for _, opt := range opts {
		opt(&o)
	}
	if o.store == nil {
		return nil, ErrNoStore
	}
	if err := o.cfg.Validate(); err != nil {
		return nil, err
	}

	m := &Manager{store: o.store, clk: o.clock, log: o.logger, cfg: o.cfg}
	m.toucher, _ = o.store.(Toucher)
	m.index, _ = o.store.(UserIndex)
	m.expirer, _ = o.store.(Expirer)

	if m.cfg.Touch > 0 && m.toucher == nil {
		return nil, ErrTouchUnsupported
	}
	return m, nil
}

func (m *Manager) now() time.Time { return m.clk.Now().UTC() }

// Start returns a fresh anonymous session. It performs no I/O and mints no
// row: storage is touched on the first save.
func (m *Manager) Start() *Session {
	now := m.now()
	rec := Record{
		ID:         id.NewUUID(),
		CreatedAt:  now,
		LastSeenAt: now,
	}
	rec.ExpiresAt = m.deadline(rec, now)
	return newSession(rec, newToken(), true, m.now)
}

// Load fetches the session for token. An expired record yields ErrExpired and
// is deleted; a missing one yields ErrNotFound.
func (m *Manager) Load(ctx context.Context, token string) (*Session, error) {
	rec, err := m.store.Load(ctx, token)
	if err != nil {
		return nil, err
	}
	now := m.now()
	if !rec.ExpiresAt.IsZero() && !rec.ExpiresAt.After(now) {
		if err := m.store.Delete(ctx, token); err != nil {
			m.log.WarnContext(ctx, "session: deleting expired record failed", slog.Any("error", err))
		}
		return nil, ErrExpired
	}
	rec.LastSeenAt = now
	rec.ExpiresAt = m.deadline(rec, now)
	return newSession(rec, token, false, m.now), nil
}

// Save persists the session, encoding any dirty namespaces first. The token the
// store returns becomes the session's token, which is what lets a stateless
// store re-encode the whole record into a fresh credential.
func (m *Manager) Save(ctx context.Context, s *Session) error {
	if s == nil {
		return ErrNoSession
	}
	if err := s.encode(); err != nil {
		return err
	}
	now := m.now()
	s.rec.LastSeenAt = now
	s.rec.ExpiresAt = m.deadline(s.rec, now)

	tok, err := m.store.Save(ctx, s.token, s.rec)
	if err != nil {
		return err
	}
	s.token = tok
	s.isNew = false
	return nil
}

// deadline is the effective expiry: the sliding idle window, capped by the
// absolute lifetime when one is configured. A zero cap means no cap.
func (m *Manager) deadline(rec Record, now time.Time) time.Time {
	idle, maxTTL := m.cfg.Idle, m.cfg.MaxTTL
	if rec.Remembered {
		idle, maxTTL = m.cfg.RememberIdle, m.cfg.RememberMax
	}
	exp := now.Add(idle)
	if maxTTL > 0 {
		if capped := rec.CreatedAt.Add(maxTTL); capped.Before(exp) {
			return capped
		}
	}
	return exp
}

// touchDue reports whether enough time has passed since the LastSeenAt that
// was actually persisted to justify a metadata write. It must read
// storedSeenAt, not rec.LastSeenAt: Load refreshes the in-memory LastSeenAt to
// now, so comparing against it would make the interval always zero and no
// request would ever touch.
func (m *Manager) touchDue(s *Session, now time.Time) bool {
	if m.cfg.Touch <= 0 || m.toucher == nil || s.isNew {
		return false
	}
	return now.Sub(s.storedSeenAt) >= m.cfg.Touch
}

func newToken() string {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		panic("session: crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
```

The `Load` path above must compare against the **stored** `LastSeenAt` for touch decisions, so keep the pre-refresh value on the session:

```go
// in Session
storedSeenAt time.Time

// in Manager.Load, before overwriting
sess := newSession(rec, token, false, m.now)
sess.storedSeenAt = rec.LastSeenAt   // capture before the refresh below
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `just test ./auth/session/`
Expected: PASS — including `session_test.go` and `namespace_test.go` from Tasks 2–3, which compile for the first time now.

- [ ] **Step 5: Commit**

```bash
just fmt ./auth/session/...
just lint
git add auth/session/manager.go auth/session/manager_test.go
git commit -m "feat(session): Manager with capability detection, load, save, and effective deadlines"
```

---

### Task 6: Authenticate, Rotate, Destroy, Elevate, Rebind

**Files:**
- Modify: `auth/session/manager.go`
- Test: `auth/session/lifecycle_test.go`

**Interfaces:**
- Consumes: `Manager.Save`, `Manager.deadline`, `newToken` (Task 5).
- Produces: `AuthOption`, `Remember(bool) AuthOption`, `(*Manager).Authenticate(ctx, *Session, userID string, opts ...AuthOption) error`, `(*Manager).Rotate(ctx, *Session) error`, `(*Manager).Destroy(ctx, *Session) error`, `(*Manager).Elevate(ctx, *Session) error`, `Bind` struct, `(*Manager).Rebind(ctx, *Session, Bind) error`.

- [ ] **Step 1: Write the failing test**

`auth/session/lifecycle_test.go`:

```go
package session_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/core/clock"
)

// failingSaveStore fails the Nth save, to prove rollback.
type failingSaveStore struct {
	*session.MemoryStore
	failOn int
	calls  int
}

func (f *failingSaveStore) Save(ctx context.Context, token string, rec session.Record) (string, error) {
	f.calls++
	if f.calls == f.failOn {
		return "", errors.New("store unavailable")
	}
	return f.MemoryStore.Save(ctx, token, rec)
}

func TestAuthenticateRotatesTokenAndPreservesIdentity(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()
	if err := mgr.Save(t.Context(), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	oldToken, oldID, oldCreated := sess.Token(), sess.ID(), sess.CreatedAt()

	if err := mgr.Authenticate(t.Context(), sess, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	if sess.Token() == oldToken {
		t.Fatal("Authenticate must rotate the token — session fixation defense")
	}
	if sess.ID() != oldID {
		t.Fatal("Authenticate must preserve the session ID across rotation")
	}
	if !sess.CreatedAt().Equal(oldCreated) {
		t.Fatal("Authenticate must preserve CreatedAt across rotation")
	}
	if sess.UserID() != "u1" {
		t.Fatalf("UserID() = %q, want u1", sess.UserID())
	}
	if _, err := mgr.Load(t.Context(), oldToken); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("the pre-rotation token must stop working")
	}
}

func TestAuthenticatePreservesPayload(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()
	nsCart.Set(sess, cartData{Items: []string{"guest-item"}})
	if err := mgr.Save(t.Context(), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := mgr.Authenticate(t.Context(), sess, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	reloaded, err := mgr.Load(t.Context(), sess.Token())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cart, err := nsCart.Get(reloaded)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(cart.Items) != 1 || cart.Items[0] != "guest-item" {
		t.Fatalf("anonymous cart lost on login: %+v", cart)
	}
}

func TestAuthenticateRollsBackTokenOnFailedSave(t *testing.T) {
	store := &failingSaveStore{MemoryStore: session.NewMemoryStore()}
	mgr, err := session.New(session.DefaultConfig(), session.WithStore(store))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	sess := mgr.Start()
	if err := mgr.Save(t.Context(), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
	oldToken := sess.Token()

	store.failOn = store.calls + 1
	if err := mgr.Authenticate(t.Context(), sess, "u1"); err == nil {
		t.Fatal("Authenticate must surface the store failure")
	}

	if sess.Token() != oldToken {
		t.Fatal("a failed save must roll the token back, or the client is holding a credential no record answers to")
	}
	if sess.UserID() != "" {
		t.Fatal("a failed save must roll the user binding back too")
	}
	if _, err := mgr.Load(t.Context(), oldToken); err != nil {
		t.Fatalf("the original session must still load after a failed Authenticate: %v", err)
	}
}

func TestRememberSelectsTheRememberDeadlines(t *testing.T) {
	start := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	clk := clock.NewMock(start)
	mgr := newTestManager(t, session.WithClock(clk),
		session.WithIdle(time.Hour), session.WithMaxTTL(0),
		session.WithRememberIdle(30*24*time.Hour), session.WithRememberMaxTTL(0))

	plain := mgr.Start()
	if err := mgr.Authenticate(t.Context(), plain, "u1", session.Remember(false)); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if plain.Remembered() {
		t.Fatal("Remember(false) must not mark the session remembered")
	}
	if want := start.Add(time.Hour); !plain.ExpiresAt().Equal(want) {
		t.Fatalf("plain ExpiresAt = %v, want %v", plain.ExpiresAt(), want)
	}

	remembered := mgr.Start()
	if err := mgr.Authenticate(t.Context(), remembered, "u2", session.Remember(true)); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if !remembered.Remembered() {
		t.Fatal("Remember(true) must mark the session remembered")
	}
	if want := start.Add(30 * 24 * time.Hour); !remembered.ExpiresAt().Equal(want) {
		t.Fatalf("remembered ExpiresAt = %v, want %v", remembered.ExpiresAt(), want)
	}
}

func TestDestroyRemovesTheRecord(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()
	if err := mgr.Save(t.Context(), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
	token := sess.Token()

	if err := mgr.Destroy(t.Context(), sess); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if _, err := mgr.Load(t.Context(), token); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("Load after Destroy = %v, want ErrNotFound", err)
	}
}

func TestElevationSurvivesRotation(t *testing.T) {
	start := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	clk := clock.NewMock(start)
	mgr := newTestManager(t, session.WithClock(clk))

	sess := mgr.Start()
	if err := mgr.Authenticate(t.Context(), sess, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if err := mgr.Rotate(t.Context(), sess); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if !sess.ElevatedWithin(time.Minute) {
		t.Fatal("rotation must preserve ElevatedAt")
	}
}

func TestElevateStampsFreshly(t *testing.T) {
	start := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	clk := clock.NewMock(start)
	mgr := newTestManager(t, session.WithClock(clk))

	sess := mgr.Start()
	if err := mgr.Authenticate(t.Context(), sess, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	clk.Advance(20 * time.Minute)
	if sess.ElevatedWithin(10 * time.Minute) {
		t.Fatal("elevation must have gone stale")
	}
	if err := mgr.Elevate(t.Context(), sess); err != nil {
		t.Fatalf("Elevate: %v", err)
	}
	if !sess.ElevatedWithin(10 * time.Minute) {
		t.Fatal("Elevate must refresh the stamp")
	}
}

func TestRebindReplacesPinnedMetadata(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()
	if err := mgr.Rebind(t.Context(), sess, session.Bind{IP: "203.0.113.4", UserAgent: "Chrome", Fingerprint: "fp1"}); err != nil {
		t.Fatalf("Rebind: %v", err)
	}
	if sess.IP() != "203.0.113.4" || sess.UserAgent() != "Chrome" || sess.Fingerprint() != "fp1" {
		t.Fatalf("Rebind did not apply: ip=%q ua=%q fp=%q", sess.IP(), sess.UserAgent(), sess.Fingerprint())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./auth/session/ -run TestAuthenticate`
Expected: FAIL — `undefined: mgr.Authenticate`.

- [ ] **Step 3: Write minimal implementation**

Append to `auth/session/manager.go`:

```go
// AuthOption tunes Authenticate.
type AuthOption func(*authOptions)

type authOptions struct{ remember bool }

// Remember selects the remember-me deadline pair and marks the record so a
// transport can choose persistent client storage. Taking the bool directly
// keeps call sites free of conditional option slices.
func Remember(v bool) AuthOption { return func(o *authOptions) { o.remember = v } }

// Authenticate binds userID to the session and rotates the token, which is
// mandatory: reusing the pre-login credential is the session fixation bug. The
// session ID, CreatedAt, and payload survive; a failed save rolls every field
// back so the client is never left holding a credential no record answers to.
func (m *Manager) Authenticate(ctx context.Context, s *Session, userID string, opts ...AuthOption) error {
	if s == nil {
		return ErrNoSession
	}
	if userID == "" {
		return ErrAnonymous
	}
	var o authOptions
	for _, opt := range opts {
		opt(&o)
	}

	oldToken, oldRec := s.token, s.rec
	now := m.now()

	s.rec.UserID = userID
	s.rec.Remembered = o.remember
	s.rec.ElevatedAt = now
	s.token = newToken()

	if err := m.Save(ctx, s); err != nil {
		s.token, s.rec = oldToken, oldRec
		return err
	}
	if !s.isNewBefore(oldRec) {
		if err := m.store.Delete(ctx, oldToken); err != nil {
			m.log.WarnContext(ctx, "session: deleting pre-rotation record failed", slog.Any("error", err))
		}
	}
	return nil
}

// Rotate issues a fresh token for the same session, preserving every field.
func (m *Manager) Rotate(ctx context.Context, s *Session) error {
	if s == nil {
		return ErrNoSession
	}
	oldToken := s.token
	s.token = newToken()
	if err := m.Save(ctx, s); err != nil {
		s.token = oldToken
		return err
	}
	if oldToken != s.token {
		if err := m.store.Delete(ctx, oldToken); err != nil {
			m.log.WarnContext(ctx, "session: deleting pre-rotation record failed", slog.Any("error", err))
		}
	}
	return nil
}

// Destroy removes the record. The caller's transport clears the credential.
func (m *Manager) Destroy(ctx context.Context, s *Session) error {
	if s == nil {
		return ErrNoSession
	}
	if err := m.store.Delete(ctx, s.token); err != nil {
		return err
	}
	s.deleted = true
	return nil
}

// Elevate stamps a successful identity re-proof. Session records that it
// happened; auth/access decides what it entitles the user to.
func (m *Manager) Elevate(ctx context.Context, s *Session) error {
	if s == nil {
		return ErrNoSession
	}
	s.rec.ElevatedAt = m.now()
	return m.Save(ctx, s)
}

// Bind carries the pinned device metadata Rebind writes.
type Bind struct {
	IP          string
	UserAgent   string
	Fingerprint string
}

// Rebind replaces the pinned metadata. This is the deliberate re-pin after a
// successful re-authentication — the middleware never does it implicitly,
// because a per-request refresh would let a stolen credential overwrite the
// binding on its first use and match forever after.
func (m *Manager) Rebind(ctx context.Context, s *Session, b Bind) error {
	if s == nil {
		return ErrNoSession
	}
	s.rec.IP, s.rec.UserAgent, s.rec.Fingerprint = b.IP, b.UserAgent, b.Fingerprint
	return m.Save(ctx, s)
}

// isNewBefore reports whether the session had never been persisted before the
// current operation, in which case there is no prior record to delete.
func (s *Session) isNewBefore(prev Record) bool { return s.isNew && prev.ID == s.rec.ID }
```

Fix the ordering bug this introduces: `m.Save` sets `s.isNew = false`, so `isNewBefore` must be evaluated **before** the save. Capture it first:

```go
	wasNew := s.isNew
	if err := m.Save(ctx, s); err != nil {
		s.token, s.rec = oldToken, oldRec
		return err
	}
	if !wasNew {
		if err := m.store.Delete(ctx, oldToken); err != nil {
			m.log.WarnContext(ctx, "session: deleting pre-rotation record failed", slog.Any("error", err))
		}
	}
```

and delete `isNewBefore` entirely.

- [ ] **Step 4: Run tests to verify they pass**

Run: `just test ./auth/session/ -v`
Expected: PASS — all eight lifecycle tests, including the rollback proof.

- [ ] **Step 5: Prove the rollback test actually catches the bug**

Temporarily revert the rollback in `Authenticate` (drop the `s.token, s.rec = oldToken, oldRec` line), run `just test ./auth/session/ -run TestAuthenticateRollsBackTokenOnFailedSave`, and confirm it **FAILS**. Restore the line and confirm it passes. A regression test that has never been seen to fail is not a regression test.

- [ ] **Step 6: Commit**

```bash
just fmt ./auth/session/...
just lint
git add auth/session/manager.go auth/session/lifecycle_test.go
git commit -m "feat(session): authenticate, rotate, destroy, elevate, rebind"
```

---

### Task 7: Device management and the reaper service

**Files:**
- Create: `auth/session/devices.go`, `auth/session/reaper.go`
- Test: `auth/session/devices_test.go`

**Interfaces:**
- Consumes: `Manager.index`, `Manager.expirer` (Task 5).
- Produces: `(*Manager).ListByUser(ctx, userID string) ([]Record, error)`, `(*Manager).Revoke(ctx, userID string, sessionID id.UUID) error`, `(*Manager).LogoutOthers(ctx, *Session) error`, `(*Manager).DeleteByUser(ctx, userID string) error`, `(*Manager).DeleteExpired(ctx) (int, error)`, `Reaper(m *Manager, every time.Duration) supervisor.Service`.

- [ ] **Step 1: Write the failing test**

`auth/session/devices_test.go`:

```go
package session_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/core/id"
)

// bareStore implements only Store — no UserIndex, no Expirer.
type bareStore struct{ inner *session.MemoryStore }

func (b bareStore) Load(ctx context.Context, tok string) (session.Record, error) {
	return b.inner.Load(ctx, tok)
}
func (b bareStore) Save(ctx context.Context, tok string, r session.Record) (string, error) {
	return b.inner.Save(ctx, tok, r)
}
func (b bareStore) Delete(ctx context.Context, tok string) error { return b.inner.Delete(ctx, tok) }

func TestMissingCapabilityIsErrUnsupported(t *testing.T) {
	mgr, err := session.New(session.DefaultConfig(),
		session.WithStore(bareStore{session.NewMemoryStore()}),
		session.WithTouch(0),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := mgr.ListByUser(t.Context(), "u1"); !errors.Is(err, session.ErrUnsupported) {
		t.Fatalf("ListByUser = %v, want ErrUnsupported", err)
	}
	if err := mgr.Revoke(t.Context(), "u1", id.NewUUID()); !errors.Is(err, session.ErrUnsupported) {
		t.Fatalf("Revoke = %v, want ErrUnsupported", err)
	}
	if _, err := mgr.DeleteExpired(t.Context()); !errors.Is(err, session.ErrUnsupported) {
		t.Fatalf("DeleteExpired = %v, want ErrUnsupported", err)
	}
}

func TestLogoutOthersKeepsTheCurrentSession(t *testing.T) {
	mgr := newTestManager(t)
	ctx := t.Context()

	current := mgr.Start()
	if err := mgr.Authenticate(ctx, current, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	for range 3 {
		other := mgr.Start()
		if err := mgr.Authenticate(ctx, other, "u1"); err != nil {
			t.Fatalf("Authenticate: %v", err)
		}
	}

	list, err := mgr.ListByUser(ctx, "u1")
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(list) != 4 {
		t.Fatalf("expected 4 sessions before logout-others, got %d", len(list))
	}

	if err := mgr.LogoutOthers(ctx, current); err != nil {
		t.Fatalf("LogoutOthers: %v", err)
	}

	list, err = mgr.ListByUser(ctx, "u1")
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(list) != 1 || list[0].ID != current.ID() {
		t.Fatalf("LogoutOthers left %d sessions, want only the current one", len(list))
	}
	if _, err := mgr.Load(ctx, current.Token()); err != nil {
		t.Fatalf("the current session must still load after LogoutOthers: %v", err)
	}
}

func TestRevokeIsUserBound(t *testing.T) {
	mgr := newTestManager(t)
	ctx := t.Context()

	victim := mgr.Start()
	if err := mgr.Authenticate(ctx, victim, "victim"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// An attacker knows the victim's session id and tries to revoke it as themselves.
	if err := mgr.Revoke(ctx, "attacker", victim.ID()); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := mgr.Load(ctx, victim.Token()); err != nil {
		t.Fatalf("Revoke must refuse a session belonging to another user: %v", err)
	}

	// The owner can revoke it.
	if err := mgr.Revoke(ctx, "victim", victim.ID()); err != nil {
		t.Fatalf("Revoke by owner: %v", err)
	}
	if _, err := mgr.Load(ctx, victim.Token()); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("the owner's revoke must remove the session")
	}
}

func TestReaperServiceName(t *testing.T) {
	mgr := newTestManager(t)
	svc := session.Reaper(mgr, time.Minute)
	if svc.Name() == "" {
		t.Fatal("a supervisor.Service must report a name")
	}
}

func TestReaperStopsOnContextCancel(t *testing.T) {
	mgr := newTestManager(t)
	svc := session.Reaper(mgr, time.Hour)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx) }()

	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v, want nil or context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Reaper did not stop within 2s of cancellation")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./auth/session/ -run TestMissingCapability`
Expected: FAIL — `undefined: mgr.ListByUser`.

- [ ] **Step 3: Write minimal implementation**

`auth/session/devices.go`:

```go
package session

import (
	"context"

	"github.com/dmitrymomot/forge/core/id"
)

// ListByUser returns every live session for userID, newest first. IP, user
// agent, and last-seen are columns, so a device list decodes no payload.
// Returns ErrUnsupported when the store has no user index.
func (m *Manager) ListByUser(ctx context.Context, userID string) ([]Record, error) {
	if m.index == nil {
		return nil, ErrUnsupported
	}
	if userID == "" {
		return nil, ErrAnonymous
	}
	return m.index.ListByUser(ctx, userID)
}

// Revoke removes one of userID's sessions. It is user-bound: passing another
// user's session id is a no-op, not a cross-account logout.
func (m *Manager) Revoke(ctx context.Context, userID string, sessionID id.UUID) error {
	if m.index == nil {
		return ErrUnsupported
	}
	if userID == "" {
		return ErrAnonymous
	}
	return m.index.DeleteOne(ctx, userID, sessionID)
}

// LogoutOthers removes every session for the bound user except this one.
func (m *Manager) LogoutOthers(ctx context.Context, s *Session) error {
	if m.index == nil {
		return ErrUnsupported
	}
	if s == nil {
		return ErrNoSession
	}
	if s.rec.UserID == "" {
		return ErrAnonymous
	}
	return m.index.DeleteByUser(ctx, s.rec.UserID, s.rec.ID)
}

// DeleteByUser removes every session for userID — the GDPR erasure path.
func (m *Manager) DeleteByUser(ctx context.Context, userID string) error {
	if m.index == nil {
		return ErrUnsupported
	}
	if userID == "" {
		return ErrAnonymous
	}
	return m.index.DeleteByUser(ctx, userID)
}

// DeleteExpired reaps expired records. Returns ErrUnsupported for stores whose
// backend expires records natively.
func (m *Manager) DeleteExpired(ctx context.Context) (int, error) {
	if m.expirer == nil {
		return 0, ErrUnsupported
	}
	return m.expirer.DeleteExpired(ctx, m.now())
}
```

`auth/session/reaper.go`:

```go
package session

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/dmitrymomot/forge/ops/supervisor"
)

type reaper struct {
	mgr   *Manager
	every time.Duration
}

// Reaper returns a supervisor.Service that periodically deletes expired
// records. On a store that expires records natively it logs once and exits
// cleanly rather than failing the supervisor.
func Reaper(m *Manager, every time.Duration) supervisor.Service {
	if every <= 0 {
		every = 15 * time.Minute
	}
	return &reaper{mgr: m, every: every}
}

// Name implements supervisor.Service.
func (r *reaper) Name() string { return "session-reaper" }

// Run implements supervisor.Service.
func (r *reaper) Run(ctx context.Context) error {
	if r.mgr.expirer == nil {
		r.mgr.log.InfoContext(ctx, "session: store reaps expired records natively; reaper is a no-op")
		<-ctx.Done()
		return nil
	}

	t := time.NewTicker(r.every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			n, err := r.mgr.DeleteExpired(ctx)
			switch {
			case errors.Is(err, context.Canceled):
				return nil
			case err != nil:
				r.mgr.log.ErrorContext(ctx, "session: reaping expired records failed", slog.Any("error", err))
			case n > 0:
				r.mgr.log.DebugContext(ctx, "session: reaped expired records", slog.Int("count", n))
			}
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `just test ./auth/session/ -race`
Expected: PASS, no data races.

- [ ] **Step 5: Commit**

```bash
just fmt ./auth/session/...
just lint
git add auth/session/devices.go auth/session/reaper.go auth/session/devices_test.go
git commit -m "feat(session): device management and the expired-record reaper service"
```

---

### Task 8: Request context — Info, For, MustFor, LogExtractor

**Files:**
- Create: `auth/session/context.go`
- Test: `auth/session/context_test.go`

**Interfaces:**
- Consumes: `Session` (Task 2), `Manager` (Task 5).
- Produces: `Info` struct with `ID`, `UserID`, `CreatedAt`, `ExpiresAt`, `ElevatedAt`; `(*Info).Authenticated() bool`; `FromContext(ctx) (*Info, bool)`; `(*Manager).For(r *http.Request) (*Session, bool)`; `(*Manager).MustFor(r *http.Request) *Session`; `LogExtractor logger.ContextExtractor`; unexported `withSession(ctx, *Session) context.Context`, `(*Session).info() *Info`, `(*Session).syncInfo()`.

`Info` is carried as a **pointer** and updated in place, so a handler that calls `Authenticate` mid-request does not leave a stale `UserID` behind for everything downstream.

- [ ] **Step 1: Write the failing test**

`auth/session/context_test.go`:

```go
package session_test

import (
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/auth/session"
)

func TestFromContextAbsent(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	if _, ok := session.FromContext(r.Context()); ok {
		t.Fatal("FromContext must report absent when the middleware never ran")
	}
}

func TestForReportsAbsentAndMustForPanics(t *testing.T) {
	mgr := newTestManager(t)
	r := httptest.NewRequest("GET", "/", nil)

	if _, ok := mgr.For(r); ok {
		t.Fatal("For must report ok=false when the middleware is not mounted")
	}

	defer func() {
		if recover() == nil {
			t.Fatal("MustFor must panic when the middleware is not mounted")
		}
	}()
	_ = mgr.MustFor(r)
}

func TestInfoTracksMidRequestAuthenticate(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()

	ctx := session.TestWithSession(t.Context(), sess)
	info, ok := session.FromContext(ctx)
	if !ok {
		t.Fatal("FromContext must find the session the middleware stored")
	}
	if info.Authenticated() {
		t.Fatal("a fresh session must not report Authenticated")
	}

	if err := mgr.Authenticate(ctx, sess, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	if info.UserID != "u1" {
		t.Fatalf("Info.UserID = %q after mid-request Authenticate, want u1 — Info must not go stale", info.UserID)
	}
	if !info.Authenticated() {
		t.Fatal("Info must report Authenticated after Authenticate")
	}
}
```

`TestWithSession` is an exported test hook so black-box tests can build a context without the middleware. Add it to `context.go` guarded by a doc comment marking it as test-only:

```go
// TestWithSession stores s in ctx exactly as Middleware does. It exists for
// tests in other packages that need a session-bearing context without an HTTP
// server; production code should never call it.
func TestWithSession(ctx context.Context, s *Session) context.Context { return withSession(ctx, s) }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./auth/session/ -run TestFromContext`
Expected: FAIL — `undefined: session.FromContext`.

- [ ] **Step 3: Write minimal implementation**

`auth/session/context.go`:

```go
package session

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/ops/logger"
)

// Info is the small, always-available view of the current session. It is
// carried by pointer and updated in place, so a handler that authenticates
// mid-request does not leave a stale UserID for the rest of the chain. The
// payload is deliberately absent: reaching it requires the Manager.
type Info struct {
	CreatedAt  time.Time
	ExpiresAt  time.Time
	ElevatedAt time.Time
	UserID     string
	ID         id.UUID
}

// Authenticated reports whether a principal is bound.
func (i *Info) Authenticated() bool { return i != nil && i.UserID != "" }

var (
	sessionKey = ctxkey.New[*Session]("session")
	infoKey    = ctxkey.New[*Info]("session.info")
)

// FromContext returns the Info stored by Middleware.
func FromContext(ctx context.Context) (*Info, bool) { return infoKey.From(ctx) }

// For returns the session Middleware loaded for r. ok is false when the
// middleware is not mounted on this route.
func (m *Manager) For(r *http.Request) (*Session, bool) {
	if r == nil {
		return nil, false
	}
	return sessionKey.From(r.Context())
}

// MustFor returns the session or panics — for handlers whose routes carry the
// middleware, where its absence is a wiring bug.
func (m *Manager) MustFor(r *http.Request) *Session {
	s, ok := m.For(r)
	if !ok {
		panic("session: no session in context — is session.Middleware mounted on this route?")
	}
	return s
}

// fromContext returns the session for adapters that only have a context.
func fromContext(ctx context.Context) (*Session, bool) { return sessionKey.From(ctx) }

func withSession(ctx context.Context, s *Session) context.Context {
	s.inf = &Info{
		ID:         s.rec.ID,
		UserID:     s.rec.UserID,
		CreatedAt:  s.rec.CreatedAt,
		ExpiresAt:  s.rec.ExpiresAt,
		ElevatedAt: s.rec.ElevatedAt,
	}
	return infoKey.With(sessionKey.With(ctx, s), s.inf)
}

// LogExtractor adds a "session" group with the session id, and the user id when
// one is bound. Wire it with logger.WithContextExtractors(session.LogExtractor).
var LogExtractor logger.ContextExtractor = func(ctx context.Context) (slog.Attr, bool) {
	inf, ok := infoKey.From(ctx)
	if !ok || inf.ID.IsZero() {
		return slog.Attr{}, false
	}
	attrs := []any{slog.String("id", inf.ID.String())}
	if inf.UserID != "" {
		attrs = append(attrs, slog.String("user", inf.UserID))
	}
	return slog.Group("session", attrs...), true
}
```

Add the back-reference and the sync call so `Info` never goes stale. In `session.go`:

```go
// in Session
inf *Info

// syncInfo mirrors the record onto the context-visible Info, if one exists.
func (s *Session) syncInfo() {
	if s.inf == nil {
		return
	}
	s.inf.ID = s.rec.ID
	s.inf.UserID = s.rec.UserID
	s.inf.CreatedAt = s.rec.CreatedAt
	s.inf.ExpiresAt = s.rec.ExpiresAt
	s.inf.ElevatedAt = s.rec.ElevatedAt
}
```

Call `s.syncInfo()` at the end of `Manager.Save` (after the successful store write) and at the end of `Manager.Rebind`, and in `Manager.Authenticate` **after** the save succeeds — never before, or a rolled-back authenticate would leave the Info claiming a user.

- [ ] **Step 4: Run tests to verify they pass**

Run: `just test ./auth/session/`
Expected: PASS — including the mid-request staleness test.

- [ ] **Step 5: Commit**

```bash
just fmt ./auth/session/...
just lint
git add auth/session/context.go auth/session/session.go auth/session/manager.go auth/session/context_test.go
git commit -m "feat(session): request context with a live Info view, For/MustFor, log extractor"
```

---

### Task 9: Transport and Policy seams

**Files:**
- Create: `auth/session/transport.go`, `auth/session/policy.go`
- Test: `auth/session/policy_test.go`

**Interfaces:**
- Consumes: `Session` (Task 2).
- Produces: `Transport` interface (`Extract`, `Embed`, `Clear`); `Policy func(ctx, *http.Request, *Session) error`; `Deny(reason string) error`; `Revoke(reason string) error`; `DenyError`/`RevokeError` types with `Reason() string`; `IsDeny(error) (string, bool)`; `IsRevoke(error) (string, bool)`.

- [ ] **Step 1: Write the failing test**

`auth/session/policy_test.go`:

```go
package session_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/dmitrymomot/forge/auth/session"
)

func TestDenyAndRevokeCarryReasons(t *testing.T) {
	deny := session.Deny("outside business hours")
	reason, ok := session.IsDeny(deny)
	if !ok {
		t.Fatal("IsDeny must recognize a Deny")
	}
	if reason != "outside business hours" {
		t.Fatalf("reason = %q, want %q", reason, "outside business hours")
	}
	if _, ok := session.IsRevoke(deny); ok {
		t.Fatal("a Deny must not be mistaken for a Revoke — the record survives a Deny")
	}

	revoke := session.Revoke("fingerprint drift")
	reason, ok = session.IsRevoke(revoke)
	if !ok {
		t.Fatal("IsRevoke must recognize a Revoke")
	}
	if reason != "fingerprint drift" {
		t.Fatalf("reason = %q, want %q", reason, "fingerprint drift")
	}
}

func TestDenyAndRevokeSurviveWrapping(t *testing.T) {
	wrapped := fmt.Errorf("policy chain: %w", session.Revoke("stolen"))
	if reason, ok := session.IsRevoke(wrapped); !ok || reason != "stolen" {
		t.Fatalf("IsRevoke through a wrap = (%q, %v), want (stolen, true)", reason, ok)
	}
}

func TestPlainErrorIsNeitherDenyNorRevoke(t *testing.T) {
	err := errors.New("database unreachable")
	if _, ok := session.IsDeny(err); ok {
		t.Fatal("an infrastructure error must not be treated as a Deny")
	}
	if _, ok := session.IsRevoke(err); ok {
		t.Fatal("an infrastructure error must not be treated as a Revoke")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./auth/session/ -run TestDenyAndRevoke`
Expected: FAIL — `undefined: session.Deny`.

- [ ] **Step 3: Write minimal implementation**

`auth/session/transport.go`:

```go
package session

import "net/http"

// Transport moves the session credential between the server and the client.
// Implementations live outside this package: if a transport cannot be written
// against this interface alone, the interface is wrong.
type Transport interface {
	// Extract finds the credential on the request.
	Extract(r *http.Request) (token string, ok bool)
	// Embed writes the credential to the response. It sets headers only — it
	// must never write a status or a body, because commit runs inside the
	// handler's WriteHeader and would override the handler's own response.
	// A transport that can read but not write returns ErrNoEmbed.
	Embed(w http.ResponseWriter, r *http.Request, s *Session) error
	// Clear removes the credential from the client.
	Clear(w http.ResponseWriter, r *http.Request)
}
```

`auth/session/policy.go`:

```go
package session

import (
	"context"
	"errors"
	"net/http"
)

// Policy inspects a loaded session against the current request. Policies run in
// the order they are registered and short-circuit on the first non-nil return.
//
// Returning nil continues; Deny answers 401 and leaves the record intact;
// Revoke deletes the record and answers 401; any other error fails closed with
// a 500. All policies are request-time, which is why the Manager carries no
// hook machinery at all.
type Policy func(ctx context.Context, r *http.Request, s *Session) error

// DenyError rejects the request without destroying the session.
type DenyError struct{ reason string }

// Error implements error.
func (e *DenyError) Error() string { return "session: denied: " + e.reason }

// Reason returns the human-readable cause, surfaced to logs and the responder.
func (e *DenyError) Reason() string { return e.reason }

// RevokeError rejects the request and destroys the session.
type RevokeError struct{ reason string }

// Error implements error.
func (e *RevokeError) Error() string { return "session: revoked: " + e.reason }

// Reason returns the human-readable cause, surfaced to logs and the responder.
func (e *RevokeError) Reason() string { return e.reason }

// Deny builds the "reject, keep the record" outcome.
func Deny(reason string) error { return &DenyError{reason: reason} }

// Revoke builds the "reject and destroy the record" outcome.
func Revoke(reason string) error { return &RevokeError{reason: reason} }

// IsDeny reports whether err is a Deny and returns its reason.
func IsDeny(err error) (string, bool) {
	var d *DenyError
	if errors.As(err, &d) {
		return d.reason, true
	}
	return "", false
}

// IsRevoke reports whether err is a Revoke and returns its reason.
func IsRevoke(err error) (string, bool) {
	var r *RevokeError
	if errors.As(err, &r) {
		return r.reason, true
	}
	return "", false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `just test ./auth/session/ -run 'TestDeny|TestPlainError'`
Expected: PASS — 3 tests.

- [ ] **Step 5: Commit**

```bash
just fmt ./auth/session/...
just lint
git add auth/session/transport.go auth/session/policy.go auth/session/policy_test.go
git commit -m "feat(session): transport and policy seams with deny/revoke outcomes"
```

---

### Task 10: The commit writer

**Files:**
- Create: `auth/session/writer.go`
- Test: `auth/session/writer_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: unexported `commitWriter` with `newCommitWriter(w http.ResponseWriter, commit func() error) *commitWriter`, `(*commitWriter).WriteHeader(int)`, `.Write([]byte) (int, error)`, `.Unwrap() http.ResponseWriter`, `.Hijack()`, `.FlushError() error`, `.ensureCommitted() error`, `.committed bool`.

- [ ] **Step 1: Write the failing test**

`auth/session/writer_test.go`:

```go
package session_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCommitRunsBeforeRedirectHeadersGoOut(t *testing.T) {
	mgr := newTestManager(t)
	mw := testMiddleware(t, mgr)

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := mgr.MustFor(r)
		nsCart.Set(sess, cartData{Items: []string{"x"}})
		http.Redirect(w, r, "/app", http.StatusSeeOther)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/login", nil))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if rec.Header().Get("Location") != "/app" {
		t.Fatalf("Location = %q, want /app", rec.Header().Get("Location"))
	}
	if got := rec.Header().Get("X-Test-Token"); got == "" {
		t.Fatal("the credential must be embedded on a 303 — a defer-based commit would silently lose it")
	}
}

func TestCommitFailureBecomes500AndSuppressesTheBody(t *testing.T) {
	mgr := newTestManager(t)
	mw := testMiddlewareWithCommitError(t, mgr, errors.New("store down"))

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := mgr.MustFor(r)
		nsCart.Set(sess, cartData{Items: []string{"x"}})
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("sensitive body"))
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 — a failed commit must never look like success", rec.Code)
	}
	if rec.Body.String() == "sensitive body" {
		t.Fatal("the handler's body must not be written after the commit failed")
	}
}

func TestFlushCommitsFirst(t *testing.T) {
	mgr := newTestManager(t)
	mw := testMiddleware(t, mgr)

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := mgr.MustFor(r)
		nsCart.Set(sess, cartData{Items: []string{"x"}})
		// An SSE handler flushes before writing anything else.
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("Flush: %v", err)
		}
		_, _ = w.Write([]byte("data: hello\n\n"))
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/events", nil))

	if rec.Header().Get("X-Test-Token") == "" {
		t.Fatal("a Flush before the first Write must still commit — ResponseController bypasses WriteHeader")
	}
}
```

`testMiddleware` and `testMiddlewareWithCommitError` are helpers built on Task 11's `Middleware` plus a stub transport that writes `X-Test-Token`. Define them at the bottom of `writer_test.go`:

```go
type headerTransport struct{ embedErr error }

func (h headerTransport) Extract(r *http.Request) (string, bool) {
	tok := r.Header.Get("X-Test-Token")
	return tok, tok != ""
}

func (h headerTransport) Embed(w http.ResponseWriter, _ *http.Request, s *session.Session) error {
	if h.embedErr != nil {
		return h.embedErr
	}
	w.Header().Set("X-Test-Token", s.Token())
	return nil
}

func (h headerTransport) Clear(w http.ResponseWriter, _ *http.Request) {
	w.Header().Del("X-Test-Token")
}

func testMiddleware(t *testing.T, mgr *session.Manager) middleware.Middleware {
	t.Helper()
	return session.Middleware(mgr, session.WithTransport(headerTransport{}))
}

func testMiddlewareWithCommitError(t *testing.T, mgr *session.Manager, err error) middleware.Middleware {
	t.Helper()
	return session.Middleware(mgr, session.WithTransport(headerTransport{embedErr: err}))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./auth/session/ -run TestCommit`
Expected: FAIL — `undefined: session.Middleware`, `undefined: session.WithTransport`.

- [ ] **Step 3: Write minimal implementation**

`auth/session/writer.go`:

```go
package session

import (
	"bufio"
	"errors"
	"net"
	"net/http"
)

// commitWriter runs the session commit exactly once, immediately before the
// first byte reaches the real writer. Headers are still open at that moment,
// so a failed commit becomes a clean 500 with nothing leaked.
//
// It re-declares Hijack and FlushError on purpose. http.ResponseController
// walks the Unwrap chain, so a websocket upgrade or an SSE flush would
// otherwise reach the real writer without ever passing through WriteHeader,
// skipping the commit entirely.
type commitWriter struct {
	http.ResponseWriter
	commit    func() error
	committed bool
	failed    bool
}

func newCommitWriter(w http.ResponseWriter, commit func() error) *commitWriter {
	return &commitWriter{ResponseWriter: w, commit: commit}
}

// ensureCommitted runs the commit once. It reports whether writing may proceed.
func (w *commitWriter) ensureCommitted() error {
	if w.committed {
		if w.failed {
			return errors.New("session: commit already failed")
		}
		return nil
	}
	w.committed = true
	if err := w.commit(); err != nil {
		w.failed = true
		return err
	}
	return nil
}

// WriteHeader commits before the status line goes out.
func (w *commitWriter) WriteHeader(code int) {
	if !w.committed {
		if err := w.ensureCommitted(); err != nil {
			clearHeaders(w.ResponseWriter.Header())
			w.ResponseWriter.WriteHeader(http.StatusInternalServerError)
			return
		}
	}
	if w.failed {
		return
	}
	w.ResponseWriter.WriteHeader(code)
}

// Write commits on an implicit 200 and swallows the body once a commit failed.
func (w *commitWriter) Write(b []byte) (int, error) {
	if !w.committed {
		w.WriteHeader(http.StatusOK)
	}
	if w.failed {
		// Report success so the handler proceeds normally; the client already
		// received a 500 and must not also receive the handler's payload.
		return len(b), nil
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap exposes the wrapped writer to http.ResponseController.
func (w *commitWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Hijack commits before the connection leaves the HTTP stack.
func (w *commitWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if err := w.ensureCommitted(); err != nil {
		return nil, nil, err
	}
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

// FlushError commits before the first flush, which ResponseController would
// otherwise route straight past WriteHeader.
func (w *commitWriter) FlushError() error {
	if !w.committed {
		if err := w.ensureCommitted(); err != nil {
			clearHeaders(w.ResponseWriter.Header())
			w.ResponseWriter.WriteHeader(http.StatusInternalServerError)
			return err
		}
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func clearHeaders(h http.Header) {
	for k := range h {
		h.Del(k)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `just test ./auth/session/ -run TestCommit` — after Task 11 lands `Middleware`. If Task 11 is not yet done, run `go build ./auth/session/` and confirm the package compiles; the three writer tests go green at the end of Task 11.

- [ ] **Step 5: Prove the Flush test catches the bug**

Temporarily delete the `FlushError` method, run `just test ./auth/session/ -run TestFlushCommitsFirst`, and confirm it **FAILS** with a missing `X-Test-Token`. Restore it. This is the sharp edge the spec calls out; a test that has never failed does not protect it.

- [ ] **Step 6: Commit**

```bash
just fmt ./auth/session/...
git add auth/session/writer.go auth/session/writer_test.go
git commit -m "feat(session): commit-at-first-byte response writer with Hijack and Flush interception"
```

---

### Task 11: Middleware

**Files:**
- Create: `auth/session/middleware.go`
- Test: `auth/session/middleware_test.go`

**Interfaces:**
- Consumes: `Manager.Load/Save/Start/Destroy` (Tasks 5–6), `withSession` (Task 8), `Transport`/`Policy`/`IsDeny`/`IsRevoke` (Task 9), `newCommitWriter` (Task 10).
- Produces: `MiddlewareOption`, `WithTransport(...Transport) MiddlewareOption`, `WithPolicy(...Policy) MiddlewareOption`, `WithResponder(problem.Responder) MiddlewareOption`, `WithMiddlewareLogger(*slog.Logger) MiddlewareOption`, `WithClientInfo(func(*http.Request) Bind) MiddlewareOption`, `Middleware(m *Manager, opts ...MiddlewareOption) middleware.Middleware`.

`WithClientInfo` is the seam that populates the pinned `IP`/`UserAgent`/`Fingerprint` columns. Session must not extract client IPs (`web/clientip` owns that) or compute fingerprints (`web/fingerprint` owns that), so it takes a function instead. It runs **only for a session that has never been persisted** — pinning is at creation, and a per-request refresh would let a stolen credential overwrite the binding on its first use.

- [ ] **Step 1: Write the failing test**

`auth/session/middleware_test.go`:

```go
package session_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/auth/session"
)

func TestAnonymousRequestCostsNoStorage(t *testing.T) {
	store := session.NewMemoryStore()
	mgr, err := session.New(session.DefaultConfig(), session.WithStore(store))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mw := session.Middleware(mgr, session.WithTransport(headerTransport{}))

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := mgr.For(r); !ok {
			t.Error("an anonymous visitor must still get a session object")
		}
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if got := rec.Header().Get("X-Test-Token"); got != "" {
		t.Fatal("a request that wrote nothing must not mint a credential")
	}
}

func TestPolicyDenyIs401AndKeepsTheRecord(t *testing.T) {
	mgr := newTestManager(t)
	seed := mgr.Start()
	if err := mgr.Save(t.Context(), seed); err != nil {
		t.Fatalf("Save: %v", err)
	}

	denied := func(context.Context, *http.Request, *session.Session) error {
		return session.Deny("nope")
	}
	mw := session.Middleware(mgr,
		session.WithTransport(headerTransport{}),
		session.WithPolicy(denied),
	)

	reached := false
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Test-Token", seed.Token())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if reached {
		t.Fatal("a denied request must not reach the handler")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if _, err := mgr.Load(t.Context(), seed.Token()); err != nil {
		t.Fatalf("Deny must leave the record intact: %v", err)
	}
}

func TestPolicyRevokeDeletesTheRecord(t *testing.T) {
	mgr := newTestManager(t)
	seed := mgr.Start()
	if err := mgr.Save(t.Context(), seed); err != nil {
		t.Fatalf("Save: %v", err)
	}

	revoked := func(context.Context, *http.Request, *session.Session) error {
		return session.Revoke("stolen")
	}
	mw := session.Middleware(mgr,
		session.WithTransport(headerTransport{}),
		session.WithPolicy(revoked),
	)
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Test-Token", seed.Token())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if _, err := mgr.Load(t.Context(), seed.Token()); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("Revoke must delete the record")
	}
}

func TestPolicyInfraErrorIs500NotAnonymous(t *testing.T) {
	mgr := newTestManager(t)
	seed := mgr.Start()
	if err := mgr.Save(t.Context(), seed); err != nil {
		t.Fatalf("Save: %v", err)
	}

	broken := func(context.Context, *http.Request, *session.Session) error {
		return errors.New("policy backend unreachable")
	}
	mw := session.Middleware(mgr,
		session.WithTransport(headerTransport{}),
		session.WithPolicy(broken),
	)

	reached := false
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Test-Token", seed.Token())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if reached {
		t.Fatal("an infrastructure failure must never degrade to an anonymous request")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestExpiredCredentialStartsAFreshAnonymousSession(t *testing.T) {
	mgr := newTestManager(t)
	mw := session.Middleware(mgr, session.WithTransport(headerTransport{}))

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := mgr.MustFor(r)
		if sess.UserID() != "" {
			t.Error("an unknown credential must not resolve to anyone")
		}
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Test-Token", "a-token-that-was-never-issued")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — an unknown credential is anonymous, not an error", rec.Code)
	}
}

func TestNoEmbedFallsThroughToTheNextTransport(t *testing.T) {
	mgr := newTestManager(t)
	mw := session.Middleware(mgr, session.WithTransport(
		readOnlyTransport{},   // matches, cannot embed
		headerTransport{},     // embeds
	))

	seed := mgr.Start()
	if err := mgr.Save(t.Context(), seed); err != nil {
		t.Fatalf("Save: %v", err)
	}

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := mgr.MustFor(r)
		if err := mgr.Authenticate(r.Context(), sess, "u1"); err != nil {
			t.Errorf("Authenticate: %v", err)
		}
		http.Redirect(w, r, "/app", http.StatusSeeOther)
	}))

	r := httptest.NewRequest("GET", "/magic?t="+seed.Token(), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Header().Get("X-Test-Token") == "" {
		t.Fatal("a transport returning ErrNoEmbed must hand off to the first transport that can embed")
	}
}

func TestClientInfoIsPinnedAtCreationOnly(t *testing.T) {
	mgr := newTestManager(t)
	calls := 0
	mw := session.Middleware(mgr,
		session.WithTransport(headerTransport{}),
		session.WithClientInfo(func(r *http.Request) session.Bind {
			calls++
			return session.Bind{IP: r.Header.Get("X-Real-IP"), UserAgent: r.UserAgent(), Fingerprint: "fp-" + r.Header.Get("X-Real-IP")}
		}),
	)

	var token string
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := mgr.MustFor(r)
		nsCart.Set(sess, cartData{Items: []string{"x"}})
		w.WriteHeader(http.StatusOK)
		token = sess.Token()
	}))

	first := httptest.NewRequest("GET", "/", nil)
	first.Header.Set("X-Real-IP", "203.0.113.4")
	first.Header.Set("User-Agent", "Chrome")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, first)

	saved, err := mgr.Load(t.Context(), token)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if saved.IP() != "203.0.113.4" || saved.UserAgent() != "Chrome" || saved.Fingerprint() != "fp-203.0.113.4" {
		t.Fatalf("client info not pinned: ip=%q ua=%q fp=%q", saved.IP(), saved.UserAgent(), saved.Fingerprint())
	}

	// A later request from a different address must NOT overwrite the pin,
	// or a stolen credential would rebind to the attacker on first use.
	second := httptest.NewRequest("GET", "/", nil)
	second.Header.Set("X-Test-Token", token)
	second.Header.Set("X-Real-IP", "198.51.100.9")
	second.Header.Set("User-Agent", "Firefox")
	h.ServeHTTP(httptest.NewRecorder(), second)

	after, err := mgr.Load(t.Context(), token)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if after.IP() != "203.0.113.4" || after.UserAgent() != "Chrome" {
		t.Fatalf("pinned metadata was overwritten on a later request: ip=%q ua=%q", after.IP(), after.UserAgent())
	}
}

// readOnlyTransport reads a query token and cannot write one back.
type readOnlyTransport struct{}

func (readOnlyTransport) Extract(r *http.Request) (string, bool) {
	tok := r.URL.Query().Get("t")
	return tok, tok != ""
}
func (readOnlyTransport) Embed(http.ResponseWriter, *http.Request, *session.Session) error {
	return session.ErrNoEmbed
}
func (readOnlyTransport) Clear(http.ResponseWriter, *http.Request) {}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./auth/session/ -run TestAnonymousRequest`
Expected: FAIL — `undefined: session.Middleware`.

- [ ] **Step 3: Write minimal implementation**

`auth/session/middleware.go`:

```go
package session

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/dmitrymomot/forge/web/middleware"
	"github.com/dmitrymomot/forge/web/problem"
)

type mwOptions struct {
	transports []Transport
	policies   []Policy
	responder  problem.Responder
	logger     *slog.Logger
	clientInfo func(*http.Request) Bind
}

// MiddlewareOption configures Middleware.
type MiddlewareOption func(*mwOptions)

// WithTransport registers transports. Extraction tries them in order;
// embedding uses whichever matched, or the first that supports it.
func WithTransport(ts ...Transport) MiddlewareOption {
	return func(o *mwOptions) { o.transports = append(o.transports, ts...) }
}

// WithPolicy registers request-time policies, run in order, short-circuiting.
func WithPolicy(ps ...Policy) MiddlewareOption {
	return func(o *mwOptions) { o.policies = append(o.policies, ps...) }
}

// WithResponder overrides the rejection response. Default: problem.JSON 401.
func WithResponder(r problem.Responder) MiddlewareOption {
	return func(o *mwOptions) { o.responder = r }
}

// WithMiddlewareLogger sets the middleware's logger.
func WithMiddlewareLogger(l *slog.Logger) MiddlewareOption {
	return func(o *mwOptions) { o.logger = l }
}

// WithClientInfo supplies the device metadata pinned when a session is first
// created. Session does not extract client IPs or compute fingerprints —
// web/clientip and web/fingerprint do — so it takes a function.
//
// It runs only for a session that has never been persisted. Refreshing these
// columns per request would not weaken binding, it would delete it: a stolen
// credential's first request would rewrite the row with the attacker's address
// and fingerprint, and every later request would match. Use Manager.Rebind for
// the deliberate re-pin after a successful re-authentication.
func WithClientInfo(fn func(*http.Request) Bind) MiddlewareOption {
	return func(o *mwOptions) { o.clientInfo = fn }
}

// Middleware loads the session, runs policies, exposes it on the context, and
// commits exactly once before the first byte of the response.
//
// A visitor with no credential gets a fresh anonymous session and costs no
// storage: the row is minted only if the handler writes something. An unknown
// or expired credential is treated the same way. An infrastructure failure is
// never degraded to an anonymous request.
func Middleware(m *Manager, opts ...MiddlewareOption) middleware.Middleware {
	o := mwOptions{
		responder: problem.JSON(problem.WithStatus(http.StatusUnauthorized)),
		logger:    m.log,
	}
	for _, opt := range opts {
		opt(&o)
	}
	if len(o.transports) == 0 {
		panic("session: Middleware requires at least one WithTransport")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess, matched, err := load(m, &o, r)
			if err != nil {
				o.logger.ErrorContext(r.Context(), "session: load failed", slog.Any("error", err))
				problem.JSON(problem.WithStatus(http.StatusInternalServerError))(w, r, err)
				return
			}
			if o.clientInfo != nil && sess.isNew {
				b := o.clientInfo(r)
				sess.rec.IP, sess.rec.UserAgent, sess.rec.Fingerprint = b.IP, b.UserAgent, b.Fingerprint
			}

			ctx := withSession(r.Context(), sess)
			r = r.WithContext(ctx)

			if err := runPolicies(m, &o, w, r, sess, matched); err != nil {
				return // runPolicies already answered
			}

			cw := newCommitWriter(w, func() error { return commit(m, &o, w, r, sess, matched) })
			next.ServeHTTP(cw, r)

			if !cw.committed {
				if err := cw.ensureCommitted(); err != nil {
					o.logger.ErrorContext(ctx, "session: commit failed after handler", slog.Any("error", err))
				}
			}
		})
	}
}

// load resolves the session for r, returning the transport that matched.
func load(m *Manager, o *mwOptions, r *http.Request) (*Session, Transport, error) {
	for _, t := range o.transports {
		token, ok := t.Extract(r)
		if !ok {
			continue
		}
		sess, err := m.Load(r.Context(), token)
		switch {
		case err == nil:
			return sess, t, nil
		case errors.Is(err, ErrNotFound), errors.Is(err, ErrExpired):
			// A credential we cannot honor is anonymous, not an error.
			return m.Start(), t, nil
		default:
			return nil, nil, err
		}
	}
	return m.Start(), nil, nil
}

func runPolicies(m *Manager, o *mwOptions, w http.ResponseWriter, r *http.Request, s *Session, matched Transport) error {
	for _, p := range o.policies {
		err := p(r.Context(), r, s)
		if err == nil {
			continue
		}
		reason, revoke := IsRevoke(err)
		if revoke {
			if delErr := m.Destroy(r.Context(), s); delErr != nil {
				o.logger.ErrorContext(r.Context(), "session: revoke could not delete the record",
					slog.String("reason", reason), slog.Any("error", delErr))
			}
			clearCredential(o, w, r, matched)
			o.logger.InfoContext(r.Context(), "session: revoked", slog.String("reason", reason))
			o.responder(w, r, err)
			return err
		}
		if reason, deny := IsDeny(err); deny {
			o.logger.InfoContext(r.Context(), "session: denied", slog.String("reason", reason))
			o.responder(w, r, err)
			return err
		}
		// Anything else is infrastructure: fail closed.
		o.logger.ErrorContext(r.Context(), "session: policy failed", slog.Any("error", err))
		problem.JSON(problem.WithStatus(http.StatusInternalServerError))(w, r, err)
		return err
	}
	return nil
}

// commit persists the session and writes the credential. It runs at most once,
// from the commit writer, while the response headers are still open.
func commit(m *Manager, o *mwOptions, w http.ResponseWriter, r *http.Request, s *Session, matched Transport) error {
	if s.deleted {
		clearCredential(o, w, r, matched)
		return nil
	}

	switch {
	case s.isDirty() || s.isNew:
		if s.isNew && !s.isDirty() && !s.Authenticated() {
			// Nothing was written and nobody logged in: mint no row, no credential.
			return nil
		}
		if err := m.Save(r.Context(), s); err != nil {
			return err
		}
	case m.touchDue(s, m.now()):
		// Metadata-only refresh. Fail-open: a failed touch must not fail the request.
		if err := m.toucher.Touch(r.Context(), s.token, s.rec.LastSeenAt, s.rec.ExpiresAt); err != nil {
			o.logger.WarnContext(r.Context(), "session: touch failed", slog.Any("error", err))
		}
		return nil
	default:
		return nil
	}

	return embed(o, w, r, s, matched)
}

// embed writes the credential using the matched transport, falling through to
// the first transport that supports embedding when the matched one cannot.
func embed(o *mwOptions, w http.ResponseWriter, r *http.Request, s *Session, matched Transport) error {
	if matched != nil {
		err := matched.Embed(w, r, s)
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrNoEmbed) {
			return err
		}
	}
	for _, t := range o.transports {
		if t == matched {
			continue
		}
		err := t.Embed(w, r, s)
		if errors.Is(err, ErrNoEmbed) {
			continue
		}
		return err
	}
	return ErrNoEmbed
}

func clearCredential(o *mwOptions, w http.ResponseWriter, r *http.Request, matched Transport) {
	if matched != nil {
		matched.Clear(w, r)
		return
	}
	for _, t := range o.transports {
		t.Clear(w, r)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `just test ./auth/session/ -race`
Expected: PASS — all middleware tests plus the three writer tests from Task 10.

- [ ] **Step 5: Commit**

```bash
just fmt ./auth/session/...
just lint
git add auth/session/middleware.go auth/session/middleware_test.go
git commit -m "feat(session): autoload middleware with policy outcomes and single-commit lifecycle"
```

---

### Task 12: guard adapter

**Files:**
- Create: `auth/session/guard.go`
- Test: `auth/session/guard_test.go`

**Interfaces:**
- Consumes: `fromContext` (Task 8), `Session` (Task 2).
- Produces: `Extractor() guard.Extractor`, `VerifierOption`, `WithIdentity(func(*Session) guard.Identity) VerifierOption`, `Verifier(m *Manager, opts ...VerifierOption) guard.Verifier`.

- [ ] **Step 1: Write the failing test**

`auth/session/guard_test.go`:

```go
package session_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/auth/guard"
	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/web/middleware"
)

func TestGuardGatesOnTheAlreadyLoadedSession(t *testing.T) {
	mgr := newTestManager(t)
	authed := mgr.Start()
	if err := mgr.Authenticate(t.Context(), authed, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	chain := middleware.Chain(
		session.Middleware(mgr, session.WithTransport(headerTransport{})),
		guard.New(session.Verifier(mgr), guard.WithExtractors(session.Extractor())),
	)

	var got guard.Identity
	h := chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = guard.MustFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Test-Token", authed.Token())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got.Subject != "u1" {
		t.Fatalf("Identity.Subject = %q, want u1", got.Subject)
	}
	if got.Method != guard.MethodSession {
		t.Fatalf("Identity.Method = %q, want %q", got.Method, guard.MethodSession)
	}
}

func TestGuard401sAnAnonymousSession(t *testing.T) {
	mgr := newTestManager(t)
	chain := middleware.Chain(
		session.Middleware(mgr, session.WithTransport(headerTransport{})),
		guard.New(session.Verifier(mgr), guard.WithExtractors(session.Extractor())),
	)

	reached := false
	h := chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if reached {
		t.Fatal("an anonymous session must not pass the guard")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestWithIdentityCarriesRoles(t *testing.T) {
	mgr := newTestManager(t)
	authed := mgr.Start()
	if err := mgr.Authenticate(t.Context(), authed, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	nsRoles.Set(authed, rolesData{Roles: []string{"admin"}})
	if err := mgr.Save(t.Context(), authed); err != nil {
		t.Fatalf("Save: %v", err)
	}

	verifier := session.Verifier(mgr, session.WithIdentity(func(s *session.Session) guard.Identity {
		roles, _ := nsRoles.Get(s)
		return guard.Identity{Subject: s.UserID(), Roles: roles.Roles, Method: guard.MethodSession}
	}))

	chain := middleware.Chain(
		session.Middleware(mgr, session.WithTransport(headerTransport{})),
		guard.New(verifier, guard.WithExtractors(session.Extractor())),
	)

	var got guard.Identity
	h := chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = guard.MustFrom(r.Context())
	}))

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Test-Token", authed.Token())
	h.ServeHTTP(httptest.NewRecorder(), r)

	if len(got.Roles) != 1 || got.Roles[0] != "admin" {
		t.Fatalf("Identity.Roles = %v, want [admin] — WithIdentity must reach the payload", got.Roles)
	}
}

type rolesData struct {
	Roles []string `json:"roles"`
}

var nsRoles = session.NewNamespace[rolesData]("test.roles")
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./auth/session/ -run TestGuard`
Expected: FAIL — `undefined: session.Verifier`.

- [ ] **Step 3: Write minimal implementation**

`auth/session/guard.go`:

```go
package session

import (
	"context"
	"net/http"

	"github.com/dmitrymomot/forge/auth/guard"
)

// Extractor reports an authenticated session to guard. It returns the session
// id rather than the credential: the session was already loaded and validated
// by Middleware, so pairing it with Verifier costs no second store read and
// never repeats a transport's cookie or header name.
func Extractor() guard.Extractor {
	return func(r *http.Request) (string, bool) {
		s, ok := fromContext(r.Context())
		if !ok || !s.Authenticated() {
			return "", false
		}
		return s.rec.ID.String(), true
	}
}

type verifierOptions struct {
	identity func(*Session) guard.Identity
}

// VerifierOption configures Verifier.
type VerifierOption func(*verifierOptions)

// WithIdentity maps a session to a guard.Identity, letting roles or scopes ride
// a session namespace instead of a per-request store read. The default emits
// Subject and Method only — session core knows nothing about roles.
func WithIdentity(fn func(*Session) guard.Identity) VerifierOption {
	return func(o *verifierOptions) { o.identity = fn }
}

// Verifier adapts the session loaded by Middleware into a guard.Verifier, so
// guard owns authentication gating and session stays out of it. Mount it with
// Extractor; the credential argument is ignored because the session in context
// is already validated.
func Verifier(m *Manager, opts ...VerifierOption) guard.Verifier {
	var o verifierOptions
	for _, opt := range opts {
		opt(&o)
	}
	return guard.VerifierFunc(func(ctx context.Context, _ string) (guard.Identity, error) {
		s, ok := fromContext(ctx)
		if !ok {
			return guard.Identity{}, ErrNoSession
		}
		if !s.Authenticated() {
			return guard.Identity{}, ErrAnonymous
		}
		if o.identity != nil {
			return o.identity(s), nil
		}
		return guard.Identity{Subject: s.rec.UserID, Method: guard.MethodSession}, nil
	})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `just test ./auth/session/ -run TestGuard -v`
Expected: PASS — 3 tests.

- [ ] **Step 5: Commit**

```bash
just fmt ./auth/session/...
just lint
git add auth/session/guard.go auth/session/guard_test.go
git commit -m "feat(session): guard adapter so authentication gating stays in auth/guard"
```

---

### Task 13: RequireElevation decider

**Files:**
- Create: `auth/session/elevation.go`
- Test: `auth/session/elevation_test.go`

**Interfaces:**
- Consumes: `FromContext` (Task 8).
- Produces: `RequireElevation(window time.Duration, actions ...access.Action) access.Decider`.

- [ ] **Step 1: Write the failing test**

`auth/session/elevation_test.go`:

```go
package session_test

import (
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/core/clock"
)

func TestRequireElevationAbstainsOnUnlistedActions(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()
	ctx := session.TestWithSession(t.Context(), sess)

	d := session.RequireElevation(10*time.Minute, "tenant:delete")

	dec, err := d.Decide(ctx, access.Subject{ID: "u1"}, "project:read", access.Resource{})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if dec.Effect != access.Abstain {
		t.Fatalf("effect = %v for an unlisted action, want Abstain — an unscoped decider would deny every action in the app", dec.Effect)
	}
}

func TestRequireElevationDeniesWhenStale(t *testing.T) {
	start := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	clk := clock.NewMock(start)
	mgr := newTestManager(t, session.WithClock(clk))

	sess := mgr.Start()
	if err := mgr.Authenticate(t.Context(), sess, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	ctx := session.TestWithSession(t.Context(), sess)

	d := session.RequireElevation(10*time.Minute, "tenant:delete")

	dec, err := d.Decide(ctx, access.Subject{ID: "u1"}, "tenant:delete", access.Resource{})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if dec.Effect != access.Abstain {
		t.Fatalf("effect = %v for a freshly elevated session, want Abstain so rbac can allow", dec.Effect)
	}

	clk.Advance(20 * time.Minute)
	dec, err = d.Decide(ctx, access.Subject{ID: "u1"}, "tenant:delete", access.Resource{})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if dec.Effect != access.Deny {
		t.Fatalf("effect = %v for a stale elevation, want Deny", dec.Effect)
	}
	if dec.Reason == "" {
		t.Fatal("a denial must carry a reason for logs and the forbidden handler")
	}
}

func TestRequireElevationDeniesWithoutASession(t *testing.T) {
	d := session.RequireElevation(10*time.Minute, "tenant:delete")

	dec, err := d.Decide(t.Context(), access.Subject{ID: "u1"}, "tenant:delete", access.Resource{})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if dec.Effect != access.Deny {
		t.Fatalf("effect = %v with no session in context, want Deny — fail closed", dec.Effect)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./auth/session/ -run TestRequireElevation`
Expected: FAIL — `undefined: session.RequireElevation`.

- [ ] **Step 3: Write minimal implementation**

`auth/session/elevation.go`:

```go
package session

import (
	"context"
	"slices"
	"time"

	"github.com/dmitrymomot/forge/auth/access"
)

// RequireElevation returns a Decider that denies the listed actions unless
// identity was re-proved within window, and abstains on everything else.
//
// The action list is mandatory: an unscoped elevation decider composed under
// access.DenyOverrides would deny every action in the application. Abstaining
// when the session IS elevated lets rbac or acl supply the Allow, so elevation
// adds a requirement rather than replacing authorization.
func RequireElevation(window time.Duration, actions ...access.Action) access.Decider {
	guarded := slices.Clone(actions)
	return access.Named("session.elevation", access.DeciderFunc(
		func(ctx context.Context, _ access.Subject, a access.Action, _ access.Resource) (access.Decision, error) {
			if !slices.Contains(guarded, a) {
				return access.Abstain.Because("action does not require elevation"), nil
			}
			inf, ok := FromContext(ctx)
			if !ok || !inf.Authenticated() {
				return access.Deny.Because("no authenticated session"), nil
			}
			if inf.ElevatedAt.IsZero() {
				return access.Deny.Because("identity has not been re-proved"), nil
			}
			if elapsed(ctx, inf.ElevatedAt) >= window {
				return access.Deny.Because("elevation expired; re-authenticate to continue"), nil
			}
			return access.Abstain.Because("elevation satisfied"), nil
		}))
}
```

The decider needs the manager's clock to stay testable, so read it from the session rather than calling `time.Now`:

```go
// elapsed returns how long ago t was, using the session's clock when one is
// available so tests can drive a mock.
func elapsed(ctx context.Context, t time.Time) time.Duration {
	if s, ok := fromContext(ctx); ok && s.now != nil {
		return s.now().Sub(t)
	}
	return time.Since(t)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `just test ./auth/session/ -run TestRequireElevation -v`
Expected: PASS — 3 tests.

- [ ] **Step 5: Commit**

```bash
just fmt ./auth/session/...
just lint
git add auth/session/elevation.go auth/session/elevation_test.go
git commit -m "feat(session): action-scoped elevation decider for auth/access"
```

---

### Task 14: Benchmarks, optimization pass, and package docs

**Files:**
- Create: `auth/session/bench_test.go`, `auth/session/doc.go`
- Modify: whichever files the benchmark results justify
- Modify: `docs/packages.md`

**Interfaces:**
- Consumes: everything.
- Produces: no new API. A `doc.go` whose example compiles against the real API.

- [ ] **Step 1: Write the benchmarks**

`auth/session/bench_test.go`:

```go
package session_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/auth/session"
)

var benchCart = session.NewNamespace[cartData]("bench.cart")

func BenchmarkNoOpRequest(b *testing.B) {
	mgr := benchManager(b)
	seed := seedSession(b, mgr)
	mw := session.Middleware(mgr, session.WithTransport(headerTransport{}))
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Test-Token", seed.Token())

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		h.ServeHTTP(httptest.NewRecorder(), r)
	}
}

func BenchmarkNamespaceGet(b *testing.B) {
	mgr := benchManager(b)
	sess := seedSessionWithCart(b, mgr)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := benchCart.Get(sess); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNamespaceSetAndEncode(b *testing.B) {
	mgr := benchManager(b)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		sess := mgr.Start()
		benchCart.Set(sess, cartData{Items: []string{"a", "b", "c"}})
		if err := mgr.Save(b.Context(), sess); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnknownNamespacePassthrough(b *testing.B) {
	mgr := benchManager(b)
	sess := seedSessionWithForeignKeys(b, mgr)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchCart.Set(sess, cartData{Items: []string{"x"}})
		if err := mgr.Save(b.Context(), sess); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCommitWithRotation(b *testing.B) {
	mgr := benchManager(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		sess := mgr.Start()
		if err := mgr.Authenticate(b.Context(), sess, "u1"); err != nil {
			b.Fatal(err)
		}
		_ = i
	}
}
```

Helpers, in the same file:

```go
func benchManager(b *testing.B) *session.Manager {
	b.Helper()
	mgr, err := session.New(session.DefaultConfig(), session.WithStore(session.NewMemoryStore()))
	if err != nil {
		b.Fatalf("session.New: %v", err)
	}
	return mgr
}

func seedSession(b *testing.B, mgr *session.Manager) *session.Session {
	b.Helper()
	sess := mgr.Start()
	if err := mgr.Authenticate(b.Context(), sess, "bench-user"); err != nil {
		b.Fatalf("Authenticate: %v", err)
	}
	return sess
}

func seedSessionWithCart(b *testing.B, mgr *session.Manager) *session.Session {
	b.Helper()
	sess := seedSession(b, mgr)
	benchCart.Set(sess, cartData{Items: []string{"a", "b", "c"}})
	if err := mgr.Save(b.Context(), sess); err != nil {
		b.Fatalf("Save: %v", err)
	}
	reloaded, err := mgr.Load(b.Context(), sess.Token())
	if err != nil {
		b.Fatalf("Load: %v", err)
	}
	return reloaded
}

// seedSessionWithForeignKeys builds a payload carrying namespaces this process
// never declared, so the benchmark measures raw passthrough on save.
func seedSessionWithForeignKeys(b *testing.B, mgr *session.Manager) *session.Session {
	b.Helper()
	sess := seedSession(b, mgr)
	for _, ns := range []*session.Namespace[map[string]string]{foreignA, foreignB, foreignC} {
		ns.Set(sess, map[string]string{"k1": "v1", "k2": "v2"})
	}
	if err := mgr.Save(b.Context(), sess); err != nil {
		b.Fatalf("Save: %v", err)
	}
	reloaded, err := mgr.Load(b.Context(), sess.Token())
	if err != nil {
		b.Fatalf("Load: %v", err)
	}
	return reloaded
}

var (
	foreignA = session.NewNamespace[map[string]string]("bench.foreign.a")
	foreignB = session.NewNamespace[map[string]string]("bench.foreign.b")
	foreignC = session.NewNamespace[map[string]string]("bench.foreign.c")
)
```

- [ ] **Step 2: Record the baseline**

Run: `just bench ./auth/session/ | tee docs/superpowers/specs/2026-07-23-session-bench-baseline.txt`
Expected: five benchmark lines with ns/op and allocs/op. This file is the "before" column in the PR.

- [ ] **Step 3: Optimization pass**

Read the baseline and act only on measured wins. Likely candidates, in the order the spec's priorities imply:

- `BenchmarkNoOpRequest` should perform **zero** JSON work and one store read. If allocations exceed roughly the context values plus the record clone, find out why before optimizing anything else — this is the hot path of every authenticated request.
- `withSession` allocates an `Info` per request. If that shows up, consider embedding the `Info` value in the `Session` and storing a pointer to the embedded field, removing one allocation.
- `Session.parse` builds a map even for an empty payload. Guard it so a zero-length payload leaves `raw` nil until a `Set` needs it.

Any change made here must show a measured improvement. Record the after-numbers:

Run: `just bench ./auth/session/ | tee docs/superpowers/specs/2026-07-23-session-bench-after.txt`

- [ ] **Step 4: Write `doc.go`**

`auth/session/doc.go` — the example must compile against the real API. Keep the code block separated from any preceding bullet list by a prose paragraph, or gofmt reflows it into the list.

```go
// Package session stores durable per-visitor state behind a rotating token.
//
// A session is a bucket, not an authentication mechanism: it holds data for
// anonymous and signed-in visitors alike. Authentication gating belongs to
// auth/guard, authorization to auth/access. Session records who the visitor is
// and hands that fact to those packages through an adapter.
//
// There are two entry points. New returns a Manager, which owns lifecycle and
// storage and knows nothing about HTTP. Middleware owns the request layer:
// it extracts the credential, loads and validates the record, runs policies,
// exposes the session on the context, and commits exactly once before the
// first byte of the response — which is why a login handler can redirect and
// still set its cookie.
//
// Stores, transports, and policies all live in sibling packages that import
// this one and use only its exported API, so a third-party implementation is
// indistinguishable from a first-party one.
//
// # Usage
//
//	var Cart = session.NewNamespace[CartData]("cart")
//
//	sessions, err := session.New(session.DefaultConfig(),
//		session.WithStore(session.NewMemoryStore()),
//		session.WithIdle(24*time.Hour),
//		session.WithMaxTTL(7*24*time.Hour),
//	)
//	if err != nil {
//		return err
//	}
//
//	mux.Handle("/", session.Middleware(sessions,
//		session.WithTransport(myTransport),
//	)(handler))
//
//	func addToCart(w http.ResponseWriter, r *http.Request) {
//		sess := sessions.MustFor(r)
//		cart, err := Cart.Get(sess)
//		if err != nil {
//			http.Error(w, "bad session", http.StatusInternalServerError)
//			return
//		}
//		cart.Items = append(cart.Items, "sku-1")
//		Cart.Set(sess, cart)
//	}
package session
```

- [ ] **Step 5: Update the package catalog**

In `docs/packages.md`, replace the `auth/session` entry's body with the shipped shape: Manager plus Middleware, the namespaced payload, pinned device metadata, elevation via an access.Decider, and the driver list marked as forthcoming. Remove the stale mentions of `WithFingerprint`, `UserIndex`-as-only-extension, and tenancy, none of which exist in this design.

- [ ] **Step 6: Full verification**

```bash
just fmt ./auth/session/...
just lint
just test ./auth/session/... -race
go vet ./auth/session/...
```

Expected: all green, no races. Confirm `go doc ./auth/session` renders the example.

- [ ] **Step 7: Commit**

```bash
git add auth/session/bench_test.go auth/session/doc.go docs/packages.md docs/superpowers/specs/2026-07-23-session-bench-*.txt
git commit -m "feat(session): benchmarks, optimization pass, package documentation"
```

---

## Definition of done for this PR

- [ ] `just test ./auth/session/... -race` green.
- [ ] `just lint` green.
- [ ] Every spec seam has an implementation outside-writable: `Store`, `Transport`, `Policy` are satisfiable without touching an unexported identifier. Sanity-check by confirming the test files' `headerTransport` and `readOnlyTransport` reference nothing unexported.
- [ ] Baseline and after benchmark numbers in the PR body.
- [ ] The two "prove it fails first" steps (Task 6 Step 5, Task 10 Step 5) actually observed failing before the fix was restored.
- [ ] `docs/packages.md` reflects the shipped design.
