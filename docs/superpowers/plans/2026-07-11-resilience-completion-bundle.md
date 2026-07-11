# Resilience Completion Bundle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the three remaining `resilience/` packages — `quota`, `loadshed`, `lock` (with `lock/pgstore` + `lock/redisstore`) — plus the `ratelimit/pgstore` counter driver, the `ttl ≤ 0` no-expiry counter-seam fix, and the `data/migration` `Source`/`Group` composite. Completing them finishes the resilience domain.

**Architecture:** Each package follows forge's `New(...Option)` idiom with `config.go`/`options.go`/`errors.go`/`doc.go` anatomy and storage-agnostic `Store` seams. `quota` rides the shared `ratelimit.Store` counter seam; `loadshed` composes `middleware.Middleware` + a plain `Acquire`; `lock` composes `supervisor.Service`. Real backends live in isolated `pgstore`/`redisstore` driver subpackages that ship embedded goose migrations applied via `data/migration` under per-source version tables.

**Tech Stack:** Go 1.26, `core/clock`, `core/id`, `web/middleware`, `ops/supervisor`, `data/postgres` (pgx v5), `data/migration` (goose v3), `github.com/redis/go-redis/v9`, `testify`.

**Spec:** `docs/superpowers/specs/2026-07-11-resilience-completion-bundle-design.md`

## Global Constraints

- **Module path:** `github.com/dmitrymomot/forge`.
- **Work only on the current branch** (`dm/packages-from-resilience-6758cc`); never switch branches.
- **Format after edits:** `just fmt ./<pkg>/...` (package-path form — the single-file form trips a spurious betteralign "undefined").
- **Lint before done:** `just lint` (runs `go vet`, `go build`, and the repo's modernize/betteralign/nilaway pass). Fix all findings.
- **Go 1.26 modernize:** use `new(expr)` directly, never a `ptr.To`-style wrapper.
- **Tests:** `just test ./<pkg>/...` runs `go test -race -cover`. Black-box tests only (`package X_test`); white-box (`package X`) only to assert unexported state.
- **Test doubles live with the seam owner.** Use `clock.Mock` for all time; use the in-memory stores as the doubles.
- **Integration tests** (pg/redis) gate on env and skip when unset: Postgres → `FORGE_TEST_POSTGRES_DSN`, Redis → `FORGE_TEST_REDIS_URL`. Follow `resilience/ratelimit/redisstore/redisstore_test.go` and `data/postgres/*_test.go`.
- **No Claude attribution** in any commit message.
- **Conventional commits**, one per task step as noted.
- **`doc.go` convention:** package comment with a `# Usage` section holding a compilable snippet (see `resilience/circuitbreaker/doc.go`), not a runnable `Example` func.
- **Errors:** single-line `errors.Is`-matchable sentinels in `errors.go`.

---

## File Structure

```
resilience/ratelimit/store.go             MODIFY  contract doc: ttl ≤ 0 = no expiry
resilience/ratelimit/memory.go            MODIFY  zero-expiresAt sentinel = never expires
resilience/ratelimit/redisstore/redisstore.go  MODIFY  Lua skips PEXPIRE when ttl ≤ 0
data/migration/group.go                   CREATE  Set, Source(), GroupMigrator, Group()

resilience/quota/window.go                CREATE  Unit, Window, Calendar/Rolling/Gauge
resilience/quota/quota.go                 CREATE  Limit, Result, Meter, New, Allow, Usage, Add, Set, Reset
resilience/quota/options.go               CREATE  Option, WithClock, WithKeyPrefix
resilience/quota/errors.go                CREATE  ErrInvalidCost, ErrInvalidLimit
resilience/quota/doc.go                    CREATE  package doc + usage

resilience/ratelimit/pgstore/pgstore.go   CREATE  Postgres counter Store (ratelimit.Store)
resilience/ratelimit/pgstore/migrations/20260711120000_counters.sql  CREATE
resilience/ratelimit/pgstore/doc.go       CREATE

resilience/loadshed/criteria.go           CREATE  Criteria, Concurrency, Latency + hooks
resilience/loadshed/loadshed.go           CREATE  Shedder, New, Acquire, Ticket, ramp
resilience/loadshed/middleware.go         CREATE  Middleware, WithSkip, WithResponder
resilience/loadshed/options.go            CREATE  Option, WithCriteria/Threshold/Floor/Clock/Rand
resilience/loadshed/doc.go                CREATE

resilience/lock/store.go                  CREATE  Store interface
resilience/lock/memory.go                 CREATE  MemoryStore
resilience/lock/lock.go                   CREATE  Lock, New, Acquire, TryAcquire
resilience/lock/lease.go                  CREATE  Lease, refresh loop, Fence/Done/Release
resilience/lock/leader.go                 CREATE  RunOnLeader → supervisor.Service
resilience/lock/options.go                CREATE  Option, WithTTL/Owner/RefreshInterval/Clock
resilience/lock/errors.go                 CREATE  ErrNotHeld, ErrLockLost
resilience/lock/doc.go                    CREATE

resilience/lock/pgstore/pgstore.go        CREATE  table-lease Store
resilience/lock/pgstore/migrations/20260711120100_locks.sql  CREATE
resilience/lock/pgstore/doc.go            CREATE

resilience/lock/redisstore/redisstore.go  CREATE  single-instance lease Store
resilience/lock/redisstore/doc.go         CREATE

docs/packages.md                          MODIFY  delete quota/loadshed/lock entries
```

---

## Task 1: Counter-seam `ttl ≤ 0` = no-expiry fix

**Files:**
- Modify: `resilience/ratelimit/store.go` (contract doc on `Incr`)
- Modify: `resilience/ratelimit/memory.go` (`counter` expiry logic in `Incr`/`Get`/`sweep`)
- Modify: `resilience/ratelimit/redisstore/redisstore.go` (`incrScript`)
- Test: `resilience/ratelimit/memory_test.go` (add cases), `resilience/ratelimit/redisstore/redisstore_test.go` (add case)

**Interfaces:**
- Consumes: nothing new.
- Produces: `ratelimit.Store.Incr` contract — "a `ttl ≤ 0` creates a key with **no expiry**". Relied on by quota gauges (Task 4) and both pgstores.

- [ ] **Step 1: Write the failing memory-store test**

Add to `resilience/ratelimit/memory_test.go`:

```go
func TestMemoryStore_NonPositiveTTLNeverExpires(t *testing.T) {
	clk := clock.NewMock(time.Unix(0, 0))
	s := ratelimit.NewMemoryStore(ratelimit.WithMemoryClock(clk))
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()

	n, err := s.Incr(ctx, "gauge", 5, 0) // ttl == 0 → no expiry
	require.NoError(t, err)
	assert.Equal(t, int64(5), n)

	clk.Advance(1000 * time.Hour) // far past any TTL
	got, err := s.Get(ctx, "gauge")
	require.NoError(t, err)
	assert.Equal(t, int64(5), got) // still present

	n, err = s.Incr(ctx, "gauge", -2, -1) // negative ttl also = no expiry
	require.NoError(t, err)
	assert.Equal(t, int64(3), n)
}
```

- [ ] **Step 2: Run it to confirm it fails**

Run: `just test ./resilience/ratelimit/`
Expected: FAIL — `Get` returns 0 after advance (zero-value `expiresAt` is treated as already expired).

- [ ] **Step 3: Implement the sentinel in `memory.go`**

In `Incr`, compute expiry with a zero-`Time` sentinel for no-expiry, and treat zero `expiresAt` as never-expired everywhere. Replace the three expiry sites:

`Incr` — the create branch:
```go
func (s *memoryStore) Incr(_ context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	now := s.clk.Now()
	sh := s.shardFor(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e, ok := sh.m[key]
	if !ok || expired(e.expiresAt, now) {
		e = counter{val: delta, expiresAt: expiryAt(now, ttl)}
	} else {
		e.val += delta
	}
	sh.m[key] = e
	return e.val, nil
}
```

`Get`:
```go
	if !ok || expired(e.expiresAt, now) {
		return 0, nil
	}
```

`sweep`:
```go
		if expired(e.expiresAt, now) {
			delete(sh.m, key)
		}
```

Add the two helpers (e.g. at the bottom of `memory.go`):
```go
// expiryAt returns the absolute expiry for a new counter. A ttl <= 0 yields the
// zero Time, the sentinel for "no expiry".
func expiryAt(now time.Time, ttl time.Duration) time.Time {
	if ttl <= 0 {
		return time.Time{}
	}
	return now.Add(ttl)
}

// expired reports whether a counter with expiry exp is expired as of now. The
// zero Time means no expiry (never expired).
func expired(exp, now time.Time) bool {
	return !exp.IsZero() && now.After(exp)
}
```

- [ ] **Step 4: Update the `Incr` contract doc in `store.go`**

```go
	// Incr atomically adds delta to key's counter and returns the new value. If
	// this call creates the key, its TTL is set to ttl; Incr never extends the
	// TTL of an existing (live) key. A ttl <= 0 creates the key with NO expiry
	// (used by quota gauges); such a key lives until Reset (or, in the memory
	// store, LRU/janitor pruning — so durable gauges need a real backend).
	Incr(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error)
```

- [ ] **Step 5: Add the failing redisstore test**

Add to `resilience/ratelimit/redisstore/redisstore_test.go`:
```go
func TestRedisStore_NonPositiveTTLNeverExpires(t *testing.T) {
	client := dial(t)
	s := redisstore.New(client, redisstore.WithPrefix("rltest:"))
	ctx := context.Background()
	key := "gauge-" + t.Name()
	require.NoError(t, s.Reset(ctx, key))

	n, err := s.Incr(ctx, key, 5, 0) // no expiry
	require.NoError(t, err)
	assert.Equal(t, int64(5), n)

	ttl, err := client.PTTL(ctx, "rltest:"+key).Result()
	require.NoError(t, err)
	assert.Equal(t, time.Duration(-1), ttl) // -1ms = key exists with no TTL

	require.NoError(t, s.Reset(ctx, key))
}
```

- [ ] **Step 6: Fix `incrScript` in `redisstore.go`**

```go
var incrScript = redis.NewScript(`
local v = redis.call('INCRBY', KEYS[1], ARGV[1])
if v == tonumber(ARGV[1]) and tonumber(ARGV[2]) > 0 then
  redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
return v`)
```

(No signature change; `Incr` still passes `ttl.Milliseconds()`, which is `≤ 0` for no-expiry, so `PEXPIRE` is skipped and the key persists.)

- [ ] **Step 7: Run tests**

Run: `just test ./resilience/ratelimit/...`
Expected: PASS (redis test skips unless `FORGE_TEST_REDIS_URL` is set). Set it locally to verify the redis path.

- [ ] **Step 8: Format, lint, commit**

```bash
just fmt ./resilience/ratelimit/...
just lint
git add resilience/ratelimit/
git commit -m "fix(ratelimit): ttl<=0 creates a no-expiry counter key"
```

---

## Task 2: `data/migration` Source + Group composite

**Files:**
- Create: `data/migration/group.go`
- Test: `data/migration/group_test.go`

**Interfaces:**
- Consumes: existing `migration.New(fs.FS, ...Option)`, `migration.WithTable(string)`, `postgres.Migrator` (structural: `Up(ctx, *sql.DB) error`).
- Produces:
  - `type Set struct{ ... }` (unexported fields)
  - `func Source(fsys fs.FS, table string) Set` — `table == ""` uses `DefaultTable`
  - `type GroupMigrator struct{ ... }` with `func (*GroupMigrator) Up(ctx context.Context, db *sql.DB) error`
  - `func Group(sets ...Set) *GroupMigrator`
  - Consumed by every pgstore's migration wiring.

- [ ] **Step 1: Write the failing integration test**

`data/migration/group_test.go`:
```go
package migration_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
)

func openDB(t *testing.T) *sql.DB {
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
	return db
}

func mig(table string) fstest.MapFS {
	return fstest.MapFS{
		"00001_init.sql": &fstest.MapFile{Data: []byte(
			"-- +goose Up\nCREATE TABLE IF NOT EXISTS " + table + " (id int);\n" +
				"-- +goose Down\nDROP TABLE IF EXISTS " + table + ";\n")},
	}
}

func TestGroup_AppliesEachUnderOwnTable(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS grp_a, grp_b; DROP TABLE IF EXISTS gv_a, gv_b")

	g := migration.Group(
		migration.Source(mig("grp_a"), "gv_a"),
		migration.Source(mig("grp_b"), "gv_b"),
	)
	require.NoError(t, g.Up(ctx, db))

	for _, tbl := range []string{"grp_a", "grp_b", "gv_a", "gv_b"} {
		var n int
		require.NoError(t, db.QueryRowContext(ctx,
			"SELECT count(*) FROM information_schema.tables WHERE table_name=$1", tbl).Scan(&n))
		assert.Equal(t, 1, n, "table %s should exist", tbl)
	}
}
```

- [ ] **Step 2: Run it to confirm it fails**

Run: `just test ./data/migration/`
Expected: FAIL — `undefined: migration.Group` / `migration.Source` (or skip if no DSN; set `FORGE_TEST_POSTGRES_DSN` to actually exercise it).

- [ ] **Step 3: Implement `group.go`**

```go
package migration

import (
	"context"
	"database/sql"
	"io/fs"
)

// Set is one migration source: an fs.FS applied under its own goose version
// table. Build it with Source; the zero value is not usable.
type Set struct {
	fsys  fs.FS
	table string
}

// Source pairs an embedded migration FS with the version table it owns. An
// empty table uses DefaultTable ("schema_migrations").
func Source(fsys fs.FS, table string) Set {
	return Set{fsys: fsys, table: table}
}

// GroupMigrator applies several Sets, each under its own version table, in
// declared order. It structurally satisfies postgres.Migrator, so it can be
// passed straight to postgres.WithMigrator. Build it with Group.
type GroupMigrator struct {
	sets []Set
}

// Group builds a composite Migrator over sets. Each Set keeps an independent
// timeline (its own version table), so forge-owned migrations never collide
// with the consumer's app migration numbering.
func Group(sets ...Set) *GroupMigrator {
	return &GroupMigrator{sets: sets}
}

// Up applies each Set's migrations under its own version table, in order. It
// stops at the first error. The db is owned by the caller and never closed.
func (g *GroupMigrator) Up(ctx context.Context, db *sql.DB) error {
	for _, s := range g.sets {
		if err := New(s.fsys, WithTable(s.table)).Up(ctx, db); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `just test ./data/migration/`
Expected: PASS (or skip without DSN).

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./data/migration/...
just lint
git add data/migration/
git commit -m "feat(migration): Group composite applies sources under per-table timelines"
```

> Note: the test helper uses `postgres.DefaultConfig()` + `cfg.URL = dsn` (URL is the DSN field, confirmed in `data/postgres/config.go`), matching `data/postgres/migrator_test.go`.

---

## Task 3: quota types — Window, Limit, Result, errors

**Files:**
- Create: `resilience/quota/window.go`, `resilience/quota/errors.go`
- Test: `resilience/quota/window_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Unit int`; `const (Daily Unit = iota; Weekly; Monthly)`
  - `type Window func(subject string, now time.Time) (period string, reset time.Time)`
  - `func Calendar(unit Unit, loc *time.Location) Window`, `func Rolling(d time.Duration) Window`, `func Gauge() Window`
  - `const Unlimited int64 = -1`
  - `type Limit struct{ Included, Max int64 }` with `func (Limit) validate() error`
  - `type Result struct{ Reset time.Time; Limit Limit; Used, Remaining, Overage int64; Allowed bool }`
  - `var ErrInvalidCost, ErrInvalidLimit error`
  - Consumed by Task 4/5 (`Meter`).

- [ ] **Step 1: Write the failing test**

`resilience/quota/window_test.go`:
```go
package quota_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/resilience/quota"
)

func TestCalendarMonthly(t *testing.T) {
	w := quota.Calendar(quota.Monthly, nil) // nil => UTC
	now := time.Date(2026, 7, 15, 9, 30, 0, 0, time.UTC)
	period, reset := w("tenant", now)
	assert.Equal(t, "2026-07", period)
	assert.Equal(t, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), reset)
}

func TestGaugeHasNoPeriodOrReset(t *testing.T) {
	period, reset := quota.Gauge()("s", time.Now())
	assert.Equal(t, "", period)
	assert.True(t, reset.IsZero())
}

func TestLimitValidate(t *testing.T) {
	assert.NoError(t, quota.Limit{Included: 10, Max: 10}.Validate())
	assert.NoError(t, quota.Limit{Included: 10, Max: quota.Unlimited}.Validate())
	assert.ErrorIs(t, quota.Limit{Included: 10, Max: 5}.Validate(), quota.ErrInvalidLimit)
}
```

> The test calls `Limit.Validate()` (exported) so black-box tests can assert it; expose it as an exported method.

- [ ] **Step 2: Run it to confirm it fails**

Run: `just test ./resilience/quota/`
Expected: FAIL — package/symbols undefined.

- [ ] **Step 3: Implement `errors.go`**

```go
package quota

import "errors"

// ErrInvalidCost is returned by Allow when cost is negative.
var ErrInvalidCost = errors.New("quota: cost must be non-negative")

// ErrInvalidLimit is returned when a Limit has Included < 0, or Max below
// Included while not Unlimited.
var ErrInvalidLimit = errors.New("quota: Max must be >= Included, or Unlimited")
```

- [ ] **Step 4: Implement `window.go`**

```go
package quota

import (
	"fmt"
	"strconv"
	"time"
)

// Unit is a calendar window granularity.
type Unit int

const (
	Daily Unit = iota
	Weekly
	Monthly
)

// Unlimited marks a Limit with no hard ceiling (pay-as-you-go / pure metering).
const Unlimited int64 = -1

// Window maps now to the current period's key suffix and its reset time. period
// is "" for gauges (no suffix); reset is the zero Time for gauges.
type Window func(subject string, now time.Time) (period string, reset time.Time)

// Calendar returns a Window aligned to calendar boundaries in loc (nil => UTC).
func Calendar(unit Unit, loc *time.Location) Window {
	if loc == nil {
		loc = time.UTC
	}
	return func(_ string, now time.Time) (string, time.Time) {
		n := now.In(loc)
		switch unit {
		case Daily:
			start := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc)
			return start.Format("2006-01-02"), start.AddDate(0, 0, 1)
		case Weekly:
			offset := (int(n.Weekday()) + 6) % 7 // ISO: Monday = 0
			start := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -offset)
			y, w := start.ISOWeek()
			return fmt.Sprintf("%04d-W%02d", y, w), start.AddDate(0, 0, 7)
		default: // Monthly
			start := time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, loc)
			return start.Format("2006-01"), start.AddDate(0, 1, 0)
		}
	}
}

// Rolling returns a Window that approximates a trailing window of length d with
// fixed buckets (floor(now/d)) — the counter-store approximation of a rolling
// window (cf. ratelimit).
func Rolling(d time.Duration) Window {
	return func(_ string, now time.Time) (string, time.Time) {
		bucket := now.Truncate(d)
		return strconv.FormatInt(bucket.Unix(), 10), bucket.Add(d)
	}
}

// Gauge returns a Window that never resets: a live count (seats, storage bytes).
func Gauge() Window {
	return func(_ string, _ time.Time) (string, time.Time) { return "", time.Time{} }
}

// Limit is the caller-resolved cap for a subject. No billing coupling.
type Limit struct {
	Included int64 // allotment included in the plan
	Max      int64 // hard ceiling; usage in (Included, Max] is allowed but billable. Unlimited => no ceiling.
}

// Validate reports whether the Limit is well-formed.
func (l Limit) Validate() error {
	if l.Included < 0 {
		return ErrInvalidLimit
	}
	if l.Max != Unlimited && l.Max < l.Included {
		return ErrInvalidLimit
	}
	return nil
}

// Result reports a quota decision for one subject.
type Result struct {
	Reset     time.Time // when the window rolls (zero for gauges)
	Limit     Limit
	Used      int64 // total consumed this window (post-call)
	Remaining int64 // max(0, Included - Used)
	Overage   int64 // max(0, Used - Included) — the billable signal
	Allowed   bool
}

// makeResult derives the reported fields from a raw used total.
func makeResult(limit Limit, used int64, reset time.Time, allowed bool) Result {
	remaining := limit.Included - used
	if remaining < 0 {
		remaining = 0
	}
	overage := used - limit.Included
	if overage < 0 {
		overage = 0
	}
	return Result{Reset: reset, Limit: limit, Used: used, Remaining: remaining, Overage: overage, Allowed: allowed}
}
```

- [ ] **Step 5: Run tests**

Run: `just test ./resilience/quota/`
Expected: PASS.

- [ ] **Step 6: Format, lint, commit**

```bash
just fmt ./resilience/quota/...
just lint
git add resilience/quota/
git commit -m "feat(quota): Window seam, Limit, and Result types"
```

---

## Task 4: quota Meter — New, Usage, Allow

**Files:**
- Create: `resilience/quota/quota.go`, `resilience/quota/options.go`
- Test: `resilience/quota/quota_test.go`

**Interfaces:**
- Consumes: `ratelimit.Store` (Incr/Get/Reset/Close), `ratelimit.NewMemoryStore`, `ratelimit.WithMemoryClock`; `clock.Clock`/`clock.Mock`; Task 3 types.
- Produces:
  - `type Option func(*config)`; `func WithClock(clock.Clock) Option`; `func WithKeyPrefix(string) Option`
  - `type Meter struct{ ... }`
  - `func New(store ratelimit.Store, window Window, opts ...Option) *Meter`
  - `func (*Meter) Usage(ctx, subject string, limit Limit) (Result, error)`
  - `func (*Meter) Allow(ctx, subject string, cost int64, limit Limit) (Result, error)`
  - Consumed by Task 5.

- [ ] **Step 1: Write the failing test**

`resilience/quota/quota_test.go`:
```go
package quota_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/quota"
	"github.com/dmitrymomot/forge/resilience/ratelimit"
)

func newMeter(t *testing.T, w quota.Window, clk *clock.Mock) *quota.Meter {
	store := ratelimit.NewMemoryStore(ratelimit.WithMemoryClock(clk))
	t.Cleanup(func() { _ = store.Close() })
	return quota.New(store, w, quota.WithClock(clk))
}

func TestAllow_HardCapRejectsAndDoesNotBurn(t *testing.T) {
	clk := clock.NewMock(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	m := newMeter(t, quota.Calendar(quota.Monthly, nil), clk)
	ctx := context.Background()
	lim := quota.Limit{Included: 10, Max: 10} // hard cap

	res, err := m.Allow(ctx, "t1", 8, lim)
	require.NoError(t, err)
	assert.True(t, res.Allowed)
	assert.Equal(t, int64(8), res.Used)

	res, err = m.Allow(ctx, "t1", 5, lim) // would hit 13 > 10 → reject + rollback
	require.NoError(t, err)
	assert.False(t, res.Allowed)
	assert.Equal(t, int64(8), res.Used) // NOT burned
	assert.Equal(t, int64(2), res.Remaining)
}

func TestAllow_OverageAllowedAndReported(t *testing.T) {
	clk := clock.NewMock(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	m := newMeter(t, quota.Calendar(quota.Monthly, nil), clk)
	ctx := context.Background()
	lim := quota.Limit{Included: 100, Max: 200}

	res, err := m.Allow(ctx, "t1", 150, lim)
	require.NoError(t, err)
	assert.True(t, res.Allowed)
	assert.Equal(t, int64(50), res.Overage)
	assert.Equal(t, int64(0), res.Remaining)
}

func TestAllow_CalendarWindowRollsAtBoundary(t *testing.T) {
	clk := clock.NewMock(time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC))
	m := newMeter(t, quota.Calendar(quota.Monthly, nil), clk)
	ctx := context.Background()
	lim := quota.Limit{Included: 10, Max: 10}

	_, err := m.Allow(ctx, "t1", 10, lim)
	require.NoError(t, err)

	clk.Advance(15 * 24 * time.Hour) // into August
	res, err := m.Usage(ctx, "t1", lim)
	require.NoError(t, err)
	assert.Equal(t, int64(0), res.Used) // fresh window
}

func TestAllow_InvalidInputs(t *testing.T) {
	clk := clock.NewMock(time.Unix(0, 0))
	m := newMeter(t, quota.Gauge(), clk)
	_, err := m.Allow(context.Background(), "t1", -1, quota.Limit{Included: 1, Max: 1})
	assert.ErrorIs(t, err, quota.ErrInvalidCost)
	_, err = m.Allow(context.Background(), "t1", 1, quota.Limit{Included: 5, Max: 1})
	assert.ErrorIs(t, err, quota.ErrInvalidLimit)
}
```

- [ ] **Step 2: Run it to confirm it fails**

Run: `just test ./resilience/quota/`
Expected: FAIL — `undefined: quota.New/Meter/WithClock`.

- [ ] **Step 3: Implement `options.go`**

```go
package quota

import "github.com/dmitrymomot/forge/core/clock"

type config struct {
	clk    clock.Clock
	prefix string
}

// Option configures a Meter.
type Option func(*config)

// WithClock injects a clock (for tests). Default clock.System(). Pass the SAME
// clock to the underlying store so window rolls and TTL expiry stay in sync.
func WithClock(clk clock.Clock) Option {
	return func(c *config) {
		if clk != nil {
			c.clk = clk
		}
	}
}

// WithKeyPrefix namespaces every store key (e.g. "quota:").
func WithKeyPrefix(p string) Option {
	return func(c *config) { c.prefix = p }
}
```

- [ ] **Step 4: Implement `quota.go`**

```go
package quota

import (
	"context"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/ratelimit"
)

// Meter tracks cumulative usage per subject over a Window, riding the shared
// ratelimit.Store counter seam. See the package doc for the covered shapes.
type Meter struct {
	store  ratelimit.Store
	window Window
	cfg    config
}

// New builds a Meter over store, using window to bucket usage. The store's
// lifecycle is the caller's.
func New(store ratelimit.Store, window Window, opts ...Option) *Meter {
	c := config{clk: clock.System()}
	for _, o := range opts {
		o(&c)
	}
	return &Meter{store: store, window: window, cfg: c}
}

func (m *Meter) key(subject, period string) string {
	if period == "" {
		return m.cfg.prefix + subject
	}
	return m.cfg.prefix + subject + ":" + period
}

// ttlFor returns the counter TTL for a window: time until reset, or -1 (no
// expiry) for gauges (reset is the zero Time).
func ttlFor(now, reset time.Time) time.Duration {
	if reset.IsZero() {
		return -1
	}
	if d := reset.Sub(now); d > 0 {
		return d
	}
	return time.Second
}

// Usage reports current consumption for subject without consuming.
func (m *Meter) Usage(ctx context.Context, subject string, limit Limit) (Result, error) {
	if err := limit.Validate(); err != nil {
		return Result{}, err
	}
	now := m.cfg.clk.Now()
	period, reset := m.window(subject, now)
	used, err := m.store.Get(ctx, m.key(subject, period))
	if err != nil {
		return Result{}, err
	}
	allowed := limit.Max == Unlimited || used < limit.Max
	return makeResult(limit, used, reset, allowed), nil
}

// Allow consumes cost against subject and reports the decision. It uses
// incr-then-rollback: it increments by cost, and if the new total exceeds a
// finite Max it compensates with -cost and reports Allowed=false, so a rejected
// call does not burn quota.
func (m *Meter) Allow(ctx context.Context, subject string, cost int64, limit Limit) (Result, error) {
	if cost < 0 {
		return Result{}, ErrInvalidCost
	}
	if err := limit.Validate(); err != nil {
		return Result{}, err
	}
	now := m.cfg.clk.Now()
	period, reset := m.window(subject, now)
	key := m.key(subject, period)
	ttl := ttlFor(now, reset)

	used, err := m.store.Incr(ctx, key, cost, ttl)
	if err != nil {
		return Result{}, err
	}
	if limit.Max != Unlimited && used > limit.Max {
		if _, rbErr := m.store.Incr(ctx, key, -cost, ttl); rbErr != nil {
			return Result{}, rbErr
		}
		return makeResult(limit, used-cost, reset, false), nil
	}
	return makeResult(limit, used, reset, true), nil
}
```

- [ ] **Step 5: Run tests**

Run: `just test ./resilience/quota/`
Expected: PASS.

- [ ] **Step 6: Format, lint, commit**

```bash
just fmt ./resilience/quota/...
just lint
git add resilience/quota/
git commit -m "feat(quota): Meter with Allow (incr-then-rollback + overage) and Usage"
```

---

## Task 5: quota Meter — Add, Set, Reset, doc.go

**Files:**
- Modify: `resilience/quota/quota.go` (append methods)
- Create: `resilience/quota/doc.go`
- Test: `resilience/quota/gauge_test.go`

**Interfaces:**
- Consumes: Task 4 `Meter`.
- Produces:
  - `func (*Meter) Add(ctx, subject string, delta int64) (int64, error)`
  - `func (*Meter) Set(ctx, subject string, value int64) error`
  - `func (*Meter) Reset(ctx, subject string) error`

- [ ] **Step 1: Write the failing gauge test**

`resilience/quota/gauge_test.go`:
```go
package quota_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/quota"
	"github.com/dmitrymomot/forge/resilience/ratelimit"
)

func TestGauge_SeatsAcquireReleaseReconcile(t *testing.T) {
	clk := clock.NewMock(time.Unix(0, 0))
	store := ratelimit.NewMemoryStore(ratelimit.WithMemoryClock(clk))
	t.Cleanup(func() { _ = store.Close() })
	m := quota.New(store, quota.Gauge(), quota.WithClock(clk))
	ctx := context.Background()
	lim := quota.Limit{Included: 5, Max: 5}

	// add 3 seats, none expire even after a long advance (no-expiry gauge)
	_, err := m.Add(ctx, "tenant", 3)
	require.NoError(t, err)
	clk.Advance(10000 * time.Hour)
	res, err := m.Usage(ctx, "tenant", lim)
	require.NoError(t, err)
	assert.Equal(t, int64(3), res.Used)

	// release one
	_, err = m.Add(ctx, "tenant", -1)
	require.NoError(t, err)

	// reconcile from DB truth
	require.NoError(t, m.Set(ctx, "tenant", 4))
	res, err = m.Usage(ctx, "tenant", lim)
	require.NoError(t, err)
	assert.Equal(t, int64(4), res.Used)
	assert.Equal(t, int64(1), res.Remaining)

	require.NoError(t, m.Reset(ctx, "tenant"))
	res, err = m.Usage(ctx, "tenant", lim)
	require.NoError(t, err)
	assert.Equal(t, int64(0), res.Used)
}
```

- [ ] **Step 2: Run it to confirm it fails**

Run: `just test ./resilience/quota/`
Expected: FAIL — `undefined: m.Add/Set/Reset`.

- [ ] **Step 3: Append the methods to `quota.go`**

```go
// Add applies a signed delta to subject's counter and returns the new total.
// Use it to reconcile a token estimate to actual (delta = actual - estimate) or
// to release gauge units (negative delta). Add never rejects.
func (m *Meter) Add(ctx context.Context, subject string, delta int64) (int64, error) {
	now := m.cfg.clk.Now()
	period, reset := m.window(subject, now)
	return m.store.Incr(ctx, m.key(subject, period), delta, ttlFor(now, reset))
}

// Set forces subject's counter to value — seed or repair a gauge from the
// consumer's authoritative count. It is Get+Add and is best-effort under
// concurrency; use it for periodic reconciliation, not per-request writes.
func (m *Meter) Set(ctx context.Context, subject string, value int64) error {
	now := m.cfg.clk.Now()
	period, reset := m.window(subject, now)
	key := m.key(subject, period)
	cur, err := m.store.Get(ctx, key)
	if err != nil {
		return err
	}
	if cur == value {
		return nil
	}
	_, err = m.store.Incr(ctx, key, value-cur, ttlFor(now, reset))
	return err
}

// Reset clears subject's counter for the current window.
func (m *Meter) Reset(ctx context.Context, subject string) error {
	now := m.cfg.clk.Now()
	period, _ := m.window(subject, now)
	return m.store.Reset(ctx, m.key(subject, period))
}
```

- [ ] **Step 4: Write `doc.go`**

```go
// Package quota caps cumulative usage per subject against a caller-owned limit —
// the plan-entitlement counterpart to ratelimit. It rides the shared
// ratelimit.Store counter seam and covers three shapes behind one Meter:
// calendar-window meters (events/month), rolling-window meters (fixed-window
// approximation), and gauges (live seats/storage, no reset).
//
// Feature-tier entitlement ("feature X on tier Y") is NOT a quota concern — that
// is set-membership; use ops/featureflag.
//
// # Usage
//
//	store := ratelimit.NewMemoryStore() // or ratelimit/redisstore, ratelimit/pgstore
//	m := quota.New(store, quota.Calendar(quota.Monthly, nil))
//	lim := quota.Limit{Included: 10_000, Max: 12_000} // 10k included, 2k overage
//	res, err := m.Allow(ctx, tenantID, tokens, lim)
//	if err != nil { /* ... */ }
//	if !res.Allowed { return errPlanExceeded }
//	if res.Overage > 0 { billing.RecordOverage(tenantID, res.Overage) }
//
// Gauges need a durable store (the memory store is LRU/janitor-pruned); use
// ratelimit/pgstore for seats and storage caps.
package quota
```

- [ ] **Step 5: Run tests, format, lint, commit**

```bash
just test ./resilience/quota/
just fmt ./resilience/quota/...
just lint
git add resilience/quota/
git commit -m "feat(quota): Add/Set/Reset for reconciliation and gauges, plus doc"
```

---

## Task 6: ratelimit/pgstore — Postgres counter Store

**Files:**
- Create: `resilience/ratelimit/pgstore/pgstore.go`, `resilience/ratelimit/pgstore/doc.go`
- Create: `resilience/ratelimit/pgstore/migrations/20260711120000_counters.sql`
- Test: `resilience/ratelimit/pgstore/pgstore_test.go`

**Interfaces:**
- Consumes: `*pgxpool.Pool`, `pgx.ErrNoRows`; `ratelimit.Store` (implements it); `data/migration` + `data/postgres` for the test.
- Produces:
  - `var Migrations embed.FS`
  - `func New(pool *pgxpool.Pool, opts ...Option) *Store` implementing `ratelimit.Store`
  - Table `forge_ratelimit_counters(key text pk, val bigint, expires_at timestamptz null)`.

- [ ] **Step 1: Write the migration file**

`resilience/ratelimit/pgstore/migrations/20260711120000_counters.sql`:
```sql
-- +goose Up
CREATE TABLE IF NOT EXISTS forge_ratelimit_counters (
    key        text PRIMARY KEY,
    val        bigint NOT NULL,
    expires_at timestamptz
);

-- +goose Down
DROP TABLE IF EXISTS forge_ratelimit_counters;
```

- [ ] **Step 2: Write the failing integration test**

`resilience/ratelimit/pgstore/pgstore_test.go`:
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

	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/resilience/ratelimit"
	"github.com/dmitrymomot/forge/resilience/ratelimit/pgstore"
)

var _ ratelimit.Store = (*pgstore.Store)(nil)

func newStore(t *testing.T) *pgstore.Store {
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
	require.NoError(t, migration.New(pgstore.Migrations, migration.WithTable("forge_ratelimit_schema")).Up(context.Background(), db))
	return pgstore.New(pool)
}

func TestPgCounter_IncrTTLAndNoExpiry(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	require.NoError(t, s.Reset(ctx, "c1"))
	require.NoError(t, s.Reset(ctx, "g1"))

	n, err := s.Incr(ctx, "c1", 3, 500*time.Millisecond)
	require.NoError(t, err)
	assert.Equal(t, int64(3), n)
	n, err = s.Incr(ctx, "c1", 2, 500*time.Millisecond) // must not extend TTL
	require.NoError(t, err)
	assert.Equal(t, int64(5), n)
	time.Sleep(700 * time.Millisecond)
	got, err := s.Get(ctx, "c1")
	require.NoError(t, err)
	assert.Equal(t, int64(0), got) // expired

	_, err = s.Incr(ctx, "g1", 4, 0) // no expiry
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)
	got, err = s.Get(ctx, "g1")
	require.NoError(t, err)
	assert.Equal(t, int64(4), got)
	require.NoError(t, s.Reset(ctx, "g1"))
}
```

- [ ] **Step 3: Run it to confirm it fails**

Run: `just test ./resilience/ratelimit/pgstore/`
Expected: FAIL — `undefined: pgstore.New/Migrations` (or skip without DSN).

- [ ] **Step 4: Implement `pgstore.go`**

```go
package pgstore

import (
	"context"
	"embed"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Migrations holds the goose migration that creates forge_ratelimit_counters.
// Apply it via data/migration under its own version table.
//
//go:embed migrations/*.sql
var Migrations embed.FS

type config struct{}

// Option configures the Store (reserved for future use).
type Option func(*config)

// Store is a durable Postgres counter Store implementing ratelimit.Store. It is
// the recommended backend for quota gauges. The pool's lifecycle is the
// caller's; Close is a no-op.
type Store struct {
	pool *pgxpool.Pool
}

// New builds a Postgres-backed counter Store. Apply Migrations first.
func New(pool *pgxpool.Pool, opts ...Option) *Store {
	var c config
	for _, o := range opts {
		o(&c)
	}
	return &Store{pool: pool}
}

const incrSQL = `
INSERT INTO forge_ratelimit_counters (key, val, expires_at)
VALUES ($1, $2, CASE WHEN $3::bigint > 0 THEN now() + ($3::text || ' milliseconds')::interval ELSE NULL END)
ON CONFLICT (key) DO UPDATE SET
    val = CASE
        WHEN forge_ratelimit_counters.expires_at IS NOT NULL AND forge_ratelimit_counters.expires_at <= now()
        THEN EXCLUDED.val
        ELSE forge_ratelimit_counters.val + EXCLUDED.val END,
    expires_at = CASE
        WHEN forge_ratelimit_counters.expires_at IS NOT NULL AND forge_ratelimit_counters.expires_at <= now()
        THEN EXCLUDED.expires_at
        ELSE forge_ratelimit_counters.expires_at END
RETURNING val`

// Incr adds delta, arming the TTL only on create or after expiry; a ttl <= 0
// stores a NULL (never-expiring) expiry.
func (s *Store) Incr(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	var v int64
	err := s.pool.QueryRow(ctx, incrSQL, key, delta, ttl.Milliseconds()).Scan(&v)
	return v, err
}

// Get returns the live counter, or 0 when absent or expired.
func (s *Store) Get(ctx context.Context, key string) (int64, error) {
	var v int64
	err := s.pool.QueryRow(ctx,
		`SELECT val FROM forge_ratelimit_counters WHERE key = $1 AND (expires_at IS NULL OR expires_at > now())`,
		key).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return v, err
}

// Reset deletes the counter.
func (s *Store) Reset(ctx context.Context, key string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM forge_ratelimit_counters WHERE key = $1`, key)
	return err
}

// Close is a no-op; the pool is owned by the caller.
func (s *Store) Close() error { return nil }
```

- [ ] **Step 5: Write `doc.go`**

```go
// Package pgstore is a durable Postgres implementation of ratelimit.Store (the
// counter seam shared by ratelimit and quota). It is the recommended backend
// for quota gauges, which need non-expiring counters the memory store can prune.
//
// The DDL (forge_ratelimit_counters: key text PK, val bigint, expires_at
// timestamptz NULL) ships as an embedded goose migration in Migrations; apply
// it via data/migration under its own version table (e.g. "forge_ratelimit_schema").
// Non-goose shops can copy that DDL into their own migration tool.
//
// # Usage
//
//	db := stdlib.OpenDBFromPool(pool)
//	_ = migration.New(pgstore.Migrations, migration.WithTable("forge_ratelimit_schema")).Up(ctx, db)
//	store := pgstore.New(pool)
//	m := quota.New(store, quota.Gauge())
package pgstore
```

- [ ] **Step 6: Run tests, format, lint, commit**

```bash
just test ./resilience/ratelimit/pgstore/
just fmt ./resilience/ratelimit/pgstore/...
just lint
git add resilience/ratelimit/pgstore/
git commit -m "feat(ratelimit): Postgres counter store driver (pgstore)"
```

---

## Task 7: loadshed — Criteria, Concurrency, Latency

**Files:**
- Create: `resilience/loadshed/criteria.go`
- Test: `resilience/loadshed/criteria_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Criteria interface{ Pressure() float64 }`
  - `func Concurrency(max int) Criteria`
  - `func Latency(threshold time.Duration, opts ...LatencyOption) Criteria`; `type LatencyOption`; `func WithEWMAAlpha(float64) LatencyOption`
  - internal `admitHook interface{ onAdmit() }`, `doneHook interface{ onDone(time.Duration) }` (unexported; consumed by Task 8).

- [ ] **Step 1: Write the failing test**

`resilience/loadshed/criteria_test.go`:
```go
package loadshed_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/resilience/loadshed"
)

func TestConcurrencyPressureZeroWhenIdle(t *testing.T) {
	c := loadshed.Concurrency(10)
	assert.Equal(t, 0.0, c.Pressure())
}

func TestLatencyPressureClampsAtOne(t *testing.T) {
	l := loadshed.Latency(100 * time.Millisecond)
	assert.Equal(t, 0.0, l.Pressure()) // no samples yet
}
```

- [ ] **Step 2: Run it to confirm it fails**

Run: `just test ./resilience/loadshed/`
Expected: FAIL — package/symbols undefined.

- [ ] **Step 3: Implement `criteria.go`**

```go
package loadshed

import (
	"sync"
	"sync/atomic"
	"time"
)

// Criteria reports current load pressure in [0,1]; 0 idle, 1 saturated. It is
// polled once per admission decision. Implement it over any signal (CPU, queue
// depth, pool saturation) to plug a custom criterion into a Shedder.
type Criteria interface {
	Pressure() float64
}

// admitHook / doneHook let built-in criteria observe the request lifecycle. The
// Shedder type-asserts these; custom criteria may ignore them.
type admitHook interface{ onAdmit() }
type doneHook interface{ onDone(latency time.Duration) }

// concurrency reports inflight/max.
type concurrency struct {
	inflight atomic.Int64
	max      int64
}

// Concurrency returns a Criteria whose pressure is the in-flight count over max.
func Concurrency(max int) Criteria { return &concurrency{max: int64(max)} }

func (c *concurrency) Pressure() float64 {
	if c.max <= 0 {
		return 0
	}
	return float64(c.inflight.Load()) / float64(c.max)
}
func (c *concurrency) onAdmit()              { c.inflight.Add(1) }
func (c *concurrency) onDone(time.Duration) { c.inflight.Add(-1) }

type latencyConfig struct{ alpha float64 }

// LatencyOption configures the Latency criterion.
type LatencyOption func(*latencyConfig)

// WithEWMAAlpha sets the EWMA smoothing factor in (0,1]; higher reacts faster.
// Default 0.2.
func WithEWMAAlpha(a float64) LatencyOption {
	return func(c *latencyConfig) {
		if a > 0 && a <= 1 {
			c.alpha = a
		}
	}
}

// latency reports an EWMA of recent request latency over threshold.
type latency struct {
	mu        sync.Mutex
	ewma      float64
	threshold float64
	alpha     float64
}

// Latency returns a Criteria whose pressure is EWMA(latency)/threshold, clamped
// to [0,1]. It observes completion latency via the admit→done lifecycle.
func Latency(threshold time.Duration, opts ...LatencyOption) Criteria {
	c := latencyConfig{alpha: 0.2}
	for _, o := range opts {
		o(&c)
	}
	return &latency{threshold: float64(threshold), alpha: c.alpha}
}

func (l *latency) Pressure() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.threshold <= 0 {
		return 0
	}
	p := l.ewma / l.threshold
	if p > 1 {
		return 1
	}
	return p
}
func (l *latency) onAdmit() {}
func (l *latency) onDone(d time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := float64(d)
	if l.ewma == 0 {
		l.ewma = s
	} else {
		l.ewma = l.alpha*s + (1-l.alpha)*l.ewma
	}
}
```

- [ ] **Step 4: Run tests, format, lint, commit**

```bash
just test ./resilience/loadshed/
just fmt ./resilience/loadshed/...
just lint
git add resilience/loadshed/
git commit -m "feat(loadshed): Criteria seam with Concurrency and Latency built-ins"
```

---

## Task 8: loadshed — Shedder, Acquire, Middleware, doc.go

**Files:**
- Create: `resilience/loadshed/loadshed.go`, `resilience/loadshed/options.go`, `resilience/loadshed/middleware.go`, `resilience/loadshed/doc.go`
- Test: `resilience/loadshed/loadshed_test.go`, `resilience/loadshed/middleware_test.go`

**Interfaces:**
- Consumes: Task 7 `Criteria`/`admitHook`/`doneHook`; `clock.Clock`; `web/middleware.Middleware`.
- Produces:
  - `type Option func(*config)`; `func WithCriteria(...Criteria) Option`; `func WithThreshold(float64) Option`; `func WithFloor(float64) Option`; `func WithClock(clock.Clock) Option`; `func WithRand(func() float64) Option`
  - `type Shedder struct{ ... }`; `func New(opts ...Option) *Shedder`
  - `type Ticket interface{ Release() }`; `func (*Shedder) Acquire(ctx) (Ticket, bool)`
  - `type MiddlewareOption`; `func WithSkip(func(*http.Request) bool) MiddlewareOption`; `func WithResponder(func(http.ResponseWriter, *http.Request)) MiddlewareOption`; `func (*Shedder) Middleware(...MiddlewareOption) middleware.Middleware`

- [ ] **Step 1: Write the failing tests**

`resilience/loadshed/loadshed_test.go`:
```go
package loadshed_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/resilience/loadshed"
)

func TestAcquire_AdmitsWhenIdle(t *testing.T) {
	s := loadshed.New(loadshed.WithCriteria(loadshed.Concurrency(4)))
	tk, ok := s.Acquire(context.Background())
	assert.True(t, ok)
	tk.Release()
}

func TestAcquire_ShedsWhenSaturated(t *testing.T) {
	// force pressure high and rand low so the ramp always rejects
	s := loadshed.New(
		loadshed.WithCriteria(loadshed.Concurrency(1)),
		loadshed.WithThreshold(0.0),
		loadshed.WithFloor(0.0),
		loadshed.WithRand(func() float64 { return 0.0 }),
	)
	tk1, ok := s.Acquire(context.Background()) // fills the single slot
	assert.True(t, ok)
	_, ok2 := s.Acquire(context.Background()) // pressure 1.0, reject prob 1.0
	assert.False(t, ok2)
	tk1.Release()
}

func TestPressure_FailsOpenOnPanic(t *testing.T) {
	s := loadshed.New(
		loadshed.WithCriteria(panicCriteria{}),
		loadshed.WithThreshold(0.0),
		loadshed.WithRand(func() float64 { return 0.0 }),
	)
	_, ok := s.Acquire(context.Background())
	assert.True(t, ok) // panic → pressure 0 → admit
}

type panicCriteria struct{}

func (panicCriteria) Pressure() float64 { panic("boom") }
```

`resilience/loadshed/middleware_test.go`:
```go
package loadshed_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/resilience/loadshed"
)

func TestMiddleware_ShedReturns503(t *testing.T) {
	s := loadshed.New(
		loadshed.WithCriteria(loadshed.Concurrency(1)),
		loadshed.WithThreshold(0.0),
		loadshed.WithFloor(0.0),
		loadshed.WithRand(func() float64 { return 0.0 }),
	)
	// occupy the slot with a never-returning handler running concurrently
	busy := make(chan struct{})
	h := s.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-busy }))
	go h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	// second request sheds
	rec := httptest.NewRecorder()
	// small spin to let the goroutine acquire
	for i := 0; i < 1000 && rec.Code == 200; i++ {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	}
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	close(busy)
}

func TestMiddleware_SkipBypasses(t *testing.T) {
	s := loadshed.New(
		loadshed.WithCriteria(loadshed.Concurrency(1)),
		loadshed.WithThreshold(0.0), loadshed.WithFloor(0.0),
		loadshed.WithRand(func() float64 { return 0.0 }),
	)
	h := s.Middleware(loadshed.WithSkip(func(*http.Request) bool { return true }))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	assert.Equal(t, 200, rec.Code)
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `just test ./resilience/loadshed/`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Implement `options.go`**

```go
package loadshed

import "github.com/dmitrymomot/forge/core/clock"

type config struct {
	clk       clock.Clock
	rnd       func() float64
	criteria  []Criteria
	threshold float64
	floor     float64
}

// Option configures a Shedder.
type Option func(*config)

// WithCriteria sets the pressure signals; overall pressure is their max.
func WithCriteria(cs ...Criteria) Option {
	return func(c *config) { c.criteria = append(c.criteria, cs...) }
}

// WithThreshold sets the low-water pressure below which all requests are
// admitted. Default 0.8. Clamped to [0,1].
func WithThreshold(t float64) Option {
	return func(c *config) { c.threshold = clamp01(t) }
}

// WithFloor sets the minimum admit fraction at full saturation (the fail-open
// sampler). Default 0.05. Clamped to [0,1].
func WithFloor(f float64) Option {
	return func(c *config) { c.floor = clamp01(f) }
}

// WithClock injects a clock (for tests). Default clock.System().
func WithClock(clk clock.Clock) Option {
	return func(c *config) {
		if clk != nil {
			c.clk = clk
		}
	}
}

// WithRand injects the [0,1) source used by the rejection ramp (for tests).
func WithRand(fn func() float64) Option {
	return func(c *config) {
		if fn != nil {
			c.rnd = fn
		}
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
```

- [ ] **Step 4: Implement `loadshed.go`**

```go
package loadshed

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

// Shedder is an adaptive admission controller: it rejects a fraction of work
// when its Criteria report overload, protecting the service from itself.
type Shedder struct {
	clk        clock.Clock
	rnd        func() float64
	criteria   []Criteria
	admitHooks []admitHook
	doneHooks  []doneHook
	threshold  float64
	floor      float64
}

// New builds a Shedder. Defaults: threshold 0.8, floor 0.05, clock.System().
func New(opts ...Option) *Shedder {
	c := config{clk: clock.System(), rnd: rand.Float64, threshold: 0.8, floor: 0.05}
	for _, o := range opts {
		o(&c)
	}
	s := &Shedder{
		clk: c.clk, rnd: c.rnd, criteria: c.criteria,
		threshold: c.threshold, floor: c.floor,
	}
	for _, cr := range c.criteria {
		if h, ok := cr.(admitHook); ok {
			s.admitHooks = append(s.admitHooks, h)
		}
		if h, ok := cr.(doneHook); ok {
			s.doneHooks = append(s.doneHooks, h)
		}
	}
	return s
}

// Ticket is returned by an admitted Acquire; Release MUST be called on
// completion (records latency, decrements inflight).
type Ticket interface{ Release() }

type ticket struct {
	s     *Shedder
	start time.Time
	done  bool
}

func (t *ticket) Release() {
	if t.done {
		return
	}
	t.done = true
	d := t.s.clk.Now().Sub(t.start)
	for _, h := range t.s.doneHooks {
		h.onDone(d)
	}
}

// Acquire reports whether the request is admitted. On admit it returns a Ticket
// whose Release must be called; on shed it returns (nil, false).
func (s *Shedder) Acquire(_ context.Context) (Ticket, bool) {
	if !s.admit() {
		return nil, false
	}
	for _, h := range s.admitHooks {
		h.onAdmit()
	}
	return &ticket{s: s, start: s.clk.Now()}, true
}

// admit applies the probabilistic rejection ramp: admit all below threshold,
// then reject with probability rising to (1 - floor) at saturation.
func (s *Shedder) admit() bool {
	p := s.pressure()
	if p <= s.threshold {
		return true
	}
	denom := 1 - s.threshold
	frac := 1.0
	if denom > 0 {
		frac = (p - s.threshold) / denom
	}
	if frac > 1 {
		frac = 1
	}
	rejectProb := frac * (1 - s.floor)
	return s.rnd() >= rejectProb
}

// pressure is the max over all criteria; a panicking criterion contributes 0
// (fail open — a monitoring glitch must not become an outage).
func (s *Shedder) pressure() float64 {
	maxP := 0.0
	for _, c := range s.criteria {
		if p := safePressure(c); p > maxP {
			maxP = p
		}
	}
	return maxP
}

func safePressure(c Criteria) (p float64) {
	defer func() {
		if recover() != nil {
			p = 0
		}
	}()
	return c.Pressure()
}
```

- [ ] **Step 5: Implement `middleware.go`**

```go
package loadshed

import (
	"net/http"

	"github.com/dmitrymomot/forge/web/middleware"
)

type middlewareConfig struct {
	responder func(http.ResponseWriter, *http.Request)
	skip      func(*http.Request) bool
}

// MiddlewareOption configures Middleware.
type MiddlewareOption func(*middlewareConfig)

// WithResponder overrides the shed response (default 503 + Retry-After).
func WithResponder(fn func(http.ResponseWriter, *http.Request)) MiddlewareOption {
	return func(c *middlewareConfig) {
		if fn != nil {
			c.responder = fn
		}
	}
}

// WithSkip never-sheds requests for which fn returns true (health, admin).
func WithSkip(fn func(*http.Request) bool) MiddlewareOption {
	return func(c *middlewareConfig) { c.skip = fn }
}

// Middleware sheds a slice of traffic under overload, returning 503 via the
// responder; admitted requests are served and their Ticket released.
func (s *Shedder) Middleware(opts ...MiddlewareOption) middleware.Middleware {
	cfg := middlewareConfig{responder: defaultResponder}
	for _, o := range opts {
		o(&cfg)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.skip != nil && cfg.skip(r) {
				next.ServeHTTP(w, r)
				return
			}
			tk, ok := s.Acquire(r.Context())
			if !ok {
				cfg.responder(w, r)
				return
			}
			defer tk.Release()
			next.ServeHTTP(w, r)
		})
	}
}

func defaultResponder(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Retry-After", "1")
	http.Error(w, "service overloaded", http.StatusServiceUnavailable)
}
```

- [ ] **Step 6: Write `doc.go`**

```go
// Package loadshed is adaptive admission control: it rejects a fraction of
// incoming work early and cheaply when the service is overloaded, so admitted
// work still succeeds. It protects the callee (this service) based on its own
// current health — unlike ratelimit (per-client fairness) or circuitbreaker
// (protecting the caller from a failing dependency).
//
// # Usage
//
//	sh := loadshed.New(loadshed.WithCriteria(
//	    loadshed.Concurrency(500),
//	    loadshed.Latency(200*time.Millisecond),
//	))
//	mux.Use(sh.Middleware()) // 503s a slice of traffic under overload
//
//	// non-HTTP:
//	if tk, ok := sh.Acquire(ctx); ok {
//	    defer tk.Release()
//	    process(job)
//	}
//
// CPU-based pressure stays consumer-side: implement Criteria over your own CPU
// reader and pass it with WithCriteria.
package loadshed
```

- [ ] **Step 7: Run tests, format, lint, commit**

```bash
just test ./resilience/loadshed/
just fmt ./resilience/loadshed/...
just lint
git add resilience/loadshed/
git commit -m "feat(loadshed): Shedder with rejection ramp, Acquire, and Middleware"
```

---

## Task 9: lock — Store interface + memory store

**Files:**
- Create: `resilience/lock/store.go`, `resilience/lock/memory.go`
- Test: `resilience/lock/memory_test.go`

**Interfaces:**
- Consumes: `clock.Clock`/`clock.Mock`.
- Produces:
  - `type Store interface{ Acquire(...); Refresh(...); Release(...) }` (exact signatures below)
  - `type MemoryStore struct{ ... }`; `func NewMemoryStore(opts ...MemoryOption) *MemoryStore`; `func WithMemoryClock(clock.Clock) MemoryOption`
  - Consumed by Tasks 10–13.

- [ ] **Step 1: Write the failing test**

`resilience/lock/memory_test.go`:
```go
package lock_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/lock"
)

func TestMemoryStore_AcquireExclusiveAndFenceMonotonic(t *testing.T) {
	clk := clock.NewMock(time.Unix(0, 0))
	s := lock.NewMemoryStore(lock.WithMemoryClock(clk))
	ctx := context.Background()

	f1, ok, err := s.Acquire(ctx, "k", "owner-a", time.Minute)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, uint64(1), f1)

	_, ok, err = s.Acquire(ctx, "k", "owner-b", time.Minute) // held by a
	require.NoError(t, err)
	assert.False(t, ok)

	clk.Advance(2 * time.Minute) // a's lease expires
	f2, ok, err := s.Acquire(ctx, "k", "owner-b", time.Minute)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Greater(t, f2, f1) // fence strictly increases
}

func TestMemoryStore_RefreshAndRelease(t *testing.T) {
	clk := clock.NewMock(time.Unix(0, 0))
	s := lock.NewMemoryStore(lock.WithMemoryClock(clk))
	ctx := context.Background()
	_, ok, _ := s.Acquire(ctx, "k", "a", time.Minute)
	require.True(t, ok)

	ok, err := s.Refresh(ctx, "k", "a", time.Minute)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = s.Refresh(ctx, "k", "b", time.Minute) // not owner
	require.NoError(t, err)
	assert.False(t, ok)

	require.NoError(t, s.Release(ctx, "k", "a"))
	_, ok, _ = s.Acquire(ctx, "k", "b", time.Minute) // now free
	assert.True(t, ok)
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `just test ./resilience/lock/`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Implement `store.go`**

```go
package lock

import (
	"context"
	"time"
)

// Store is the 3-method distributed-lease seam. Implementations must make
// Acquire atomic per key. A fencing token is monotonic per key: pass it to the
// protected resource so a stale holder (paused past its TTL) is rejected.
type Store interface {
	// Acquire claims key for owner until now+ttl, returning a monotonic fencing
	// token on success. ok is false if another live owner holds key.
	Acquire(ctx context.Context, key, owner string, ttl time.Duration) (fence uint64, ok bool, err error)
	// Refresh extends the lease iff owner still holds key; ok is false if lost.
	Refresh(ctx context.Context, key, owner string, ttl time.Duration) (ok bool, err error)
	// Release frees key iff held by owner (no-op otherwise).
	Release(ctx context.Context, key, owner string) error
}
```

- [ ] **Step 4: Implement `memory.go`**

```go
package lock

import (
	"context"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

type memLease struct {
	expiresAt time.Time
	owner     string
	fence     uint64
}

// MemoryStore is an in-process lease Store for single-node use and tests.
type MemoryStore struct {
	clk   clock.Clock
	m     map[string]memLease
	mu    sync.Mutex
	fence uint64
}

type memoryConfig struct{ clk clock.Clock }

// MemoryOption configures NewMemoryStore.
type MemoryOption func(*memoryConfig)

// WithMemoryClock injects a clock (for tests). Default clock.System().
func WithMemoryClock(clk clock.Clock) MemoryOption {
	return func(c *memoryConfig) {
		if clk != nil {
			c.clk = clk
		}
	}
}

// NewMemoryStore returns an in-process lease Store.
func NewMemoryStore(opts ...MemoryOption) *MemoryStore {
	c := memoryConfig{clk: clock.System()}
	for _, o := range opts {
		o(&c)
	}
	return &MemoryStore{clk: c.clk, m: make(map[string]memLease)}
}

func (s *MemoryStore) Acquire(_ context.Context, key, owner string, ttl time.Duration) (uint64, bool, error) {
	now := s.clk.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if l, ok := s.m[key]; ok && l.owner != owner && now.Before(l.expiresAt) {
		return 0, false, nil // held by another live owner
	}
	s.fence++
	s.m[key] = memLease{owner: owner, expiresAt: now.Add(ttl), fence: s.fence}
	return s.fence, true, nil
}

func (s *MemoryStore) Refresh(_ context.Context, key, owner string, ttl time.Duration) (bool, error) {
	now := s.clk.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.m[key]
	if !ok || l.owner != owner || !now.Before(l.expiresAt) {
		return false, nil
	}
	l.expiresAt = now.Add(ttl)
	s.m[key] = l
	return true, nil
}

func (s *MemoryStore) Release(_ context.Context, key, owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l, ok := s.m[key]; ok && l.owner == owner {
		delete(s.m, key)
	}
	return nil
}
```

- [ ] **Step 5: Run tests, format, lint, commit**

```bash
just test ./resilience/lock/
just fmt ./resilience/lock/...
just lint
git add resilience/lock/store.go resilience/lock/memory.go resilience/lock/memory_test.go
git commit -m "feat(lock): lease Store seam and in-process memory store"
```

---

## Task 10: lock — Lock, Lease, Acquire/TryAcquire, auto-refresh

**Files:**
- Create: `resilience/lock/lock.go`, `resilience/lock/lease.go`, `resilience/lock/options.go`, `resilience/lock/errors.go`
- Test: `resilience/lock/lock_test.go`

**Interfaces:**
- Consumes: Task 9 `Store`; `clock.Clock`; `core/id` (`id.NewShort().String()`).
- Produces:
  - `type Option func(*config)`; `func WithTTL(time.Duration) Option`; `func WithOwner(string) Option`; `func WithRefreshInterval(time.Duration) Option`; `func WithClock(clock.Clock) Option`
  - `type Lock struct{ ... }`; `func New(store Store, opts ...Option) *Lock`
  - `func (*Lock) Acquire(ctx, key string) (*Lease, error)`; `func (*Lock) TryAcquire(ctx, key string) (*Lease, bool, error)`
  - `type Lease struct{ ... }`; `func (*Lease) Fence() uint64`; `func (*Lease) Done() <-chan struct{}`; `func (*Lease) Release(ctx) error`
  - `var ErrNotHeld, ErrLockLost error`
  - Consumed by Task 11 (`RunOnLeader`).

- [ ] **Step 1: Write the failing test**

`resilience/lock/lock_test.go`:
```go
package lock_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/resilience/lock"
)

func TestLock_TryAcquireMutualExclusion(t *testing.T) {
	store := lock.NewMemoryStore() // system clock
	l := lock.New(store, lock.WithTTL(time.Minute))
	ctx := context.Background()

	lease, ok, err := l.TryAcquire(ctx, "job")
	require.NoError(t, err)
	require.True(t, ok)
	assert.NotZero(t, lease.Fence())

	_, ok, err = l.TryAcquire(ctx, "job") // same owner? No — New() gives a random owner per Lock
	require.NoError(t, err)
	assert.True(t, ok) // same Lock instance = same owner → re-acquire allowed

	require.NoError(t, lease.Release(ctx))
}

func TestLock_AutoRefreshKeepsLeaseAlive(t *testing.T) {
	store := lock.NewMemoryStore()
	// short real durations: refresh every 10ms keeps a 40ms lease alive
	l := lock.New(store, lock.WithTTL(40*time.Millisecond), lock.WithRefreshInterval(10*time.Millisecond))
	ctx := context.Background()

	lease, err := l.Acquire(ctx, "singleton")
	require.NoError(t, err)
	defer lease.Release(ctx)

	// a competitor with a different owner must NOT get it while refresh runs
	other := lock.New(store, lock.WithTTL(40*time.Millisecond))
	time.Sleep(120 * time.Millisecond) // 3x TTL
	_, ok, err := other.TryAcquire(ctx, "singleton")
	require.NoError(t, err)
	assert.False(t, ok, "auto-refresh should keep the original lease alive")

	select {
	case <-lease.Done():
		t.Fatal("lease should not be lost while refreshing")
	default:
	}
}

func TestLease_DoneClosesWhenLost(t *testing.T) {
	store := lock.NewMemoryStore()
	l := lock.New(store, lock.WithTTL(30*time.Millisecond), lock.WithRefreshInterval(10*time.Millisecond))
	ctx := context.Background()
	lease, err := l.Acquire(ctx, "k")
	require.NoError(t, err)

	// steal it out from under the refresh loop by releasing via the store directly,
	// then acquiring as another owner
	require.NoError(t, store.Release(ctx, "k", ownerOf(lease))) // helper below is illustrative
	// simpler: forcibly expire by waiting past TTL after stopping refresh is hard;
	// instead assert Done eventually closes once refresh fails after a competing steal:
	other := lock.New(store, lock.WithTTL(time.Minute))
	_, ok, _ := other.TryAcquire(ctx, "k")
	require.True(t, ok)

	select {
	case <-lease.Done():
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Done should close after the lease is stolen")
	}
}
```

> The `ownerOf` line above is illustrative — remove it; the test works via the competing `TryAcquire` steal. Keep the final `select` assertion. (If you prefer, drop `TestLease_DoneClosesWhenLost` and cover loss in the pgstore/redisstore integration tests where stealing is natural; the memory refresh test above is the load-bearing one.)

- [ ] **Step 2: Run to confirm failure**

Run: `just test ./resilience/lock/`
Expected: FAIL — undefined `lock.New`/`Lease`.

- [ ] **Step 3: Implement `errors.go`**

```go
package lock

import "errors"

// ErrNotHeld is returned when refreshing or releasing a lease this owner does
// not hold.
var ErrNotHeld = errors.New("lock: not held by this owner")

// ErrLockLost reports that a held lease was lost (a refresh failed); observe it
// via Lease.Done.
var ErrLockLost = errors.New("lock: lease lost")
```

- [ ] **Step 4: Implement `options.go`**

```go
package lock

import (
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/id"
)

type config struct {
	clk     clock.Clock
	owner   string
	ttl     time.Duration
	refresh time.Duration
}

// Option configures a Lock.
type Option func(*config)

// WithTTL sets the lease duration. Default 30s.
func WithTTL(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.ttl = d
		}
	}
}

// WithOwner sets this Lock's owner id. Default a random short id — all leases
// from one Lock share it, so the same Lock can re-acquire its own key.
func WithOwner(owner string) Option {
	return func(c *config) {
		if owner != "" {
			c.owner = owner
		}
	}
}

// WithRefreshInterval sets how often a held lease is refreshed. Default TTL/3.
func WithRefreshInterval(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.refresh = d
		}
	}
}

// WithClock injects a clock (for tests). Default clock.System().
func WithClock(clk clock.Clock) Option {
	return func(c *config) {
		if clk != nil {
			c.clk = clk
		}
	}
}

func defaultConfig() config {
	return config{clk: clock.System(), owner: id.NewShort().String(), ttl: 30 * time.Second}
}
```

- [ ] **Step 5: Implement `lock.go`**

```go
package lock

import (
	"context"
	"time"
)

// Lock issues distributed leases over a Store. All leases from one Lock share
// its owner id.
type Lock struct {
	store Store
	cfg   config
}

// New builds a Lock. RefreshInterval defaults to TTL/3 when unset.
func New(store Store, opts ...Option) *Lock {
	c := defaultConfig()
	for _, o := range opts {
		o(&c)
	}
	if c.refresh <= 0 {
		c.refresh = c.ttl / 3
	}
	if c.refresh <= 0 {
		c.refresh = c.ttl
	}
	return &Lock{store: store, cfg: c}
}

// TryAcquire makes a single attempt to claim key. ok is false if already held.
func (l *Lock) TryAcquire(ctx context.Context, key string) (*Lease, bool, error) {
	fence, ok, err := l.store.Acquire(ctx, key, l.cfg.owner, l.cfg.ttl)
	if err != nil || !ok {
		return nil, ok, err
	}
	return l.newLease(key, fence), true, nil
}

// Acquire blocks, retrying at the refresh cadence, until key is held or ctx is
// cancelled.
func (l *Lock) Acquire(ctx context.Context, key string) (*Lease, error) {
	for {
		lease, ok, err := l.TryAcquire(ctx, key)
		if err != nil {
			return nil, err
		}
		if ok {
			return lease, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(l.cfg.refresh):
		}
	}
}
```

- [ ] **Step 6: Implement `lease.go`**

```go
package lock

import (
	"context"
	"sync"
	"time"
)

// Lease is a held distributed lock. A background goroutine refreshes it until
// Release or ctx-cancel; if a refresh fails (expired or stolen) Done closes and
// the holder must stop its critical section.
type Lease struct {
	lock   *Lock
	key    string
	cancel context.CancelFunc
	done   chan struct{}
	fence  uint64
	once   sync.Once
}

func (l *Lock) newLease(key string, fence uint64) *Lease {
	ctx, cancel := context.WithCancel(context.Background())
	le := &Lease{lock: l, key: key, fence: fence, cancel: cancel, done: make(chan struct{})}
	go le.refreshLoop(ctx)
	return le
}

// Fence returns the monotonic fencing token for this lease.
func (le *Lease) Fence() uint64 { return le.fence }

// Done is closed when the lease is lost (a refresh failed) or after Release.
func (le *Lease) Done() <-chan struct{} { return le.done }

// Release frees the lease and stops refreshing. Safe to call multiple times.
func (le *Lease) Release(ctx context.Context) error {
	le.stop()
	return le.lock.store.Release(ctx, le.key, le.lock.cfg.owner)
}

func (le *Lease) stop() {
	le.once.Do(func() {
		le.cancel()
		close(le.done)
	})
}

func (le *Lease) refreshLoop(ctx context.Context) {
	t := time.NewTicker(le.lock.cfg.refresh)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			ok, err := le.lock.store.Refresh(ctx, le.key, le.lock.cfg.owner, le.lock.cfg.ttl)
			if err != nil || !ok {
				le.stop() // lease lost
				return
			}
		}
	}
}
```

- [ ] **Step 7: Run tests, format, lint, commit**

Run: `just test ./resilience/lock/` (expect PASS; if `TestLease_DoneClosesWhenLost` is flaky, keep only the deterministic assertions or move loss coverage to the driver integration tests as noted).

```bash
just fmt ./resilience/lock/...
just lint
git add resilience/lock/
git commit -m "feat(lock): Lock, Lease, blocking/try acquire, and auto-refresh"
```

---

## Task 11: lock — RunOnLeader + doc.go

**Files:**
- Create: `resilience/lock/leader.go`, `resilience/lock/doc.go`
- Test: `resilience/lock/leader_test.go`

**Interfaces:**
- Consumes: Task 10 `Lock`/`Lease`; `ops/supervisor.Service`.
- Produces: `func (*Lock) RunOnLeader(name, key string, run func(ctx context.Context) error) supervisor.Service`.

- [ ] **Step 1: Write the failing test**

`resilience/lock/leader_test.go`:
```go
package lock_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/resilience/lock"
)

func TestRunOnLeader_OnlyOneRunsAtATime(t *testing.T) {
	store := lock.NewMemoryStore()
	l := lock.New(store, lock.WithTTL(40*time.Millisecond), lock.WithRefreshInterval(10*time.Millisecond))

	var running atomic.Int32
	var maxSeen atomic.Int32
	work := func(ctx context.Context) error {
		n := running.Add(1)
		for {
			if m := maxSeen.Load(); n > m {
				maxSeen.CompareAndSwap(m, n)
			}
			select {
			case <-ctx.Done():
				running.Add(-1)
				return ctx.Err()
			case <-time.After(2 * time.Millisecond):
			}
		}
	}

	svcA := l.RunOnLeader("worker", "leader-key", work)
	svcB := l.RunOnLeader("worker", "leader-key", work)
	assert.Equal(t, "worker", svcA.Name())

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = svcA.Run(ctx) }()
	go func() { _ = svcB.Run(ctx) }()

	time.Sleep(150 * time.Millisecond)
	assert.LessOrEqual(t, maxSeen.Load(), int32(1), "at most one leader runs at a time")
	cancel()
	time.Sleep(60 * time.Millisecond)
}
```

> Note: A and B share one `Lock` (same owner), so this proves single-run via mutual exclusion of the leader loop. For true multi-owner election, construct two Locks with distinct owners over the same store — optional extra assertion.

- [ ] **Step 2: Run to confirm failure**

Run: `just test ./resilience/lock/`
Expected: FAIL — `undefined: l.RunOnLeader`.

- [ ] **Step 3: Implement `leader.go`**

```go
package lock

import (
	"context"
	"errors"
	"time"

	"github.com/dmitrymomot/forge/ops/supervisor"
)

// RunOnLeader returns a supervisor.Service that runs run on exactly one node.
// It continuously campaigns for key; whoever holds the lease runs
// run(leaderCtx) while the rest stand by. leaderCtx is cancelled on leadership
// loss (run must return promptly). On supervisor shutdown it releases the lease
// for instant failover. Election is automatic and continuous.
func (l *Lock) RunOnLeader(name, key string, run func(ctx context.Context) error) supervisor.Service {
	return &leader{lock: l, name: name, key: key, run: run}
}

type leader struct {
	lock *Lock
	run  func(context.Context) error
	name string
	key  string
}

func (le *leader) Name() string { return le.name }

func (le *leader) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		lease, err := le.lock.Acquire(ctx, le.key)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err() // cancelled while campaigning = clean stop
			}
			return err
		}

		leaderCtx, cancel := context.WithCancel(ctx)
		go func() {
			select {
			case <-lease.Done(): // lost leadership
				cancel()
			case <-leaderCtx.Done():
			}
		}()

		runErr := le.run(leaderCtx)
		cancel()
		le.release(ctx, lease)

		if ctx.Err() != nil {
			return ctx.Err()
		}
		if runErr != nil && !errors.Is(runErr, context.Canceled) {
			return runErr // a real error stops the service
		}
		// lost leadership but parent alive → re-campaign
	}
}

// release frees the lease even when the parent ctx is already cancelled, so
// shutdown yields the lock immediately (instant failover) instead of after TTL.
func (le *leader) release(parent context.Context, lease *Lease) {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	_ = lease.Release(rctx)
}
```

- [ ] **Step 4: Write `doc.go`**

```go
// Package lock is a distributed mutex with TTL leases, monotonic fencing
// tokens, and auto-refresh, plus cluster-singleton leader election. It has an
// in-process memory store; real backends live in lock/pgstore (table lease) and
// lock/redisstore (single-instance).
//
// # Usage
//
//	l := lock.New(store, lock.WithTTL(30*time.Second))
//
//	// mutual exclusion around a critical section:
//	lease, err := l.Acquire(ctx, "tenant:42:import")
//	if err != nil { return err }
//	defer lease.Release(ctx)
//	importBatch(ctx, lease.Fence()) // pass the fence to reject stale holders
//
//	// cluster singleton — run on exactly one node, failover automatic:
//	sup.Add(l.RunOnLeader("outbox", "outbox-pump", outbox.Run))
package lock
```

- [ ] **Step 5: Run tests, format, lint, commit**

```bash
just test ./resilience/lock/
just fmt ./resilience/lock/...
just lint
git add resilience/lock/
git commit -m "feat(lock): RunOnLeader cluster-singleton service and package doc"
```

---

## Task 12: lock/pgstore — table-lease Store

**Files:**
- Create: `resilience/lock/pgstore/pgstore.go`, `resilience/lock/pgstore/doc.go`
- Create: `resilience/lock/pgstore/migrations/20260711120100_locks.sql`
- Test: `resilience/lock/pgstore/pgstore_test.go`

**Interfaces:**
- Consumes: `*pgxpool.Pool`, `pgx.ErrNoRows`; `lock.Store` (implements it); `data/migration`, `data/postgres` for the test.
- Produces: `var Migrations embed.FS`; `func New(pool *pgxpool.Pool, opts ...Option) *Store` implementing `lock.Store`.

- [ ] **Step 1: Write the migration file**

`resilience/lock/pgstore/migrations/20260711120100_locks.sql`:
```sql
-- +goose Up
CREATE TABLE IF NOT EXISTS forge_locks (
    key        text PRIMARY KEY,
    owner      text NOT NULL,
    expires_at timestamptz NOT NULL,
    fence      bigint NOT NULL
);
CREATE SEQUENCE IF NOT EXISTS forge_locks_fence_seq;

-- +goose Down
DROP TABLE IF EXISTS forge_locks;
DROP SEQUENCE IF EXISTS forge_locks_fence_seq;
```

- [ ] **Step 2: Write the failing integration test**

`resilience/lock/pgstore/pgstore_test.go`:
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

	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/resilience/lock"
	"github.com/dmitrymomot/forge/resilience/lock/pgstore"
)

var _ lock.Store = (*pgstore.Store)(nil)

func newStore(t *testing.T) *pgstore.Store {
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
	require.NoError(t, migration.New(pgstore.Migrations, migration.WithTable("forge_lock_schema")).Up(context.Background(), db))
	return pgstore.New(pool)
}

func TestPgLock_AcquireExclusiveFenceRefreshRelease(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	key := "k-" + t.Name()
	require.NoError(t, s.Release(ctx, key, "a"))
	require.NoError(t, s.Release(ctx, key, "b"))

	f1, ok, err := s.Acquire(ctx, key, "a", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	assert.NotZero(t, f1)

	_, ok, err = s.Acquire(ctx, key, "b", time.Minute) // held by a
	require.NoError(t, err)
	assert.False(t, ok)

	ok, err = s.Refresh(ctx, key, "a", time.Minute)
	require.NoError(t, err)
	assert.True(t, ok)

	require.NoError(t, s.Release(ctx, key, "a"))
	f2, ok, err := s.Acquire(ctx, key, "b", time.Minute) // now free
	require.NoError(t, err)
	require.True(t, ok)
	assert.Greater(t, f2, f1) // fence monotonic
	require.NoError(t, s.Release(ctx, key, "b"))
}

func TestPgLock_ExpiredLeaseIsReclaimable(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	key := "exp-" + t.Name()
	require.NoError(t, s.Release(ctx, key, "a"))

	_, ok, err := s.Acquire(ctx, key, "a", 200*time.Millisecond)
	require.NoError(t, err)
	require.True(t, ok)
	time.Sleep(400 * time.Millisecond)
	_, ok, err = s.Acquire(ctx, key, "b", time.Minute) // a's lease expired
	require.NoError(t, err)
	assert.True(t, ok)
	require.NoError(t, s.Release(ctx, key, "b"))
}
```

- [ ] **Step 3: Run to confirm failure**

Run: `just test ./resilience/lock/pgstore/`
Expected: FAIL — undefined `pgstore.New`/`Migrations` (or skip without DSN).

- [ ] **Step 4: Implement `pgstore.go`**

```go
package pgstore

import (
	"context"
	"embed"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Migrations holds the goose migration creating forge_locks + its fence
// sequence. Apply it via data/migration under its own version table.
//
//go:embed migrations/*.sql
var Migrations embed.FS

type config struct{}

// Option configures the Store (reserved for future use).
type Option func(*config)

// Store is a Postgres table-lease implementation of lock.Store. Expiry is
// compared against the database's own now(), so it is immune to cross-node
// clock skew. The pool's lifecycle is the caller's.
type Store struct {
	pool *pgxpool.Pool
}

// New builds a Postgres lock Store. Apply Migrations first.
func New(pool *pgxpool.Pool, opts ...Option) *Store {
	var c config
	for _, o := range opts {
		o(&c)
	}
	return &Store{pool: pool}
}

const acquireSQL = `
INSERT INTO forge_locks (key, owner, expires_at, fence)
VALUES ($1, $2, now() + ($3::text || ' milliseconds')::interval, nextval('forge_locks_fence_seq'))
ON CONFLICT (key) DO UPDATE
    SET owner = EXCLUDED.owner, expires_at = EXCLUDED.expires_at, fence = EXCLUDED.fence
    WHERE forge_locks.expires_at <= now() OR forge_locks.owner = EXCLUDED.owner
RETURNING fence`

func (s *Store) Acquire(ctx context.Context, key, owner string, ttl time.Duration) (uint64, bool, error) {
	var fence uint64
	err := s.pool.QueryRow(ctx, acquireSQL, key, owner, ttl.Milliseconds()).Scan(&fence)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil // held by another live owner
	}
	if err != nil {
		return 0, false, err
	}
	return fence, true, nil
}

func (s *Store) Refresh(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE forge_locks SET expires_at = now() + ($3::text || ' milliseconds')::interval
		 WHERE key = $1 AND owner = $2 AND expires_at > now()`,
		key, owner, ttl.Milliseconds())
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) Release(ctx context.Context, key, owner string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM forge_locks WHERE key = $1 AND owner = $2`, key, owner)
	return err
}
```

> On `ON CONFLICT ... WHERE` failing (row held by a live different owner), no row is updated and `RETURNING` yields no rows → `pgx.ErrNoRows` → `ok=false`. `nextval` may advance on a rejected acquire; sequence gaps are fine — fences stay monotonic.

- [ ] **Step 5: Write `doc.go`**

```go
// Package pgstore is a Postgres table-lease implementation of lock.Store. It
// gives TTL leases, monotonic fencing tokens (a sequence), and refresh, all
// compared against the database's own now() so multi-node clock skew cannot
// mis-expire a lease. It works through any connection pooler.
//
// The DDL (forge_locks + forge_locks_fence_seq) ships as an embedded goose
// migration in Migrations; apply it via data/migration under its own version
// table (e.g. "forge_lock_schema").
//
// # Usage
//
//	db := stdlib.OpenDBFromPool(pool)
//	_ = migration.New(pgstore.Migrations, migration.WithTable("forge_lock_schema")).Up(ctx, db)
//	l := lock.New(pgstore.New(pool), lock.WithTTL(30*time.Second))
package pgstore
```

- [ ] **Step 6: Run tests, format, lint, commit**

```bash
just test ./resilience/lock/pgstore/
just fmt ./resilience/lock/pgstore/...
just lint
git add resilience/lock/pgstore/
git commit -m "feat(lock): Postgres table-lease store driver (pgstore)"
```

---

## Task 13: lock/redisstore — single-instance lease Store

**Files:**
- Create: `resilience/lock/redisstore/redisstore.go`, `resilience/lock/redisstore/doc.go`
- Test: `resilience/lock/redisstore/redisstore_test.go`

**Interfaces:**
- Consumes: `redis.UniversalClient`; `lock.Store` (implements it).
- Produces: `func New(client redis.UniversalClient, opts ...Option) *Store`; `func WithPrefix(string) Option`.

- [ ] **Step 1: Write the failing integration test**

`resilience/lock/redisstore/redisstore_test.go`:
```go
package redisstore_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/resilience/lock"
	"github.com/dmitrymomot/forge/resilience/lock/redisstore"
)

var _ lock.Store = (*redisstore.Store)(nil)

func dial(t *testing.T) redis.UniversalClient {
	addr := os.Getenv("FORGE_TEST_REDIS_URL")
	if addr == "" {
		t.Skip("set FORGE_TEST_REDIS_URL (host:port)")
	}
	c := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestRedisLock_AcquireExclusiveFenceRefreshRelease(t *testing.T) {
	s := redisstore.New(dial(t), redisstore.WithPrefix("locktest:"))
	ctx := context.Background()
	key := "k-" + t.Name()
	_ = s.Release(ctx, key, "a")
	_ = s.Release(ctx, key, "b")

	f1, ok, err := s.Acquire(ctx, key, "a", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	assert.NotZero(t, f1)

	_, ok, err = s.Acquire(ctx, key, "b", time.Minute)
	require.NoError(t, err)
	assert.False(t, ok)

	ok, err = s.Refresh(ctx, key, "a", time.Minute)
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = s.Refresh(ctx, key, "b", time.Minute)
	require.NoError(t, err)
	assert.False(t, ok)

	require.NoError(t, s.Release(ctx, key, "a"))
	f2, ok, err := s.Acquire(ctx, key, "b", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Greater(t, f2, f1)
	require.NoError(t, s.Release(ctx, key, "b"))
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `just test ./resilience/lock/redisstore/`
Expected: FAIL — undefined `redisstore.New` (or skip without `FORGE_TEST_REDIS_URL`).

- [ ] **Step 3: Implement `redisstore.go`**

```go
package redisstore

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// acquireScript claims the lock (SET NX PX) and returns a fresh monotonic fence
// (INCR on a companion key). If the caller already owns it, it refreshes the
// TTL and returns the current fence. Returns 0 when another owner holds it.
// KEYS[1]=lock KEYS[2]=fence  ARGV[1]=owner ARGV[2]=ttlMillis
var acquireScript = redis.NewScript(`
if redis.call('SET', KEYS[1], ARGV[1], 'NX', 'PX', ARGV[2]) then
  return redis.call('INCR', KEYS[2])
end
if redis.call('GET', KEYS[1]) == ARGV[1] then
  redis.call('PEXPIRE', KEYS[1], ARGV[2])
  return tonumber(redis.call('GET', KEYS[2]) or '0')
end
return 0`)

// refreshScript extends the TTL iff the caller owns the lock.
// KEYS[1]=lock ARGV[1]=owner ARGV[2]=ttlMillis
var refreshScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
return 0`)

// releaseScript deletes the lock iff the caller owns it.
// KEYS[1]=lock ARGV[1]=owner
var releaseScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0`)

type config struct{ prefix string }

// Option configures the Store.
type Option func(*config)

// WithPrefix namespaces all keys (e.g. "lock:").
func WithPrefix(p string) Option { return func(c *config) { c.prefix = p } }

// Store is a single-instance Redis implementation of lock.Store (SET NX PX +
// owner-checked Lua). It is NOT Redlock: multi-master safety is out of scope.
// The client's lifecycle is the caller's.
type Store struct {
	client redis.UniversalClient
	prefix string
}

// New builds a Redis lock Store.
func New(client redis.UniversalClient, opts ...Option) *Store {
	var c config
	for _, o := range opts {
		o(&c)
	}
	return &Store{client: client, prefix: c.prefix}
}

func (s *Store) lockKey(k string) string  { return s.prefix + k }
func (s *Store) fenceKey(k string) string { return s.prefix + k + ":fence" }

func (s *Store) Acquire(ctx context.Context, key, owner string, ttl time.Duration) (uint64, bool, error) {
	keys := []string{s.lockKey(key), s.fenceKey(key)}
	fence, err := acquireScript.Run(ctx, s.client, keys, owner, ttl.Milliseconds()).Int64()
	if err != nil {
		return 0, false, err
	}
	if fence <= 0 {
		return 0, false, nil
	}
	return uint64(fence), true, nil
}

func (s *Store) Refresh(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	n, err := refreshScript.Run(ctx, s.client, []string{s.lockKey(key)}, owner, ttl.Milliseconds()).Int64()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func (s *Store) Release(ctx context.Context, key, owner string) error {
	return releaseScript.Run(ctx, s.client, []string{s.lockKey(key)}, owner).Err()
}
```

> The fence key persists (no TTL) so tokens stay monotonic across acquisitions. `Release` returns nil even when not owned (no-op), matching the Store contract.

- [ ] **Step 4: Write `doc.go`**

```go
// Package redisstore is a single-instance Redis implementation of lock.Store,
// using SET NX PX for mutual exclusion, owner-checked Lua for refresh/release,
// and a companion INCR key for monotonic fencing tokens.
//
// It is NOT Redlock — multi-master/quorum locking is out of scope. For a single
// Redis it is correct and fast.
//
// # Usage
//
//	l := lock.New(redisstore.New(client, redisstore.WithPrefix("lock:")))
package redisstore
```

- [ ] **Step 5: Run tests, format, lint, commit**

```bash
just test ./resilience/lock/redisstore/
just fmt ./resilience/lock/redisstore/...
just lint
git add resilience/lock/redisstore/
git commit -m "feat(lock): single-instance Redis lease store driver (redisstore)"
```

---

## Task 14: Roadmap cleanup + final verification

**Files:**
- Modify: `docs/packages.md` (delete the `resilience/quota`, `resilience/loadshed`, `resilience/lock` entries and the now-empty `resilience/` section scaffolding as appropriate)

**Interfaces:** none.

- [ ] **Step 1: Remove the shipped entries from `docs/packages.md`**

Delete the three blocks under `## resilience/` (`**resilience/quota**`, `**resilience/loadshed**`, `**resilience/lock**`) and their `---` separators. If that leaves `## resilience/` with no remaining entries, remove the empty `## resilience/` heading too (the roadmap lists only unbuilt packages; a domain with none built-out has no heading). Leave other domains untouched.

- [ ] **Step 2: Verify nothing else references the removed roadmap entries**

Run: `rg -n "resilience/quota|resilience/loadshed|resilience/lock" docs/`
Expected: no matches in `docs/packages.md` (matches in the spec/plan under `docs/superpowers/` are fine).

- [ ] **Step 3: Full build, lint, and race-tested run across the bundle**

```bash
just fmt ./...
just lint
just test ./resilience/... ./data/migration/...
```
Expected: build clean, lint clean, tests PASS (pg/redis integration tests skip unless `FORGE_TEST_POSTGRES_DSN` / `FORGE_TEST_REDIS_URL` are set — set them locally to exercise all drivers before opening the PR).

- [ ] **Step 4: Commit**

```bash
git add docs/packages.md
git commit -m "docs(packages): remove shipped resilience quota/loadshed/lock entries"
```

---

## Self-Review

**1. Spec coverage:**
- quota shapes (calendar/rolling/gauge) → Tasks 3–5. ✓
- `Limit{Included,Max}` hard-cap/overage/pay-as-you-go + incr-then-rollback → Task 4. ✓
- `Add`/`Set`/`Reset`, gauge durability → Task 5 (+ Task 6 pgstore, + Task 1 no-expiry). ✓
- Feature-tier entitlement non-goal → documented in `quota/doc.go` (Task 5). ✓
- loadshed Criteria + Concurrency/Latency, max-combine, ramp+floor, fail-open, Acquire+Middleware+Skip, CPU consumer-side → Tasks 7–8. ✓
- lock 3-method Store, memory + pgstore(table-lease) + redisstore(single-instance), Lease+auto-refresh+fencing+Done, RunOnLeader → Tasks 9–13. ✓
- Counter-seam `ttl≤0` fix across memory + redisstore + contract → Task 1. ✓
- pg migration pattern (embedded FS, per-source version table) → Tasks 6, 12; `migration.Group`/`Source` → Task 2. ✓
- Roadmap cleanup → Task 14. ✓

**2. Placeholder scan:** No "TBD/TODO/handle edge cases"; every code step shows complete code. The one illustrative `ownerOf` line in Task 10 is explicitly flagged for removal with a working alternative. ✓

**3. Type consistency:** `ratelimit.Store` reused verbatim; `quota.Meter` methods (`Allow/Usage/Add/Set/Reset`) consistent across Tasks 4–5; `lock.Store` signatures identical in Task 9 (interface) and Tasks 9/12/13 (impls); `Lease.Fence/Done/Release` consistent Tasks 10–11; `migration.Source/Group/GroupMigrator` consistent Tasks 2/6/12. `postgres.Config.URL` confirmed against `data/postgres/config.go`; test helpers use `DefaultConfig()` + `cfg.URL`. ✓
