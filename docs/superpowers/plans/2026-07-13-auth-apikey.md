# auth/apikey Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `auth/apikey` — Stripe-style API keys (issue/manage/verify) behind a storage-agnostic Store, implementing `guard.Verifier`, plus the `pgstore` Postgres driver.

**Architecture:** One core package: `Manager` owns management (Create/Get/List/Revoke/Rotate) and verification (`Verify` implements `guard.Verifier` directly). Keys are `<prefix>_<43 base62 chars><6-char CRC32>`; only the hex SHA-256 of the full plaintext is stored, and malformed credentials are rejected by checksum before any store access. State lives behind a 7-method `Store` seam with an in-package memory store and a pgx driver subpackage.

**Tech Stack:** Go 1.26, `auth/guard`, `core/id` (UUIDv7), `core/random`, `crypto/consttime`, stdlib `crypto/sha256` + `hash/crc32`; driver: pgx v5 + goose migration via `data/migration`.

**Spec:** `docs/superpowers/specs/2026-07-13-auth-apikey-design.md`

## Global Constraints

- Work ONLY in the current branch; never switch.
- After changing files: `just fmt ./auth/apikey/...` (package-path form — single-file form trips a spurious betteralign "undefined").
- After each task: `just lint` (runs modernize/betteralign/nilaway) must pass.
- Tests: black-box only (`package apikey_test` / `package pgstore_test`). Use `httptest.NewRequest`, never `http.NewRequest` with `_` (nilaway).
- No C-style ascending loops — `for i := range n` (descending loops are fine).
- Sentinel errors: single-line `errors.New("apikey: ...")`, `errors.Is`-matchable.
- Never add Claude/AI attribution to commits.
- Every forge package ships `bench_test.go` (repo rule).

---

### Task 1: Record types, errors, Store seam, memory store

**Files:**
- Create: `auth/apikey/apikey.go` (Key, CreateParams, Filter)
- Create: `auth/apikey/errors.go`
- Create: `auth/apikey/store.go` (Store interface)
- Create: `auth/apikey/memory.go` (NewMemoryStore)
- Test: `auth/apikey/store_test.go`

**Interfaces:**
- Consumes: `core/id` (`id.UUID` = `[16]byte`, `id.NewUUID()`).
- Produces (later tasks rely on exactly these):
  - `type Key struct { Meta map[string]string; Scopes []string; Hash, Preview, Name, Subject, Tenant string; ID id.UUID; CreatedAt, ExpiresAt, LastUsedAt, RevokedAt time.Time }`
  - `type CreateParams struct { Meta map[string]string; Scopes []string; Name, Subject, Tenant string; ExpiresAt time.Time }`
  - `type Filter struct { Subject, Tenant string }`
  - `type Store interface { Create(ctx, Key) error; Get(ctx, id.UUID) (Key, error); GetByHash(ctx, string) (Key, error); List(ctx, Filter) ([]Key, error); Revoke(ctx, id.UUID, time.Time) error; Expire(ctx, id.UUID, time.Time) error; Touch(ctx, id.UUID, time.Time) error }`
  - `func NewMemoryStore() Store`
  - Sentinels: `ErrNotFound, ErrDuplicate, ErrMalformedKey, ErrKeyNotFound, ErrKeyRevoked, ErrKeyExpired, ErrSubjectRequired, ErrTenantMismatch, ErrScope`

- [ ] **Step 1: Write the failing store contract test**

`auth/apikey/store_test.go`:

```go
package apikey_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/apikey"
	"github.com/dmitrymomot/forge/core/id"
)

// mkKey builds a record with a handcrafted ID so ordering tests are
// deterministic (UUIDv7 ids from the same millisecond have random tails).
func mkKey(b byte, hash, subject, tenant string) apikey.Key {
	return apikey.Key{
		ID:        id.UUID{15: b}, // last byte varies → byte-ascending = ascending b
		Hash:      hash,
		Preview:   "key_preview1",
		Subject:   subject,
		Tenant:    tenant,
		Scopes:    []string{"read"},
		Meta:      map[string]string{"k": "v"},
		CreatedAt: time.Now().UTC(),
	}
}

func TestMemoryStore_CreateGet(t *testing.T) {
	t.Parallel()
	s := apikey.NewMemoryStore()
	ctx := context.Background()
	k := mkKey(1, "h1", "u1", "t1")

	require.NoError(t, s.Create(ctx, k))

	got, err := s.Get(ctx, k.ID)
	require.NoError(t, err)
	assert.Equal(t, k.Subject, got.Subject)
	assert.Equal(t, k.Hash, got.Hash)

	byHash, err := s.GetByHash(ctx, "h1")
	require.NoError(t, err)
	assert.Equal(t, k.ID, byHash.ID)
}

func TestMemoryStore_NotFound(t *testing.T) {
	t.Parallel()
	s := apikey.NewMemoryStore()
	ctx := context.Background()

	_, err := s.Get(ctx, id.UUID{15: 9})
	assert.ErrorIs(t, err, apikey.ErrNotFound)
	_, err = s.GetByHash(ctx, "missing")
	assert.ErrorIs(t, err, apikey.ErrNotFound)
	assert.ErrorIs(t, s.Revoke(ctx, id.UUID{15: 9}, time.Now()), apikey.ErrNotFound)
	assert.ErrorIs(t, s.Expire(ctx, id.UUID{15: 9}, time.Now()), apikey.ErrNotFound)
	assert.ErrorIs(t, s.Touch(ctx, id.UUID{15: 9}, time.Now()), apikey.ErrNotFound)
}

func TestMemoryStore_Duplicate(t *testing.T) {
	t.Parallel()
	s := apikey.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, mkKey(1, "h1", "u1", "")))

	assert.ErrorIs(t, s.Create(ctx, mkKey(1, "h-other", "u1", "")), apikey.ErrDuplicate) // same ID
	assert.ErrorIs(t, s.Create(ctx, mkKey(2, "h1", "u1", "")), apikey.ErrDuplicate)      // same hash
}

func TestMemoryStore_ListFilterAndOrder(t *testing.T) {
	t.Parallel()
	s := apikey.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, mkKey(1, "h1", "u1", "t1")))
	require.NoError(t, s.Create(ctx, mkKey(2, "h2", "u2", "t1")))
	require.NoError(t, s.Create(ctx, mkKey(3, "h3", "u1", "t2")))

	all, err := s.List(ctx, apikey.Filter{})
	require.NoError(t, err)
	require.Len(t, all, 3)
	// Newest first = descending ID bytes.
	assert.Equal(t, id.UUID{15: 3}, all[0].ID)
	assert.Equal(t, id.UUID{15: 1}, all[2].ID)

	t1, err := s.List(ctx, apikey.Filter{Tenant: "t1"})
	require.NoError(t, err)
	assert.Len(t, t1, 2)

	u1t1, err := s.List(ctx, apikey.Filter{Subject: "u1", Tenant: "t1"})
	require.NoError(t, err)
	require.Len(t, u1t1, 1)
	assert.Equal(t, id.UUID{15: 1}, u1t1[0].ID)
}

func TestMemoryStore_Mutators(t *testing.T) {
	t.Parallel()
	s := apikey.NewMemoryStore()
	ctx := context.Background()
	k := mkKey(1, "h1", "u1", "")
	require.NoError(t, s.Create(ctx, k))
	at := time.Now().UTC().Truncate(time.Second)

	require.NoError(t, s.Revoke(ctx, k.ID, at))
	require.NoError(t, s.Expire(ctx, k.ID, at.Add(time.Hour)))
	require.NoError(t, s.Touch(ctx, k.ID, at.Add(time.Minute)))

	got, err := s.Get(ctx, k.ID)
	require.NoError(t, err)
	assert.True(t, got.RevokedAt.Equal(at))
	assert.True(t, got.ExpiresAt.Equal(at.Add(time.Hour)))
	assert.True(t, got.LastUsedAt.Equal(at.Add(time.Minute)))
}

func TestMemoryStore_CloneIsolation(t *testing.T) {
	t.Parallel()
	s := apikey.NewMemoryStore()
	ctx := context.Background()
	k := mkKey(1, "h1", "u1", "")
	require.NoError(t, s.Create(ctx, k))

	// Mutating the caller's copy or a returned copy must not affect storage.
	k.Meta["k"] = "mutated"
	got1, err := s.Get(ctx, k.ID)
	require.NoError(t, err)
	assert.Equal(t, "v", got1.Meta["k"])

	got1.Meta["k"] = "mutated-again"
	got1.Scopes[0] = "write"
	got2, err := s.Get(ctx, k.ID)
	require.NoError(t, err)
	assert.Equal(t, "v", got2.Meta["k"])
	assert.Equal(t, "read", got2.Scopes[0])
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./auth/apikey/`
Expected: FAIL — compile errors (`undefined: apikey.NewMemoryStore` etc.); the package doesn't exist yet.

- [ ] **Step 3: Implement types, errors, seam, memory store**

`auth/apikey/apikey.go`:

```go
package apikey

import (
	"time"

	"github.com/dmitrymomot/forge/core/id"
)

// Key is the stored API-key record. It never contains the plaintext
// secret: Hash is the hex SHA-256 of the full plaintext and Preview its
// first 12 characters for dashboard display.
type Key struct {
	Meta       map[string]string // caller extras, copied into Identity.Meta on verify
	Scopes     []string          // carried into Identity.Scopes; never enforced here
	Hash       string            // hex SHA-256 of the full plaintext key
	Preview    string            // first 12 plaintext chars — safe to display
	Name       string            // human label
	Subject    string            // principal the key acts as — never empty
	Tenant     string            // owning tenant; empty in single-tenant apps
	ID         id.UUID           // UUIDv7 record id — time-ordered, never secret-derived
	CreatedAt  time.Time
	ExpiresAt  time.Time // zero = never expires
	LastUsedAt time.Time // zero = never used
	RevokedAt  time.Time // zero = active
}

// CreateParams describes a key to mint. Subject is required: for personal
// keys it is the user id; for tenant-wide keys, whatever principal
// represents the org acting as itself (tenant id or a service-account id).
type CreateParams struct {
	Meta      map[string]string
	Scopes    []string
	Name      string
	Subject   string    // required
	Tenant    string    // optional; constrained by the WithScope hook when set
	ExpiresAt time.Time // zero = never
}

// Filter narrows List results. Zero fields match everything.
type Filter struct {
	Subject string
	Tenant  string
}
```

`auth/apikey/errors.go`:

```go
package apikey

import "errors"

var (
	// ErrNotFound is returned by a Store when no record matches, and by
	// management operations for other tenants' keys under WithScope (so
	// cross-tenant existence cannot be probed).
	ErrNotFound = errors.New("apikey: record not found")

	// ErrDuplicate is returned by Store.Create when a record with the same
	// ID or Hash already exists.
	ErrDuplicate = errors.New("apikey: duplicate record")

	// ErrMalformedKey rejects credentials failing prefix/length/checksum
	// validation — decided before any store access.
	ErrMalformedKey = errors.New("apikey: malformed key")

	// ErrKeyNotFound rejects well-formed credentials with no matching record.
	ErrKeyNotFound = errors.New("apikey: key not found")

	// ErrKeyRevoked rejects credentials of revoked keys.
	ErrKeyRevoked = errors.New("apikey: key revoked")

	// ErrKeyExpired rejects credentials of expired keys.
	ErrKeyExpired = errors.New("apikey: key expired")

	// ErrSubjectRequired rejects CreateParams with an empty Subject.
	ErrSubjectRequired = errors.New("apikey: subject required")

	// ErrTenantMismatch rejects management calls whose explicit tenant
	// conflicts with the WithScope-derived tenant.
	ErrTenantMismatch = errors.New("apikey: tenant mismatch")

	// ErrScope fails management operations closed when the WithScope hook
	// errors or yields an empty tenant.
	ErrScope = errors.New("apikey: tenant scope unavailable")
)
```

`auth/apikey/store.go`:

```go
package apikey

import (
	"context"
	"time"

	"github.com/dmitrymomot/forge/core/id"
)

// Store persists Key records. Implementations must be safe for concurrent
// use. Create fails with ErrDuplicate when a record with the same ID or
// Hash exists; lookups and mutators return ErrNotFound for unknown ids.
type Store interface {
	Create(ctx context.Context, k Key) error
	Get(ctx context.Context, keyID id.UUID) (Key, error)
	// GetByHash is the verification path: one point lookup by the hex
	// SHA-256 of the presented plaintext.
	GetByHash(ctx context.Context, hash string) (Key, error)
	// List returns records matching f, newest first (UUIDv7 id order;
	// ties within one millisecond are unordered).
	List(ctx context.Context, f Filter) ([]Key, error)
	Revoke(ctx context.Context, keyID id.UUID, at time.Time) error
	Expire(ctx context.Context, keyID id.UUID, at time.Time) error
	Touch(ctx context.Context, keyID id.UUID, at time.Time) error
}
```

`auth/apikey/memory.go`:

```go
package apikey

import (
	"bytes"
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/core/id"
)

type memoryStore struct {
	byID   map[id.UUID]Key
	byHash map[string]id.UUID
	mu     sync.RWMutex
}

// NewMemoryStore returns an in-memory Store for tests and development.
func NewMemoryStore() Store {
	return &memoryStore{
		byID:   make(map[id.UUID]Key),
		byHash: make(map[string]id.UUID),
	}
}

// cloneKey copies the record's reference fields so callers cannot mutate
// stored state through shared slices/maps (and vice versa).
func cloneKey(k Key) Key {
	k.Scopes = slices.Clone(k.Scopes)
	k.Meta = maps.Clone(k.Meta)
	return k
}

func (s *memoryStore) Create(_ context.Context, k Key) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[k.ID]; ok {
		return ErrDuplicate
	}
	if _, ok := s.byHash[k.Hash]; ok {
		return ErrDuplicate
	}
	s.byID[k.ID] = cloneKey(k)
	s.byHash[k.Hash] = k.ID
	return nil
}

func (s *memoryStore) Get(_ context.Context, keyID id.UUID) (Key, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	k, ok := s.byID[keyID]
	if !ok {
		return Key{}, ErrNotFound
	}
	return cloneKey(k), nil
}

func (s *memoryStore) GetByHash(_ context.Context, hash string) (Key, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keyID, ok := s.byHash[hash]
	if !ok {
		return Key{}, ErrNotFound
	}
	return cloneKey(s.byID[keyID]), nil
}

func (s *memoryStore) List(_ context.Context, f Filter) ([]Key, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Key, 0, len(s.byID))
	for _, k := range s.byID {
		if f.Subject != "" && k.Subject != f.Subject {
			continue
		}
		if f.Tenant != "" && k.Tenant != f.Tenant {
			continue
		}
		out = append(out, cloneKey(k))
	}
	// UUIDv7 ids are time-ordered, so byte-descending is newest first.
	slices.SortFunc(out, func(a, b Key) int {
		return bytes.Compare(b.ID[:], a.ID[:])
	})
	return out, nil
}

func (s *memoryStore) Revoke(_ context.Context, keyID id.UUID, at time.Time) error {
	return s.update(keyID, func(k *Key) { k.RevokedAt = at })
}

func (s *memoryStore) Expire(_ context.Context, keyID id.UUID, at time.Time) error {
	return s.update(keyID, func(k *Key) { k.ExpiresAt = at })
}

func (s *memoryStore) Touch(_ context.Context, keyID id.UUID, at time.Time) error {
	return s.update(keyID, func(k *Key) { k.LastUsedAt = at })
}

func (s *memoryStore) update(keyID id.UUID, fn func(*Key)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.byID[keyID]
	if !ok {
		return ErrNotFound
	}
	fn(&k)
	s.byID[keyID] = k
	return nil
}
```

- [ ] **Step 4: Run tests, fmt, verify pass**

Run: `just fmt ./auth/apikey/... && just test ./auth/apikey/`
Expected: PASS (race + cover on).

- [ ] **Step 5: Commit**

```bash
git add auth/apikey/
git commit -m "feat(apikey): record types, sentinels, Store seam, memory store"
```

---

### Task 2: Key generation, Manager construction, Verify

**Files:**
- Create: `auth/apikey/keygen.go`
- Create: `auth/apikey/options.go`
- Create: `auth/apikey/manager.go` (New + Create only)
- Create: `auth/apikey/verify.go`
- Test: `auth/apikey/manager_test.go`, `auth/apikey/verify_test.go`

**Interfaces:**
- Consumes: Task 1's types/sentinels/Store; `random.String(43)` (defaults to the 62-char Alphanumeric set); `consttime.StringEqual(a, b string) bool`; `guard.Identity{Meta, Subject, Tenant, Method, Scopes}`, `guard.MethodAPIKey`, `guard.Verifier`.
- Produces:
  - `func New(store Store, opts ...Option) *Manager` — panics on nil store / invalid prefix
  - `func WithPrefix(p string) Option`, `func WithTouchInterval(d time.Duration) Option`, `func WithScope(fn func(context.Context) (string, error)) Option` (declared here, wired in Task 3)
  - `func (m *Manager) Create(ctx context.Context, p CreateParams) (Key, string, error)`
  - `func (m *Manager) Verify(ctx context.Context, credential string) (guard.Identity, error)`
  - Unexported: `newKey(prefix) string`, `validKey(prefix, credential) bool`, `hashKey(credential) string`, `validPrefix(p) bool`, `previewLen = 12`

- [ ] **Step 1: Write the failing tests**

`auth/apikey/manager_test.go`:

```go
package apikey_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/apikey"
)

func TestNew_Panics(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { apikey.New(nil) })
	assert.Panics(t, func() { apikey.New(apikey.NewMemoryStore(), apikey.WithPrefix("")) })
	assert.Panics(t, func() { apikey.New(apikey.NewMemoryStore(), apikey.WithPrefix("SK-Live")) })
}

func TestCreate_KeyAnatomy(t *testing.T) {
	t.Parallel()
	store := apikey.NewMemoryStore()
	mgr := apikey.New(store, apikey.WithPrefix("sk_live"))

	k, plaintext, err := mgr.Create(context.Background(), apikey.CreateParams{
		Subject: "user_42", Tenant: "org_7", Name: "CI deploy", Scopes: []string{"deploy:write"},
	})
	require.NoError(t, err)

	// <prefix>_<43 payload><6 checksum>
	assert.Len(t, plaintext, len("sk_live")+1+43+6)
	assert.True(t, strings.HasPrefix(plaintext, "sk_live_"))
	for _, c := range plaintext[len("sk_live_"):] {
		assert.Contains(t, "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz", string(c))
	}
	assert.Equal(t, plaintext[:12], k.Preview)
	assert.NotContains(t, k.Hash, plaintext[8:20]) // hash, not plaintext, at rest
	assert.False(t, k.ID.IsZero())
	assert.False(t, k.CreatedAt.IsZero())

	stored, err := store.Get(context.Background(), k.ID)
	require.NoError(t, err)
	assert.Equal(t, "user_42", stored.Subject)
	assert.Equal(t, "org_7", stored.Tenant)
}

func TestCreate_SubjectRequired(t *testing.T) {
	t.Parallel()
	mgr := apikey.New(apikey.NewMemoryStore())
	_, _, err := mgr.Create(context.Background(), apikey.CreateParams{})
	assert.ErrorIs(t, err, apikey.ErrSubjectRequired)
}

func TestCreate_DefaultPrefix(t *testing.T) {
	t.Parallel()
	mgr := apikey.New(apikey.NewMemoryStore())
	_, plaintext, err := mgr.Create(context.Background(), apikey.CreateParams{Subject: "u1"})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(plaintext, "key_"))
}
```

`auth/apikey/verify_test.go`:

```go
package apikey_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/apikey"
	"github.com/dmitrymomot/forge/auth/guard"
)

// issue returns a manager over its own memory store plus one minted key.
func issue(t *testing.T, opts ...apikey.Option) (*apikey.Manager, apikey.Store, apikey.Key, string) {
	t.Helper()
	store := apikey.NewMemoryStore()
	mgr := apikey.New(store, append([]apikey.Option{apikey.WithPrefix("sk_live")}, opts...)...)
	k, plaintext, err := mgr.Create(context.Background(), apikey.CreateParams{
		Subject: "user_42", Tenant: "org_7", Name: "CI deploy",
		Scopes: []string{"deploy:write"}, Meta: map[string]string{"env": "prod"},
	})
	require.NoError(t, err)
	return mgr, store, k, plaintext
}

// tamper flips the final checksum character to a different base62 char.
func tamper(s string) string {
	last := s[len(s)-1]
	repl := byte('a')
	if last == 'a' {
		repl = 'b'
	}
	return s[:len(s)-1] + string(repl)
}

func TestVerify_OK(t *testing.T) {
	t.Parallel()
	mgr, _, k, plaintext := issue(t)

	identity, err := mgr.Verify(context.Background(), plaintext)
	require.NoError(t, err)
	assert.Equal(t, "user_42", identity.Subject)
	assert.Equal(t, "org_7", identity.Tenant)
	assert.Equal(t, guard.MethodAPIKey, identity.Method)
	assert.Equal(t, []string{"deploy:write"}, identity.Scopes)
	assert.Equal(t, k.ID.String(), identity.Meta["key_id"])
	assert.Equal(t, "CI deploy", identity.Meta["key_name"])
	assert.Equal(t, "prod", identity.Meta["env"])
}

func TestVerify_Malformed(t *testing.T) {
	t.Parallel()
	mgr, _, _, plaintext := issue(t)
	ctx := context.Background()

	for name, cred := range map[string]string{
		"empty":        "",
		"prefix only":  "sk_live_",
		"truncated":    plaintext[:len(plaintext)-1],
		"bad checksum": tamper(plaintext),
		"wrong prefix": "sk_test" + plaintext[len("sk_live"):],
	} {
		_, err := mgr.Verify(ctx, cred)
		assert.ErrorIs(t, err, apikey.ErrMalformedKey, name)
	}
}

func TestVerify_UnknownKey(t *testing.T) {
	t.Parallel()
	mgr, _, _, _ := issue(t)
	// A second issuer with the same prefix mints a structurally valid key
	// that the first store has never seen.
	other := apikey.New(apikey.NewMemoryStore(), apikey.WithPrefix("sk_live"))
	_, foreign, err := other.Create(context.Background(), apikey.CreateParams{Subject: "u9"})
	require.NoError(t, err)

	_, err = mgr.Verify(context.Background(), foreign)
	assert.ErrorIs(t, err, apikey.ErrKeyNotFound)
}

func TestVerify_Revoked(t *testing.T) {
	t.Parallel()
	mgr, store, k, plaintext := issue(t)
	require.NoError(t, store.Revoke(context.Background(), k.ID, time.Now().UTC()))

	_, err := mgr.Verify(context.Background(), plaintext)
	assert.ErrorIs(t, err, apikey.ErrKeyRevoked)
}

func TestVerify_Expired(t *testing.T) {
	t.Parallel()
	mgr, store, k, plaintext := issue(t)
	ctx := context.Background()

	require.NoError(t, store.Expire(ctx, k.ID, time.Now().UTC().Add(time.Hour)))
	_, err := mgr.Verify(ctx, plaintext) // future expiry still verifies
	require.NoError(t, err)

	require.NoError(t, store.Expire(ctx, k.ID, time.Now().UTC().Add(-time.Second)))
	_, err = mgr.Verify(ctx, plaintext)
	assert.ErrorIs(t, err, apikey.ErrKeyExpired)
}

func TestVerify_EmptySubjectRecordRejected(t *testing.T) {
	t.Parallel()
	// A corrupt record (empty Subject) seeded directly into the store must
	// not authenticate, even though guard would also catch it.
	_, _, k, plaintext := issue(t)
	ctx := context.Background()

	corrupt := k
	corrupt.Subject = ""
	store := apikey.NewMemoryStore()
	require.NoError(t, store.Create(ctx, corrupt))
	mgr := apikey.New(store, apikey.WithPrefix("sk_live"))

	_, err := mgr.Verify(ctx, plaintext)
	assert.ErrorIs(t, err, apikey.ErrKeyNotFound)
}

func TestVerify_TouchThrottling(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("default touches never-used key once", func(t *testing.T) {
		t.Parallel()
		mgr, store, k, plaintext := issue(t)
		_, err := mgr.Verify(ctx, plaintext)
		require.NoError(t, err)
		got, err := store.Get(ctx, k.ID)
		require.NoError(t, err)
		first := got.LastUsedAt
		assert.False(t, first.IsZero())

		// Immediately re-verify: fresher than 60s → untouched.
		_, err = mgr.Verify(ctx, plaintext)
		require.NoError(t, err)
		got, err = store.Get(ctx, k.ID)
		require.NoError(t, err)
		assert.True(t, got.LastUsedAt.Equal(first))
	})

	t.Run("stale record is touched", func(t *testing.T) {
		t.Parallel()
		mgr, store, k, plaintext := issue(t)
		old := time.Now().UTC().Add(-2 * time.Minute)
		require.NoError(t, store.Touch(ctx, k.ID, old))
		_, err := mgr.Verify(ctx, plaintext)
		require.NoError(t, err)
		got, err := store.Get(ctx, k.ID)
		require.NoError(t, err)
		assert.True(t, got.LastUsedAt.After(old))
	})

	t.Run("negative interval disables tracking", func(t *testing.T) {
		t.Parallel()
		mgr, store, k, plaintext := issue(t, apikey.WithTouchInterval(-1))
		_, err := mgr.Verify(ctx, plaintext)
		require.NoError(t, err)
		got, err := store.Get(ctx, k.ID)
		require.NoError(t, err)
		assert.True(t, got.LastUsedAt.IsZero())
	})

	t.Run("zero interval touches every request", func(t *testing.T) {
		t.Parallel()
		mgr, store, k, plaintext := issue(t, apikey.WithTouchInterval(0))
		recent := time.Now().UTC().Add(-time.Second)
		require.NoError(t, store.Touch(ctx, k.ID, recent))
		_, err := mgr.Verify(ctx, plaintext)
		require.NoError(t, err)
		got, err := store.Get(ctx, k.ID)
		require.NoError(t, err)
		assert.True(t, got.LastUsedAt.After(recent))
	})
}

func TestVerify_IdentityMetaIsolated(t *testing.T) {
	t.Parallel()
	mgr, _, _, plaintext := issue(t)
	ctx := context.Background()

	id1, err := mgr.Verify(ctx, plaintext)
	require.NoError(t, err)
	id1.Meta["env"] = "mutated"
	id1.Scopes[0] = "mutated"

	id2, err := mgr.Verify(ctx, plaintext)
	require.NoError(t, err)
	assert.Equal(t, "prod", id2.Meta["env"])
	assert.Equal(t, "deploy:write", id2.Scopes[0])
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./auth/apikey/`
Expected: FAIL — `undefined: apikey.New`, `undefined: apikey.WithPrefix`, etc.

- [ ] **Step 3: Implement keygen, options, Manager.New/Create, Verify**

`auth/apikey/keygen.go`:

```go
package apikey

import (
	"crypto/sha256"
	"encoding/hex"
	"hash/crc32"
	"strings"

	"github.com/dmitrymomot/forge/core/random"
)

const (
	payloadLen  = 43 // base62 chars ≈ 256 bits of entropy
	checksumLen = 6  // fixed-width base62 CRC32 (62^6 > 2^32)
	previewLen  = 12
)

// base62 is the checksum alphabet. The payload draws from the same 62
// characters via random.String's default Alphanumeric set.
const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// newKey mints a plaintext key: <prefix>_<payload><checksum>.
func newKey(prefix string) string {
	payload := random.String(payloadLen)
	var b strings.Builder
	b.Grow(len(prefix) + 1 + payloadLen + checksumLen)
	b.WriteString(prefix)
	b.WriteByte('_')
	b.WriteString(payload)
	b.WriteString(encodeChecksum(crc32.ChecksumIEEE([]byte(payload))))
	return b.String()
}

// encodeChecksum renders v as fixed-width base62, most significant first.
func encodeChecksum(v uint32) string {
	var buf [checksumLen]byte
	for i := checksumLen - 1; i >= 0; i-- {
		buf[i] = base62[v%62]
		v /= 62
	}
	return string(buf[:])
}

// validKey reports whether credential is structurally valid for prefix:
// exact length, prefix match, and payload CRC32 matching the checksum
// suffix. CRC32 detects any burst error up to 32 bits, so every
// single-character corruption is caught. The checksum is compared in
// place (no encodeChecksum string) to keep this reject path
// allocation-free — it is the DoS-relevant surface that shields the
// store from credential-stuffing garbage.
func validKey(prefix, credential string) bool {
	wantLen := len(prefix) + 1 + payloadLen + checksumLen
	if len(credential) != wantLen {
		return false
	}
	if credential[:len(prefix)] != prefix || credential[len(prefix)] != '_' {
		return false
	}
	payload := credential[len(prefix)+1 : wantLen-checksumLen]
	sum := crc32.ChecksumIEEE([]byte(payload))
	suffix := credential[wantLen-checksumLen:]
	for i := checksumLen - 1; i >= 0; i-- {
		if suffix[i] != base62[sum%62] {
			return false
		}
		sum /= 62
	}
	return true
}

// hashKey returns the hex SHA-256 of the full plaintext key — the stored
// lookup hash. Unsalted is safe: the payload carries ~256 bits of entropy,
// so preimage search is infeasible.
func hashKey(credential string) string {
	sum := sha256.Sum256([]byte(credential))
	return hex.EncodeToString(sum[:])
}

// validPrefix reports whether p is non-empty [a-z0-9_]+.
func validPrefix(p string) bool {
	if p == "" {
		return false
	}
	for i := range len(p) {
		c := p[i]
		if c != '_' && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}
```

`auth/apikey/options.go`:

```go
package apikey

import (
	"context"
	"time"
)

type config struct {
	scope         func(context.Context) (string, error)
	prefix        string
	touchInterval time.Duration
}

// Option configures New.
type Option func(*config)

// WithPrefix sets the key prefix (default "key"); keys read
// <prefix>_<payload><checksum>. The prefix must match [a-z0-9_]+.
// Environments are separate issuers: run one Manager with "sk_live" and
// another with "sk_test".
func WithPrefix(p string) Option {
	return func(c *config) { c.prefix = p }
}

// WithScope derives the tenant from context for every management
// operation: Create stamps it, List is confined to it, and
// Get/Revoke/Rotate report ErrNotFound for other tenants' keys.
// Fail-closed: a hook error or empty tenant fails the operation with
// ErrScope. Verify is unaffected — the key record itself carries the
// tenant. A nil fn leaves the manager unscoped.
func WithScope(fn func(context.Context) (string, error)) Option {
	return func(c *config) { c.scope = fn }
}

// WithTouchInterval throttles last-used-at writes: Verify touches the
// record only when LastUsedAt is staler than d (default 60s). Zero
// touches on every request; negative disables tracking.
func WithTouchInterval(d time.Duration) Option {
	return func(c *config) { c.touchInterval = d }
}
```

`auth/apikey/manager.go` (Create's scope-hook wiring lands in Task 3):

```go
package apikey

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/dmitrymomot/forge/core/id"
)

// Manager issues, manages, and verifies API keys over a Store. It
// implements guard.Verifier. Safe for concurrent use.
type Manager struct {
	store Store
	cfg   config
}

// New builds a Manager. It panics on a nil store or an invalid prefix —
// wiring bugs caught at startup, like guard.New's nil-verifier panic.
func New(store Store, opts ...Option) *Manager {
	if store == nil {
		panic("apikey: nil store")
	}
	cfg := config{prefix: "key", touchInterval: time.Minute}
	for _, o := range opts {
		o(&cfg)
	}
	if !validPrefix(cfg.prefix) {
		panic(fmt.Sprintf("apikey: invalid prefix %q", cfg.prefix))
	}
	return &Manager{store: store, cfg: cfg}
}

// Create mints a key for p, returning the stored record and the plaintext.
// The plaintext is shown exactly once — only its hash is persisted.
func (m *Manager) Create(ctx context.Context, p CreateParams) (Key, string, error) {
	if p.Subject == "" {
		return Key{}, "", ErrSubjectRequired
	}
	plaintext := newKey(m.cfg.prefix)
	k := Key{
		ID:        id.NewUUID(),
		Hash:      hashKey(plaintext),
		Preview:   plaintext[:previewLen],
		Name:      p.Name,
		Subject:   p.Subject,
		Tenant:    p.Tenant,
		Scopes:    slices.Clone(p.Scopes),
		Meta:      maps.Clone(p.Meta),
		CreatedAt: time.Now().UTC(),
		ExpiresAt: p.ExpiresAt,
	}
	if err := m.store.Create(ctx, k); err != nil {
		return Key{}, "", fmt.Errorf("apikey: create: %w", err)
	}
	return k, plaintext, nil
}
```

`auth/apikey/verify.go`:

```go
package apikey

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/dmitrymomot/forge/auth/guard"
	"github.com/dmitrymomot/forge/crypto/consttime"
)

var _ guard.Verifier = (*Manager)(nil)

// Verify implements guard.Verifier: it resolves a plaintext credential to
// the identity of the key's Subject. Malformed credentials (wrong prefix,
// length, or checksum) are rejected before any store access. Under
// guard.New every error collapses to an opaque 401; the sentinels serve
// metrics and direct callers.
func (m *Manager) Verify(ctx context.Context, credential string) (guard.Identity, error) {
	if !validKey(m.cfg.prefix, credential) {
		return guard.Identity{}, ErrMalformedKey
	}
	h := hashKey(credential)
	k, err := m.store.GetByHash(ctx, h)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return guard.Identity{}, ErrKeyNotFound
		}
		return guard.Identity{}, fmt.Errorf("apikey: verify: %w", err)
	}
	// Defense-in-depth: a buggy store returning the wrong record (or a
	// corrupt subject-less row) must not authenticate.
	if !consttime.StringEqual(k.Hash, h) || k.Subject == "" {
		return guard.Identity{}, ErrKeyNotFound
	}
	if !k.RevokedAt.IsZero() {
		return guard.Identity{}, ErrKeyRevoked
	}
	now := time.Now().UTC()
	if !k.ExpiresAt.IsZero() && !k.ExpiresAt.After(now) {
		return guard.Identity{}, ErrKeyExpired
	}
	if m.cfg.touchInterval >= 0 && (k.LastUsedAt.IsZero() || now.Sub(k.LastUsedAt) >= m.cfg.touchInterval) {
		// Best-effort by design: a failed touch must not fail authentication.
		_ = m.store.Touch(ctx, k.ID, now)
	}
	meta := maps.Clone(k.Meta)
	if meta == nil {
		meta = make(map[string]string, 2)
	}
	meta["key_id"] = k.ID.String()
	if k.Name != "" {
		meta["key_name"] = k.Name
	}
	return guard.Identity{
		Subject: k.Subject,
		Tenant:  k.Tenant,
		Scopes:  slices.Clone(k.Scopes),
		Method:  guard.MethodAPIKey,
		Meta:    meta,
	}, nil
}
```

- [ ] **Step 4: Run tests, fmt, verify pass**

Run: `just fmt ./auth/apikey/... && just test ./auth/apikey/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add auth/apikey/
git commit -m "feat(apikey): key generation, Manager, guard.Verifier verification"
```

---

### Task 3: Management operations and tenant scope hook

**Files:**
- Modify: `auth/apikey/manager.go` (add scoped helper + Get/List/Revoke/Rotate; wire scope into Create)
- Test: `auth/apikey/manager_test.go` (append), create `auth/apikey/scope_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–2.
- Produces:
  - `func (m *Manager) Get(ctx context.Context, keyID id.UUID) (Key, error)`
  - `func (m *Manager) List(ctx context.Context, f Filter) ([]Key, error)`
  - `func (m *Manager) Revoke(ctx context.Context, keyID id.UUID) error`
  - `func (m *Manager) Rotate(ctx context.Context, keyID id.UUID, grace time.Duration) (Key, string, error)`

- [ ] **Step 1: Write the failing tests**

Append to `auth/apikey/manager_test.go`:

```go
func TestGetRevoke(t *testing.T) {
	t.Parallel()
	store := apikey.NewMemoryStore()
	mgr := apikey.New(store)
	ctx := context.Background()
	k, plaintext, err := mgr.Create(ctx, apikey.CreateParams{Subject: "u1"})
	require.NoError(t, err)

	got, err := mgr.Get(ctx, k.ID)
	require.NoError(t, err)
	assert.Equal(t, "u1", got.Subject)

	require.NoError(t, mgr.Revoke(ctx, k.ID))
	got, err = mgr.Get(ctx, k.ID)
	require.NoError(t, err)
	assert.False(t, got.RevokedAt.IsZero())

	_, err = mgr.Verify(ctx, plaintext)
	assert.ErrorIs(t, err, apikey.ErrKeyRevoked)

	assert.ErrorIs(t, mgr.Revoke(ctx, id.UUID{15: 9}), apikey.ErrNotFound)
}

func TestRotate(t *testing.T) {
	t.Parallel()
	store := apikey.NewMemoryStore()
	mgr := apikey.New(store, apikey.WithPrefix("sk_live"))
	ctx := context.Background()
	old, oldPlain, err := mgr.Create(ctx, apikey.CreateParams{
		Subject: "u1", Tenant: "t1", Name: "prod", Scopes: []string{"a"}, Meta: map[string]string{"m": "1"},
	})
	require.NoError(t, err)

	grace := time.Hour
	before := time.Now().UTC()
	fresh, freshPlain, err := mgr.Rotate(ctx, old.ID, grace)
	require.NoError(t, err)

	// Inheritance.
	assert.Equal(t, old.Subject, fresh.Subject)
	assert.Equal(t, old.Tenant, fresh.Tenant)
	assert.Equal(t, old.Name, fresh.Name)
	assert.Equal(t, old.Scopes, fresh.Scopes)
	assert.Equal(t, old.Meta, fresh.Meta)
	assert.NotEqual(t, old.ID, fresh.ID)
	assert.NotEqual(t, oldPlain, freshPlain)

	// Overlap: both verify during grace.
	_, err = mgr.Verify(ctx, oldPlain)
	require.NoError(t, err)
	_, err = mgr.Verify(ctx, freshPlain)
	require.NoError(t, err)

	// Old key's expiry ≈ now+grace.
	oldStored, err := mgr.Get(ctx, old.ID)
	require.NoError(t, err)
	assert.WithinDuration(t, before.Add(grace), oldStored.ExpiresAt, 5*time.Second)
}

func TestRotate_ZeroGraceCutsOver(t *testing.T) {
	t.Parallel()
	mgr := apikey.New(apikey.NewMemoryStore())
	ctx := context.Background()
	old, oldPlain, err := mgr.Create(ctx, apikey.CreateParams{Subject: "u1"})
	require.NoError(t, err)

	_, freshPlain, err := mgr.Rotate(ctx, old.ID, 0)
	require.NoError(t, err)

	_, err = mgr.Verify(ctx, oldPlain)
	assert.ErrorIs(t, err, apikey.ErrKeyExpired)
	_, err = mgr.Verify(ctx, freshPlain)
	assert.NoError(t, err)
}

func TestRotate_DeadKeysRejected(t *testing.T) {
	t.Parallel()
	store := apikey.NewMemoryStore()
	mgr := apikey.New(store)
	ctx := context.Background()

	revoked, _, err := mgr.Create(ctx, apikey.CreateParams{Subject: "u1"})
	require.NoError(t, err)
	require.NoError(t, mgr.Revoke(ctx, revoked.ID))
	_, _, err = mgr.Rotate(ctx, revoked.ID, time.Hour)
	assert.ErrorIs(t, err, apikey.ErrKeyRevoked)

	expired, _, err := mgr.Create(ctx, apikey.CreateParams{Subject: "u2"})
	require.NoError(t, err)
	require.NoError(t, store.Expire(ctx, expired.ID, time.Now().UTC().Add(-time.Minute)))
	_, _, err = mgr.Rotate(ctx, expired.ID, time.Hour)
	assert.ErrorIs(t, err, apikey.ErrKeyExpired)
}
```

Add `"time"` and `"github.com/dmitrymomot/forge/core/id"` to `manager_test.go` imports.

Create `auth/apikey/scope_test.go`:

```go
package apikey_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/apikey"
)

type tenantCtxKey struct{}

func scopeHook(ctx context.Context) (string, error) {
	t, _ := ctx.Value(tenantCtxKey{}).(string)
	return t, nil
}

func tenantCtx(tenant string) context.Context {
	return context.WithValue(context.Background(), tenantCtxKey{}, tenant)
}

func newScoped(t *testing.T) *apikey.Manager {
	t.Helper()
	return apikey.New(apikey.NewMemoryStore(), apikey.WithScope(scopeHook))
}

func TestScope_CreateStampsTenant(t *testing.T) {
	t.Parallel()
	mgr := newScoped(t)

	k, _, err := mgr.Create(tenantCtx("t1"), apikey.CreateParams{Subject: "u1"})
	require.NoError(t, err)
	assert.Equal(t, "t1", k.Tenant)

	// Matching explicit tenant is fine; conflicting one is not.
	_, _, err = mgr.Create(tenantCtx("t1"), apikey.CreateParams{Subject: "u1", Tenant: "t1"})
	assert.NoError(t, err)
	_, _, err = mgr.Create(tenantCtx("t1"), apikey.CreateParams{Subject: "u1", Tenant: "t2"})
	assert.ErrorIs(t, err, apikey.ErrTenantMismatch)
}

func TestScope_FailClosed(t *testing.T) {
	t.Parallel()
	// Empty tenant from the hook.
	mgr := newScoped(t)
	_, _, err := mgr.Create(context.Background(), apikey.CreateParams{Subject: "u1"})
	assert.ErrorIs(t, err, apikey.ErrScope)
	_, err = mgr.List(context.Background(), apikey.Filter{})
	assert.ErrorIs(t, err, apikey.ErrScope)

	// Hook error.
	boom := errors.New("boom")
	failing := apikey.New(apikey.NewMemoryStore(), apikey.WithScope(
		func(context.Context) (string, error) { return "", boom }))
	_, _, err = failing.Create(context.Background(), apikey.CreateParams{Subject: "u1"})
	assert.ErrorIs(t, err, apikey.ErrScope)
	assert.ErrorIs(t, err, boom)
}

func TestScope_CrossTenantReadsAsNotFound(t *testing.T) {
	t.Parallel()
	mgr := newScoped(t)
	k, _, err := mgr.Create(tenantCtx("t1"), apikey.CreateParams{Subject: "u1"})
	require.NoError(t, err)

	_, err = mgr.Get(tenantCtx("t2"), k.ID)
	assert.ErrorIs(t, err, apikey.ErrNotFound)
	assert.ErrorIs(t, mgr.Revoke(tenantCtx("t2"), k.ID), apikey.ErrNotFound)
	_, _, err = mgr.Rotate(tenantCtx("t2"), k.ID, time.Hour)
	assert.ErrorIs(t, err, apikey.ErrNotFound)

	// Same tenant still works.
	_, err = mgr.Get(tenantCtx("t1"), k.ID)
	assert.NoError(t, err)
}

func TestScope_ListConfined(t *testing.T) {
	t.Parallel()
	mgr := newScoped(t)
	_, _, err := mgr.Create(tenantCtx("t1"), apikey.CreateParams{Subject: "u1"})
	require.NoError(t, err)
	_, _, err = mgr.Create(tenantCtx("t2"), apikey.CreateParams{Subject: "u2"})
	require.NoError(t, err)

	keys, err := mgr.List(tenantCtx("t1"), apikey.Filter{})
	require.NoError(t, err)
	require.Len(t, keys, 1)
	assert.Equal(t, "t1", keys[0].Tenant)

	// A conflicting explicit filter tenant is rejected, not silently overridden.
	_, err = mgr.List(tenantCtx("t1"), apikey.Filter{Tenant: "t2"})
	assert.ErrorIs(t, err, apikey.ErrTenantMismatch)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./auth/apikey/`
Expected: FAIL — `mgr.Get undefined`, `mgr.Revoke undefined`, etc.

- [ ] **Step 3: Implement management ops + scope wiring**

Append to `auth/apikey/manager.go`, and change `Create`'s tenant line:

In `Create`, replace `Tenant: p.Tenant,` usage: insert before `plaintext := newKey(...)`:

```go
	tenant, err := m.scoped(ctx, p.Tenant)
	if err != nil {
		return Key{}, "", err
	}
```

and use `Tenant: tenant,` in the `Key{...}` literal.

Append:

```go
// scoped resolves the tenant a management operation is confined to. With
// no WithScope hook it passes the requested tenant through. Fail-closed:
// hook errors and empty scoped tenants abort the operation with ErrScope;
// an explicit requested tenant must be empty or equal to the scoped one.
func (m *Manager) scoped(ctx context.Context, requested string) (string, error) {
	if m.cfg.scope == nil {
		return requested, nil
	}
	t, err := m.cfg.scope(ctx)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrScope, err)
	}
	if t == "" {
		return "", ErrScope
	}
	if requested != "" && requested != t {
		return "", ErrTenantMismatch
	}
	return t, nil
}

// Get returns one key record. With WithScope configured, other tenants'
// keys read as ErrNotFound so existence cannot be probed across tenants.
func (m *Manager) Get(ctx context.Context, keyID id.UUID) (Key, error) {
	tenant, err := m.scoped(ctx, "")
	if err != nil {
		return Key{}, err
	}
	k, err := m.store.Get(ctx, keyID)
	if err != nil {
		return Key{}, err
	}
	if m.cfg.scope != nil && k.Tenant != tenant {
		return Key{}, ErrNotFound
	}
	return k, nil
}

// List returns keys matching f, newest first. With WithScope configured
// the filter is confined to the scoped tenant.
func (m *Manager) List(ctx context.Context, f Filter) ([]Key, error) {
	tenant, err := m.scoped(ctx, f.Tenant)
	if err != nil {
		return nil, err
	}
	f.Tenant = tenant
	return m.store.List(ctx, f)
}

// Revoke permanently disables a key. Revocation is terminal — a revoked
// key cannot be un-revoked or rotated.
func (m *Manager) Revoke(ctx context.Context, keyID id.UUID) error {
	if _, err := m.Get(ctx, keyID); err != nil {
		return err
	}
	return m.store.Revoke(ctx, keyID, time.Now().UTC())
}

// Rotate mints a replacement inheriting the old key's Subject, Tenant,
// Scopes, Name, and Meta (not its expiry), and expires the old key after
// grace (zero = immediate cutover). Both keys verify during the grace
// window. The replacement is created before the old key is expired so a
// failure cannot leave the caller without a working key.
func (m *Manager) Rotate(ctx context.Context, keyID id.UUID, grace time.Duration) (Key, string, error) {
	old, err := m.Get(ctx, keyID)
	if err != nil {
		return Key{}, "", err
	}
	now := time.Now().UTC()
	if !old.RevokedAt.IsZero() {
		return Key{}, "", ErrKeyRevoked
	}
	if !old.ExpiresAt.IsZero() && !old.ExpiresAt.After(now) {
		return Key{}, "", ErrKeyExpired
	}
	replacement, plaintext, err := m.Create(ctx, CreateParams{
		Name:    old.Name,
		Subject: old.Subject,
		Tenant:  old.Tenant,
		Scopes:  old.Scopes,
		Meta:    old.Meta,
	})
	if err != nil {
		return Key{}, "", err
	}
	if err := m.store.Expire(ctx, old.ID, now.Add(grace)); err != nil {
		return Key{}, "", fmt.Errorf("apikey: rotate: %w", err)
	}
	return replacement, plaintext, nil
}
```

- [ ] **Step 4: Run tests, fmt, verify pass**

Run: `just fmt ./auth/apikey/... && just test ./auth/apikey/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add auth/apikey/
git commit -m "feat(apikey): management ops (Get/List/Revoke/Rotate) and WithScope tenant isolation"
```

---

### Task 4: guard integration, doc.go, fuzz, benchmarks, catalog cleanup

**Files:**
- Create: `auth/apikey/doc.go`
- Create: `auth/apikey/integration_test.go`
- Create: `auth/apikey/fuzz_test.go`
- Create: `auth/apikey/bench_test.go`
- Modify: `docs/packages.md` (delete the `auth/apikey` roadmap entry)

**Interfaces:**
- Consumes: full Task 1–3 surface; `guard.New(v, ...guard.Option) middleware.Middleware`, `guard.WithExtractors`, `guard.BearerHeader()`, `guard.Header(name)`, `guard.MustFrom(ctx) guard.Identity`.
- Produces: nothing new — verification, docs, benchmarks.

- [ ] **Step 1: Write the guard integration test**

`auth/apikey/integration_test.go`:

```go
package apikey_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/apikey"
	"github.com/dmitrymomot/forge/auth/guard"
)

func TestGuardIntegration(t *testing.T) {
	t.Parallel()
	store := apikey.NewMemoryStore()
	mgr := apikey.New(store, apikey.WithPrefix("sk_live"))
	ctx := context.Background()
	k, plaintext, err := mgr.Create(ctx, apikey.CreateParams{Subject: "user_42", Tenant: "org_7"})
	require.NoError(t, err)

	authn := guard.New(mgr, guard.WithExtractors(guard.BearerHeader(), guard.Header("X-API-Key")))
	handler := authn(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := guard.MustFrom(r.Context())
		w.Header().Set("X-Subject", identity.Subject)
		w.WriteHeader(http.StatusOK)
	}))

	do := func(mutate func(*http.Request)) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api", nil)
		mutate(req)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	t.Run("no credential 401", func(t *testing.T) {
		rec := do(func(*http.Request) {})
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("bearer 200", func(t *testing.T) {
		rec := do(func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+plaintext) })
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "user_42", rec.Header().Get("X-Subject"))
	})

	t.Run("X-API-Key 200", func(t *testing.T) {
		rec := do(func(r *http.Request) { r.Header.Set("X-API-Key", plaintext) })
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("garbage 401", func(t *testing.T) {
		rec := do(func(r *http.Request) { r.Header.Set("X-API-Key", "sk_live_garbage") })
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("revoked 401", func(t *testing.T) {
		require.NoError(t, mgr.Revoke(ctx, k.ID))
		rec := do(func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+plaintext) })
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}
```

Note: the revoked subtest mutates shared state — keep the subtests in this order and do NOT mark them `t.Parallel()`.

- [ ] **Step 2: Write the fuzz test**

`auth/apikey/fuzz_test.go`:

```go
package apikey_test

import (
	"context"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/auth/apikey"
)

// FuzzVerify asserts the core property: no input other than the real
// plaintext ever authenticates, and no input panics the parser.
func FuzzVerify(f *testing.F) {
	store := apikey.NewMemoryStore()
	mgr := apikey.New(store, apikey.WithPrefix("sk"))
	_, plaintext, err := mgr.Create(context.Background(), apikey.CreateParams{Subject: "u1"})
	if err != nil {
		f.Fatal(err)
	}

	f.Add(plaintext)
	f.Add("")
	f.Add("sk_")
	f.Add("sk_" + strings.Repeat("a", 49))
	f.Add(plaintext[:len(plaintext)-1] + "!")

	f.Fuzz(func(t *testing.T, cred string) {
		identity, err := mgr.Verify(context.Background(), cred)
		if err == nil {
			if cred != plaintext {
				t.Fatalf("accepted forged credential %q", cred)
			}
			if identity.Subject != "u1" {
				t.Fatalf("wrong subject %q", identity.Subject)
			}
		}
	})
}
```

- [ ] **Step 3: Write benchmarks**

`auth/apikey/bench_test.go`:

```go
package apikey_test

import (
	"context"
	"testing"

	"github.com/dmitrymomot/forge/auth/apikey"
)

func BenchmarkCreate(b *testing.B) {
	mgr := apikey.New(apikey.NewMemoryStore(), apikey.WithPrefix("sk_live"))
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := mgr.Create(ctx, apikey.CreateParams{Subject: "u1"}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifyHit(b *testing.B) {
	// Touch disabled: measures the steady-state verify path.
	mgr := apikey.New(apikey.NewMemoryStore(), apikey.WithPrefix("sk_live"), apikey.WithTouchInterval(-1))
	ctx := context.Background()
	_, plaintext, err := mgr.Create(ctx, apikey.CreateParams{Subject: "u1"})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := mgr.Verify(ctx, plaintext); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifyMalformedReject(b *testing.B) {
	// The DoS-relevant path: checksum rejection without store access.
	// Target: zero allocations.
	mgr := apikey.New(apikey.NewMemoryStore(), apikey.WithPrefix("sk_live"))
	ctx := context.Background()
	_, plaintext, err := mgr.Create(ctx, apikey.CreateParams{Subject: "u1"})
	if err != nil {
		b.Fatal(err)
	}
	bad := plaintext[:len(plaintext)-1] + "!"
	b.ReportAllocs()
	for b.Loop() {
		if _, err := mgr.Verify(ctx, bad); err == nil {
			b.Fatal("expected rejection")
		}
	}
}
```

- [ ] **Step 4: Run integration/fuzz/bench**

Run: `just fmt ./auth/apikey/... && just test ./auth/apikey/`
Expected: PASS.

Run: `go test -run=NONE -fuzz=FuzzVerify -fuzztime=30s ./auth/apikey/`
Expected: no crashers.

Run: `just bench ./auth/apikey/`
Expected: benchmarks run; record ns/op + allocs/op for the PR description. `BenchmarkVerifyMalformedReject` should report 0 allocs/op (the `[]byte(payload)` conversion is non-escaping; if allocs appear, run the post-benchmark optimization pass required by repo rules).

- [ ] **Step 5: Write doc.go**

`auth/apikey/doc.go`:

```go
// Package apikey issues, manages, and verifies API keys: Stripe-style
// prefixed secrets (sk_live_x7Kp…) with a CRC32 checksum that rejects
// malformed credentials before any store access, SHA-256 hashes at rest,
// and the plaintext returned exactly once at creation. Management —
// create/list/revoke/rotate, per-key scopes, optional expiry, throttled
// last-used-at — runs behind the storage-agnostic Store seam;
// verification implements guard.Verifier.
//
// Personal and tenant-wide keys share one model: Subject is the principal
// the key acts as — a user id for personal keys, or a tenant or
// service-account id for keys owned by the org itself — and Tenant
// optionally pins the owning org.
//
//	store := apikey.NewMemoryStore() // pgstore.New(pool) in production
//	mgr := apikey.New(store, apikey.WithPrefix("sk_live"))
//
//	key, plaintext, err := mgr.Create(ctx, apikey.CreateParams{
//		Subject: "user_42", Tenant: "org_7", Name: "CI deploy",
//		Scopes:  []string{"deploy:write"},
//	})
//	// Show plaintext once; key.Preview ("sk_live_x7Kp") is what
//	// dashboards keep. Only the SHA-256 of the plaintext is stored.
//
//	authn := guard.New(mgr, guard.WithExtractors(guard.BearerHeader(), guard.Header("X-API-Key")))
//	mux.Handle("POST /api/deploy", authn(deployHandler))
//
// Scopes are carried into guard.Identity.Scopes but never enforced here —
// enforcement belongs to the authorization seam (auth/rbac).
//
// Multi-tenant applications confine management operations with WithScope;
// verification needs no hook because the key record itself resolves the
// tenant:
//
//	mgr := apikey.New(store, apikey.WithScope(func(ctx context.Context) (string, error) {
//		return tenantFromCtx(ctx), nil // fail-closed: empty or error aborts
//	}))
//
// Rotation overlaps old and new keys so consumers can deploy the new
// plaintext before the old one dies:
//
//	fresh, plaintext, err := mgr.Rotate(ctx, key.ID, 24*time.Hour)
package apikey
```

- [ ] **Step 6: Catalog cleanup**

In `docs/packages.md`: delete the whole `**auth/apikey**` entry (heading, description paragraph, `Deps:` line, and its `---` separator — around line 654). Then check remaining references:

Run: `grep -n "apikey" docs/packages.md`
Expected: only the `auth/scim` entry's "authenticated via `apikey` or `oauthserver` tokens" prose and the `web/hostrouter` "API-key-derived" prose remain — both describe capabilities, not planned-dep tags; leave them.

- [ ] **Step 7: Lint and commit**

Run: `just lint`
Expected: clean.

```bash
git add auth/apikey/ docs/packages.md
git commit -m "feat(apikey): guard integration test, fuzz, benchmarks, doc.go; drop shipped catalog entry"
```

---

### Task 5: Postgres driver (auth/apikey/pgstore)

**Files:**
- Create: `auth/apikey/pgstore/migrations/00001_create_forge_api_keys.sql`
- Create: `auth/apikey/pgstore/pgstore.go`
- Create: `auth/apikey/pgstore/doc.go`
- Test: `auth/apikey/pgstore/pgstore_test.go`

**Interfaces:**
- Consumes: `apikey.Store` seam + `apikey.Key/Filter` + sentinels (Task 1); `id.UUID` (pgx encodes/decodes `[16]byte`-underlying types as `uuid` natively); `data/migration` (`migration.New(fsys, migration.WithTable(...)).Up(ctx, db)`); `data/postgres` (`postgres.Open`, `postgres.DefaultConfig`) — test-only.
- Produces: `pgstore.New(pool *pgxpool.Pool) *Store` implementing `apikey.Store`; `pgstore.Migrations fs.FS`.

- [ ] **Step 1: Start ephemeral Postgres for live integration runs**

```bash
docker run --rm -d --name forge-apikey-pg -e POSTGRES_PASSWORD=forge -e POSTGRES_DB=forge -p 5499:5432 postgres:16-alpine
export FORGE_TEST_POSTGRES_DSN="postgres://postgres:forge@localhost:5499/forge?sslmode=disable"
```

Expected: container starts; `docker logs forge-apikey-pg` eventually shows "database system is ready to accept connections". (Stop with `docker stop forge-apikey-pg` at the end of the task.)

- [ ] **Step 2: Write the failing integration test**

`auth/apikey/pgstore/pgstore_test.go` — mirrors the `resilience/lock/pgstore` gating pattern; each test uses `t.Name()`-derived subjects/hashes for isolation on the shared table:

```go
package pgstore_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/apikey"
	"github.com/dmitrymomot/forge/auth/apikey/pgstore"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
)

var _ apikey.Store = (*pgstore.Store)(nil)

func newStore(t *testing.T) *pgstore.Store {
	t.Helper()
	dsn := os.Getenv("FORGE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set FORGE_TEST_POSTGRES_DSN")
	}
	cfg := postgres.DefaultConfig()
	cfg.URL = dsn
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, migration.New(pgstore.Migrations, migration.WithTable("forge_apikey_schema")).Up(context.Background(), db))
	return pgstore.New(pool)
}

// mkKey builds a record whose hash/subject/tenant are unique per call:
// the table persists across test runs, so deterministic values would
// collide on the unique hash index or inflate List counts on re-runs.
func mkKey(t *testing.T) apikey.Key {
	t.Helper()
	uid := id.NewUUID()
	return apikey.Key{
		ID:        uid,
		Hash:      "hash-" + uid.String(),
		Preview:   "key_preview1",
		Name:      "key-" + t.Name(),
		Subject:   "subj-" + uid.String(),
		Tenant:    "tenant-" + uid.String(),
		Scopes:    []string{"read", "write"},
		Meta:      map[string]string{"env": "prod"},
		CreatedAt: time.Now().UTC(),
	}
}

func TestPg_CreateGetRoundTrip(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	k := mkKey(t)
	k.ExpiresAt = time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond)
	require.NoError(t, s.Create(ctx, k))

	got, err := s.Get(ctx, k.ID)
	require.NoError(t, err)
	assert.Equal(t, k.ID, got.ID)
	assert.Equal(t, k.Hash, got.Hash)
	assert.Equal(t, k.Scopes, got.Scopes)
	assert.Equal(t, k.Meta, got.Meta)
	assert.True(t, got.ExpiresAt.Equal(k.ExpiresAt))
	// NULL ⇔ zero-time mapping.
	assert.True(t, got.LastUsedAt.IsZero())
	assert.True(t, got.RevokedAt.IsZero())

	byHash, err := s.GetByHash(ctx, k.Hash)
	require.NoError(t, err)
	assert.Equal(t, k.ID, byHash.ID)
}

func TestPg_NotFound(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	_, err := s.Get(ctx, id.NewUUID())
	assert.ErrorIs(t, err, apikey.ErrNotFound)
	_, err = s.GetByHash(ctx, "missing-"+id.NewUUID().String())
	assert.ErrorIs(t, err, apikey.ErrNotFound)
	assert.ErrorIs(t, s.Revoke(ctx, id.NewUUID(), time.Now()), apikey.ErrNotFound)
	assert.ErrorIs(t, s.Expire(ctx, id.NewUUID(), time.Now()), apikey.ErrNotFound)
	assert.ErrorIs(t, s.Touch(ctx, id.NewUUID(), time.Now()), apikey.ErrNotFound)
}

func TestPg_DuplicateHash(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	k := mkKey(t)
	require.NoError(t, s.Create(ctx, k))

	dup := mkKey(t)
	dup.Hash = k.Hash
	assert.ErrorIs(t, s.Create(ctx, dup), apikey.ErrDuplicate)

	dupID := mkKey(t)
	dupID.ID = k.ID
	assert.ErrorIs(t, s.Create(ctx, dupID), apikey.ErrDuplicate)
}

func TestPg_ListFilterAndOrder(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	k1, k2 := mkKey(t), mkKey(t)
	k2.Tenant = k1.Tenant
	// Deterministic id ordering: id.NewUUID() is NOT monotonic within one
	// millisecond, so same-ms ids sort randomly and would flake this test.
	// Share one id and vary only the last byte: k2 > k1 by construction.
	k2.ID = k1.ID
	k1.ID[15], k2.ID[15] = 0x01, 0x02
	require.NoError(t, s.Create(ctx, k1))
	require.NoError(t, s.Create(ctx, k2))

	all, err := s.List(ctx, apikey.Filter{Tenant: k1.Tenant})
	require.NoError(t, err)
	require.Len(t, all, 2)
	// Newest first = descending id bytes.
	assert.Equal(t, k2.ID, all[0].ID)

	one, err := s.List(ctx, apikey.Filter{Tenant: k1.Tenant, Subject: k1.Subject})
	require.NoError(t, err)
	require.Len(t, one, 1)
	assert.Equal(t, k1.ID, one[0].ID)
}

func TestPg_Mutators(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	k := mkKey(t)
	require.NoError(t, s.Create(ctx, k))
	at := time.Now().UTC().Truncate(time.Millisecond)

	require.NoError(t, s.Revoke(ctx, k.ID, at))
	require.NoError(t, s.Expire(ctx, k.ID, at.Add(time.Hour)))
	require.NoError(t, s.Touch(ctx, k.ID, at.Add(time.Minute)))

	got, err := s.Get(ctx, k.ID)
	require.NoError(t, err)
	assert.True(t, got.RevokedAt.Equal(at))
	assert.True(t, got.ExpiresAt.Equal(at.Add(time.Hour)))
	assert.True(t, got.LastUsedAt.Equal(at.Add(time.Minute)))
}

func TestPg_ManagerEndToEnd(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	mgr := apikey.New(s, apikey.WithPrefix("sk_pg"))

	k, plaintext, err := mgr.Create(ctx, apikey.CreateParams{Subject: t.Name() + "-u", Tenant: t.Name() + "-t"})
	require.NoError(t, err)

	identity, err := mgr.Verify(ctx, plaintext)
	require.NoError(t, err)
	assert.Equal(t, t.Name()+"-u", identity.Subject)

	fresh, freshPlain, err := mgr.Rotate(ctx, k.ID, time.Hour)
	require.NoError(t, err)
	_, err = mgr.Verify(ctx, plaintext) // old still inside grace
	require.NoError(t, err)
	_, err = mgr.Verify(ctx, freshPlain)
	require.NoError(t, err)

	require.NoError(t, mgr.Revoke(ctx, fresh.ID))
	_, err = mgr.Verify(ctx, freshPlain)
	assert.ErrorIs(t, err, apikey.ErrKeyRevoked)
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./auth/apikey/pgstore/`
Expected: FAIL — compile errors (`undefined: pgstore.New`, no package).

- [ ] **Step 4: Implement migration + driver**

`auth/apikey/pgstore/migrations/00001_create_forge_api_keys.sql`:

```sql
-- +goose Up
CREATE TABLE forge_api_keys (
    id           uuid PRIMARY KEY,
    hash         text NOT NULL UNIQUE,
    preview      text NOT NULL,
    name         text NOT NULL DEFAULT '',
    subject      text NOT NULL,
    tenant       text NOT NULL DEFAULT '',
    scopes       text[] NOT NULL DEFAULT '{}',
    meta         jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at   timestamptz NOT NULL,
    expires_at   timestamptz,
    last_used_at timestamptz,
    revoked_at   timestamptz
);

CREATE INDEX forge_api_keys_list_idx ON forge_api_keys (tenant, subject, id DESC);

-- +goose Down
DROP TABLE forge_api_keys;
```

`auth/apikey/pgstore/doc.go`:

```go
// Package pgstore is the Postgres apikey.Store driver over pgx. The DDL
// (forge_api_keys) ships as an embedded goose migration in Migrations;
// apply it via data/migration under its own version table before first
// use:
//
//	_ = migration.New(pgstore.Migrations, migration.WithTable("forge_apikey_schema")).Up(ctx, db)
//
// GetByHash — the verification hot path — is a single point lookup on the
// unique hash index. Zero time.Time fields map to SQL NULL and back.
package pgstore
```

`auth/apikey/pgstore/pgstore.go`:

```go
package pgstore

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/auth/apikey"
	"github.com/dmitrymomot/forge/core/id"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations holds the goose migration creating forge_api_keys, rooted so
// its .sql files sit at fsys root (data/migration.New globs fsys's root,
// not subdirectories). Apply via data/migration under its own version
// table ("forge_apikey_schema").
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(err) // unreachable: migrations/*.sql is embedded at compile time
	}
	Migrations = sub
}

// Store is the Postgres implementation of apikey.Store. The pool's
// lifecycle is the caller's.
type Store struct {
	pool *pgxpool.Pool
}

var _ apikey.Store = (*Store)(nil)

// New builds a Postgres apikey Store. Apply Migrations first.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

const cols = `id, hash, preview, name, subject, tenant, scopes, meta, created_at, expires_at, last_used_at, revoked_at`

const createSQL = `
INSERT INTO forge_api_keys (` + cols + `)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`

// Create inserts k. A colliding id or hash yields apikey.ErrDuplicate.
func (s *Store) Create(ctx context.Context, k apikey.Key) error {
	scopes := k.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	meta := k.Meta
	if meta == nil {
		meta = map[string]string{}
	}
	_, err := s.pool.Exec(ctx, createSQL,
		k.ID, k.Hash, k.Preview, k.Name, k.Subject, k.Tenant, scopes, meta,
		k.CreatedAt, nullTime(k.ExpiresAt), nullTime(k.LastUsedAt), nullTime(k.RevokedAt))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		return apikey.ErrDuplicate
	}
	return err
}

// Get returns the record for keyID, or apikey.ErrNotFound.
func (s *Store) Get(ctx context.Context, keyID id.UUID) (apikey.Key, error) {
	return scanKey(s.pool.QueryRow(ctx, `SELECT `+cols+` FROM forge_api_keys WHERE id = $1`, keyID))
}

// GetByHash returns the record whose hash matches, or apikey.ErrNotFound.
// This is the verification hot path: one point lookup on the unique index.
func (s *Store) GetByHash(ctx context.Context, hash string) (apikey.Key, error) {
	return scanKey(s.pool.QueryRow(ctx, `SELECT `+cols+` FROM forge_api_keys WHERE hash = $1`, hash))
}

// List returns records matching f, newest first (UUIDv7 ids are
// time-ordered, so id DESC is creation order).
func (s *Store) List(ctx context.Context, f apikey.Filter) ([]apikey.Key, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+cols+` FROM forge_api_keys
		 WHERE ($1 = '' OR tenant = $1) AND ($2 = '' OR subject = $2)
		 ORDER BY id DESC`, f.Tenant, f.Subject)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []apikey.Key
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// Revoke sets revoked_at, or returns apikey.ErrNotFound.
func (s *Store) Revoke(ctx context.Context, keyID id.UUID, at time.Time) error {
	return s.setTime(ctx, `UPDATE forge_api_keys SET revoked_at = $2 WHERE id = $1`, keyID, at)
}

// Expire sets expires_at (rotation grace), or returns apikey.ErrNotFound.
func (s *Store) Expire(ctx context.Context, keyID id.UUID, at time.Time) error {
	return s.setTime(ctx, `UPDATE forge_api_keys SET expires_at = $2 WHERE id = $1`, keyID, at)
}

// Touch sets last_used_at, or returns apikey.ErrNotFound.
func (s *Store) Touch(ctx context.Context, keyID id.UUID, at time.Time) error {
	return s.setTime(ctx, `UPDATE forge_api_keys SET last_used_at = $2 WHERE id = $1`, keyID, at)
}

func (s *Store) setTime(ctx context.Context, sql string, keyID id.UUID, at time.Time) error {
	tag, err := s.pool.Exec(ctx, sql, keyID, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apikey.ErrNotFound
	}
	return nil
}

// row is satisfied by both pgx.Row and pgx.Rows.
type row interface{ Scan(dest ...any) error }

func scanKey(r row) (apikey.Key, error) {
	var k apikey.Key
	var exp, lu, rv *time.Time
	err := r.Scan(&k.ID, &k.Hash, &k.Preview, &k.Name, &k.Subject, &k.Tenant,
		&k.Scopes, &k.Meta, &k.CreatedAt, &exp, &lu, &rv)
	if errors.Is(err, pgx.ErrNoRows) {
		return apikey.Key{}, apikey.ErrNotFound
	}
	if err != nil {
		return apikey.Key{}, err
	}
	k.ExpiresAt = deref(exp)
	k.LastUsedAt = deref(lu)
	k.RevokedAt = deref(rv)
	return k, nil
}

// deref maps SQL NULL back to the zero time.
func deref(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// nullTime maps the zero time to SQL NULL.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
```

- [ ] **Step 5: Run integration tests live**

Run: `just fmt ./auth/apikey/... && FORGE_TEST_POSTGRES_DSN="postgres://postgres:forge@localhost:5499/forge?sslmode=disable" go test -race ./auth/apikey/pgstore/`
Expected: PASS (all tests run, none skip).

Also verify the skip path: `go test ./auth/apikey/pgstore/` without the env var → tests skip, exit 0.

- [ ] **Step 6: Lint, stop container, commit**

Run: `just lint`
Expected: clean.

```bash
docker stop forge-apikey-pg
git add auth/apikey/pgstore/
git commit -m "feat(apikey/pgstore): Postgres Store driver with embedded goose migration"
```

---

## Final verification (after all tasks)

- [ ] `just test ./auth/apikey/...` — all green with race detector.
- [ ] `just lint` — clean.
- [ ] `just bench ./auth/apikey/` — numbers recorded for the PR description (repo rule: before/after if any optimization pass happened).
- [ ] `grep -rn "apikey" docs/packages.md` — no stale roadmap entry.
- [ ] PR flow per CLAUDE.md: create PR → wait for CI → fix failures → address Claude review → repeat until clean. Note: claude-code-review.yml times out silently on big PRs — run a local whole-branch review regardless.
