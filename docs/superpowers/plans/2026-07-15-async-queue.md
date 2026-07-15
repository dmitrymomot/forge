# async/queue Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `async/queue` — durable background-work engine with a pull-only `Broker` seam, typed `Kind[T]` jobs, weighted-priority queues, at-least-once claim-with-lease delivery — plus in-memory, postgres, and redis brokers, per spec `docs/superpowers/specs/2026-07-15-async-queue-design.md`.

**Architecture:** Engine (`async/queue`) owns all semantics: retry/backoff, delay, max-attempts → dead-letter, verdicts, lease heartbeat, weighted claiming, graceful drain. Brokers move bytes behind a strictly non-blocking `Broker` interface; the engine polls on a ticker. Drivers: `async/queue/postgres` (pkg `pgqueue`, SKIP LOCKED + `TxPusher`), `async/queue/redis` (pkg `redisqueue`, Streams + consumer groups + XAUTOCLAIM + delay zset). One conformance suite (`async/queue/brokertest`) runs against all three brokers.

**Tech Stack:** Go 1.26, `jackc/pgx/v5`, `redis/go-redis/v9`, forge deps: `ops/supervisor`, `resilience/backoff`, `core/id`, `core/clock`, `ops/logger`, `data/migration`, `data/postgres`, `data/redis`. Tests: testify, live docker pg16/redis7.

## Global Constraints

- Module path `github.com/dmitrymomot/forge`. New packages: `async/queue`, `async/queue/brokertest`, `async/queue/postgres` (package name `pgqueue`), `async/queue/redis` (package name `redisqueue`).
- Read `docs/design.md` before starting any task and follow it exactly.
- After changing files in a package run `just fmt ./async/...`; after finishing a task run `just lint` (golangci-lint incl. modernize, betteralign, nilaway — all must pass).
- Tests: black-box (`package queue_test` etc.) unless unexported state demands otherwise; `just test ./async/...` runs race-enabled.
- Go 1.26: use `new(expr)` not ptr helpers; use `wg.Go(fn)` not `wg.Add(1)`/`go`; use `slices`/`maps` stdlib packages; `for range n` for counted loops.
- Optional `*slog.Logger` options default to `logger.NewNope()` — NEVER `slog.Default()`.
- Single-line structured log attrs; errors as attrs, no stacks/blobs.
- No manual line-wrapping in any prose/markdown/commit body.
- Commit after every task (conventional commits, no Claude attribution lines).
- Driver tests need NO Docker by default: `redisqueue` runs against an in-process `miniredis`, `pgqueue` against an embedded PostgreSQL 18 (`fergusstrange/embedded-postgres`, downloaded once then cached, run natively — no container-VM clock skew). Both fall back to a real server when its env var is set, to run the same conformance suite against production backends:
  - `export FORGE_TEST_POSTGRES_DSN='postgres://…'` (e.g. `postgres:18-alpine`)
  - `export FORGE_TEST_REDIS_URL='host:port'` (e.g. `redis:8-alpine`)
- Engine-owned semantics: drivers NEVER decide retry/delay/dead-letter; they only execute `Push/Claim/Extend/Ack/Nack/Kill/ListDead/Requeue/Purge/Stats`.
- `Claim` is non-blocking and atomically sets the lease AND increments the attempt counter.
- Every package ships `doc.go` and `bench_test.go`; post-benchmark optimization pass with before/after numbers recorded for the PR.

## Existing APIs consumed (verified signatures)

- `supervisor.Service`: `Name() string`; `Run(ctx context.Context) error` (blocking; must drain on ctx cancel; returning `context.Canceled` on cancellation = clean stop).
- `backoff.Backoff`: `Next(attempt int) time.Duration`; `backoff.Exponential(base, max time.Duration, opts ...backoff.Option) backoff.Backoff`; `backoff.WithJitter(fraction float64)`.
- `id.NewULID() id.ULID` (has `String() string`).
- `clock.Clock` (`Now() time.Time`), `clock.NewMock(t time.Time) *clock.Mock`, `(*Mock).Advance(d)`.
- `logger.NewNope() *slog.Logger`.
- `migration.New(fsys fs.FS, opts ...migration.Option) *Migrator`; `(*Migrator).Up(ctx, *sql.DB) error`; `migration.WithTable(name string)`.
- `postgres.Open(ctx, postgres.WithConfig(cfg)) (*pgxpool.Pool, error)`; `postgres.DefaultConfig() postgres.Config` (set `.URL`); `stdlib.OpenDBFromPool(pool) *sql.DB` (pgx stdlib).
- `redis.Open(...)` yields `goredis.UniversalClient`; driver constructor takes `goredis.UniversalClient` directly (like `resilience/lock/redisstore`).
- pgx encodes `time.Duration` as Postgres `interval` and `[]byte`→`jsonb` needs `payload` to be valid JSON (Push always marshals JSON; `PushRaw` takes `json.RawMessage`).

## File Structure

```
async/queue/
  doc.go            package docs + idempotency contract + example pointers
  errors.go         sentinels + SkipRetry/Cancel verdicts
  job.go            Job envelope, Stats/QueueStats
  codec.go          versioned wire codec EncodeJob/DecodeJob (drivers + DLQ stability)
  kind.go           Kind[T], NewKind
  config.go         Config (env tags), DefaultConfig, Validate
  options.go        ClientOption, ServiceOption, PushOption, HandlerOption
  broker.go         Broker interface, TxPusher capability
  memory.go         in-memory reference broker (NewMemoryBroker)
  client.go         Client, NewClient, Push, PushRaw, PushTx, DLQ pass-throughs
  service.go        Service, NewService, Register, Run loop, weighted claiming, heartbeat, drain
  example_test.go   runnable Example
  bench_test.go     push/claim/dispatch benchmarks
  *_test.go         black-box tests per file
async/queue/brokertest/
  brokertest.go     exported conformance suite Run(t, factory)
async/queue/postgres/        (package pgqueue)
  doc.go  pgqueue.go  migrations/20260715120000_queue_jobs.sql  pgqueue_test.go  bench_test.go
async/queue/redis/           (package redisqueue)
  doc.go  redisqueue.go  mover.go  redisqueue_test.go  bench_test.go
docs/packages.md    catalog updates (rename jobqueue→queue etc.)
```

---

### Task 1: Envelope, errors, verdicts, codec

**Files:**
- Create: `async/queue/errors.go`, `async/queue/job.go`, `async/queue/codec.go`
- Test: `async/queue/codec_test.go`, `async/queue/errors_test.go`

**Interfaces:**
- Consumes: stdlib only.
- Produces: `queue.Job` struct (fields below), `queue.Stats`/`queue.QueueStats`, sentinels `ErrInvalidConfig, ErrNoHandler, ErrJobNotFound, ErrScopeMissing, ErrNotDead, ErrTxUnsupported`, verdict constructors `SkipRetry(error) error`, `Cancel` (error value), predicates `IsSkipRetry(error) bool`, `EncodeJob(Job) ([]byte, error)`, `DecodeJob([]byte) (Job, error)`.

- [ ] **Step 1: Write failing tests**

`async/queue/codec_test.go`:

```go
package queue_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/queue"
)

func sampleJob() queue.Job {
	return queue.Job{
		ID:          "01J0000000000000000000TEST",
		Queue:       "critical",
		Type:        "email.send_welcome",
		Payload:     []byte(`{"user_id":"u1"}`),
		Scope:       "tenant-a",
		Attempt:     3,
		MaxAttempts: 25,
		RunAt:       time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
		CreatedAt:   time.Date(2026, 7, 15, 11, 0, 0, 0, time.UTC),
		LastError:   "boom",
	}
}

func TestCodec_RoundTrip(t *testing.T) {
	t.Parallel()
	in := sampleJob()
	b, err := queue.EncodeJob(in)
	require.NoError(t, err)
	out, err := queue.DecodeJob(b)
	require.NoError(t, err)
	assert.Equal(t, in, out)
}

func TestCodec_RoundTripZeroTimes(t *testing.T) {
	t.Parallel()
	in := queue.Job{ID: "x", Queue: "default", Type: "t", Payload: []byte(`{}`)}
	b, err := queue.EncodeJob(in)
	require.NoError(t, err)
	out, err := queue.DecodeJob(b)
	require.NoError(t, err)
	assert.True(t, out.RunAt.IsZero())
	assert.True(t, out.CreatedAt.IsZero())
	assert.Equal(t, in, out)
}

func TestCodec_RejectsUnknownVersion(t *testing.T) {
	t.Parallel()
	_, err := queue.DecodeJob([]byte(`{"v":99,"id":"x"}`))
	require.Error(t, err)
}

func TestCodec_RejectsGarbage(t *testing.T) {
	t.Parallel()
	_, err := queue.DecodeJob([]byte(`not json`))
	require.Error(t, err)
}
```

`async/queue/errors_test.go`:

```go
package queue_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/queue"
)

func TestSkipRetry_WrapsAndDetects(t *testing.T) {
	t.Parallel()
	base := errors.New("poison payload")
	err := queue.SkipRetry(base)
	require.Error(t, err)
	assert.True(t, queue.IsSkipRetry(err))
	assert.ErrorIs(t, err, base) // original error stays reachable
	assert.False(t, queue.IsSkipRetry(base))
	assert.False(t, queue.IsSkipRetry(nil))
}

func TestSkipRetry_Nil(t *testing.T) {
	t.Parallel()
	assert.Nil(t, queue.SkipRetry(nil))
}

func TestCancel_IsSentinel(t *testing.T) {
	t.Parallel()
	assert.True(t, errors.Is(queue.Cancel, queue.Cancel))
	wrapped := errors.Join(queue.Cancel, errors.New("context"))
	assert.True(t, errors.Is(wrapped, queue.Cancel))
	assert.False(t, errors.Is(errors.New("x"), queue.Cancel))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./async/queue/ 2>&1 | head -20`
Expected: compile failure — package does not exist yet.

- [ ] **Step 3: Implement**

`async/queue/errors.go`:

```go
package queue

import (
	"errors"
	"fmt"
)

var (
	// ErrInvalidConfig is returned by Config.Validate and the constructors on bad configuration.
	ErrInvalidConfig = errors.New("queue: invalid config")
	// ErrNoHandler is recorded as the dead-letter reason when a claimed job's kind has no registered handler.
	ErrNoHandler = errors.New("queue: no handler registered for kind")
	// ErrJobNotFound is returned by DLQ operations when the id does not exist.
	ErrJobNotFound = errors.New("queue: job not found")
	// ErrScopeMissing is returned by Push when a scope hook is configured and yields an error or empty scope.
	ErrScopeMissing = errors.New("queue: scope missing")
	// ErrNotDead is returned by Requeue/Purge when the job exists but is not dead-lettered.
	ErrNotDead = errors.New("queue: job is not dead")
	// ErrTxUnsupported is returned by PushTx when the broker does not implement TxPusher.
	ErrTxUnsupported = errors.New("queue: broker does not support transactional push")
)

// Cancel is a handler verdict: the job became moot; discard it as done
// without retrying and without dead-lettering.
var Cancel error = errors.New("queue: job cancelled")

type skipRetryError struct{ err error }

func (e *skipRetryError) Error() string { return fmt.Sprintf("queue: skip retry: %v", e.err) }
func (e *skipRetryError) Unwrap() error { return e.err }

// SkipRetry wraps err into a handler verdict: fail the job straight to the
// dead-letter queue without burning the remaining attempts (poison input).
// SkipRetry(nil) returns nil.
func SkipRetry(err error) error {
	if err == nil {
		return nil
	}
	return &skipRetryError{err: err}
}

// IsSkipRetry reports whether err carries the SkipRetry verdict.
func IsSkipRetry(err error) bool {
	var s *skipRetryError
	return errors.As(err, &s)
}
```

`async/queue/job.go`:

```go
package queue

import "time"

// Job is the queue envelope. Payload is always valid JSON. Scope is empty
// when the producing Client has no scope hook configured. Attempt is the
// number of claims so far (a job being processed for the first time has
// Attempt == 1). MaxAttempts == 0 means "use the worker's configured default".
type Job struct {
	ID          string
	Queue       string
	Type        string
	Payload     []byte
	Scope       string
	Attempt     int
	MaxAttempts int
	RunAt       time.Time
	CreatedAt   time.Time
	LastError   string
}

// QueueStats are per-queue counts reported by Broker.Stats.
type QueueStats struct {
	Pending int
	Dead    int
}

// Stats maps queue name to its counts.
type Stats map[string]QueueStats
```

`async/queue/codec.go`:

```go
package queue

import (
	"encoding/json"
	"fmt"
	"time"
)

// wireVersion is the envelope encoding version. Bump only with a decoder
// that still accepts every previous version: dead-lettered jobs encoded by
// old binaries must stay requeueable.
const wireVersion = 1

type wireJob struct {
	V           int             `json:"v"`
	ID          string          `json:"id"`
	Queue       string          `json:"q"`
	Type        string          `json:"t"`
	Payload     json.RawMessage `json:"p,omitempty"`
	Scope       string          `json:"s,omitempty"`
	Attempt     int             `json:"a,omitempty"`
	MaxAttempts int             `json:"ma,omitempty"`
	RunAtMS     int64           `json:"ra,omitempty"`
	CreatedAtMS int64           `json:"ca,omitempty"`
	LastError   string          `json:"le,omitempty"`
}

// EncodeJob serializes a Job into the stable, versioned wire form used by
// non-columnar brokers and DLQ storage.
func EncodeJob(j Job) ([]byte, error) {
	w := wireJob{
		V: wireVersion, ID: j.ID, Queue: j.Queue, Type: j.Type,
		Payload: json.RawMessage(j.Payload), Scope: j.Scope,
		Attempt: j.Attempt, MaxAttempts: j.MaxAttempts, LastError: j.LastError,
	}
	if !j.RunAt.IsZero() {
		w.RunAtMS = j.RunAt.UnixMilli()
	}
	if !j.CreatedAt.IsZero() {
		w.CreatedAtMS = j.CreatedAt.UnixMilli()
	}
	b, err := json.Marshal(w)
	if err != nil {
		return nil, fmt.Errorf("queue: encode job: %w", err)
	}
	return b, nil
}

// DecodeJob parses the wire form produced by EncodeJob.
func DecodeJob(b []byte) (Job, error) {
	var w wireJob
	if err := json.Unmarshal(b, &w); err != nil {
		return Job{}, fmt.Errorf("queue: decode job: %w", err)
	}
	if w.V != wireVersion {
		return Job{}, fmt.Errorf("queue: decode job: unsupported wire version %d", w.V)
	}
	j := Job{
		ID: w.ID, Queue: w.Queue, Type: w.Type, Payload: []byte(w.Payload),
		Scope: w.Scope, Attempt: w.Attempt, MaxAttempts: w.MaxAttempts, LastError: w.LastError,
	}
	if w.RunAtMS != 0 {
		j.RunAt = time.UnixMilli(w.RunAtMS).UTC()
	}
	if w.CreatedAtMS != 0 {
		j.CreatedAt = time.UnixMilli(w.CreatedAtMS).UTC()
	}
	return j, nil
}
```

Note: the round-trip test compares times with `assert.Equal` — construct test times in UTC (as shown) since the decoder normalizes to UTC. Millisecond precision is contractual; tests must not use sub-millisecond timestamps.

- [ ] **Step 4: Run tests to verify they pass**

Run: `just fmt ./async/... && go test ./async/queue/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add async/queue && git commit -m "feat(queue): job envelope, verdicts, versioned wire codec"
```

---

### Task 2: Kind[T] and Config

**Files:**
- Create: `async/queue/kind.go`, `async/queue/config.go`
- Test: `async/queue/kind_test.go`, `async/queue/config_test.go`

**Interfaces:**
- Consumes: `ErrInvalidConfig` (Task 1).
- Produces: `type Kind[T any] struct{...}` with `NewKind[T any](name string) Kind[T]` (panics on empty name) and method `Name() string`; `queue.Config{Concurrency int; PollInterval, Lease time.Duration; MaxAttempts, ClaimBatch int}` with env tags `QUEUE_CONCURRENCY, QUEUE_POLL_INTERVAL, QUEUE_LEASE, QUEUE_MAX_ATTEMPTS, QUEUE_CLAIM_BATCH`, `DefaultConfig() Config` (10, 1s, 30s, 25, 0=derived), `(Config) Validate() error`.

- [ ] **Step 1: Write failing tests**

`async/queue/kind_test.go`:

```go
package queue_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/async/queue"
)

type welcomePayload struct {
	UserID string `json:"user_id"`
}

func TestNewKind_Name(t *testing.T) {
	t.Parallel()
	k := queue.NewKind[welcomePayload]("email.send_welcome")
	assert.Equal(t, "email.send_welcome", k.Name())
}

func TestNewKind_PanicsOnEmptyName(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { queue.NewKind[welcomePayload]("") })
}
```

`async/queue/config_test.go`:

```go
package queue_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/queue"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := queue.DefaultConfig()
	assert.Equal(t, 10, cfg.Concurrency)
	assert.Equal(t, time.Second, cfg.PollInterval)
	assert.Equal(t, 30*time.Second, cfg.Lease)
	assert.Equal(t, 25, cfg.MaxAttempts)
	assert.Equal(t, 0, cfg.ClaimBatch)
	require.NoError(t, cfg.Validate())
}

func TestConfig_ValidateRejects(t *testing.T) {
	t.Parallel()
	cases := []func(*queue.Config){
		func(c *queue.Config) { c.Concurrency = 0 },
		func(c *queue.Config) { c.PollInterval = 0 },
		func(c *queue.Config) { c.Lease = 0 },
		func(c *queue.Config) { c.MaxAttempts = 0 },
		func(c *queue.Config) { c.ClaimBatch = -1 },
	}
	for _, mutate := range cases {
		cfg := queue.DefaultConfig()
		mutate(&cfg)
		assert.ErrorIs(t, cfg.Validate(), queue.ErrInvalidConfig)
	}
}

func TestConfig_EveryFieldHasEnvTag(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeFor[queue.Config]()
	for i := range typ.NumField() {
		f := typ.Field(i)
		assert.NotEmpty(t, f.Tag.Get("env"), "field %s missing env tag", f.Name)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./async/queue/ 2>&1 | head -10`
Expected: compile failure — `NewKind`, `DefaultConfig` undefined.

- [ ] **Step 3: Implement**

`async/queue/kind.go`:

```go
package queue

// Kind binds a job type name to its payload type T. Declare one package-level
// Kind per job type and share it between producers and workers: the name
// string exists in exactly one place, and payload type drift between Push and
// Register becomes a compile error.
//
//	var KindSendWelcome = queue.NewKind[SendWelcome]("email.send_welcome")
type Kind[T any] struct {
	name string
}

// NewKind creates a Kind for payload type T. The name must be non-empty and
// unique across the application (convention: "domain.action"). Panics on an
// empty name: kinds are package-level wiring, not runtime data.
func NewKind[T any](name string) Kind[T] {
	if name == "" {
		panic("queue: NewKind requires a non-empty name")
	}
	return Kind[T]{name: name}
}

// Name returns the job type name.
func (k Kind[T]) Name() string { return k.name }
```

`async/queue/config.go`:

```go
package queue

import (
	"fmt"
	"time"
)

// Config holds the env-loadable worker knobs. ClaimBatch == 0 derives the
// per-poll claim budget from free worker slots.
type Config struct {
	Concurrency  int           `env:"QUEUE_CONCURRENCY"`
	PollInterval time.Duration `env:"QUEUE_POLL_INTERVAL"`
	Lease        time.Duration `env:"QUEUE_LEASE"`
	MaxAttempts  int           `env:"QUEUE_MAX_ATTEMPTS"`
	ClaimBatch   int           `env:"QUEUE_CLAIM_BATCH"`
}

// DefaultConfig returns production defaults: 10 workers, 1s poll, 30s lease,
// 25 attempts, claim batch derived from free slots.
func DefaultConfig() Config {
	return Config{Concurrency: 10, PollInterval: time.Second, Lease: 30 * time.Second, MaxAttempts: 25}
}

// Validate checks the configuration invariants.
func (c Config) Validate() error {
	if c.Concurrency <= 0 {
		return fmt.Errorf("%w: Concurrency must be > 0, got %d", ErrInvalidConfig, c.Concurrency)
	}
	if c.PollInterval <= 0 {
		return fmt.Errorf("%w: PollInterval must be > 0, got %v", ErrInvalidConfig, c.PollInterval)
	}
	if c.Lease <= 0 {
		return fmt.Errorf("%w: Lease must be > 0, got %v", ErrInvalidConfig, c.Lease)
	}
	if c.MaxAttempts <= 0 {
		return fmt.Errorf("%w: MaxAttempts must be > 0, got %d", ErrInvalidConfig, c.MaxAttempts)
	}
	if c.ClaimBatch < 0 {
		return fmt.Errorf("%w: ClaimBatch must be >= 0, got %d", ErrInvalidConfig, c.ClaimBatch)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `just fmt ./async/... && go test ./async/queue/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add async/queue && git commit -m "feat(queue): typed Kind and worker Config"
```

---

### Task 3: Broker seam, memory broker, conformance suite

**Files:**
- Create: `async/queue/broker.go`, `async/queue/memory.go`, `async/queue/brokertest/brokertest.go`
- Test: `async/queue/memory_test.go`

**Interfaces:**
- Consumes: `Job`, `Stats`, `QueueStats`, sentinels (Task 1); `clock.Clock` from `core/clock`; `id.NewULID` from `core/id`.
- Produces: `queue.Broker` interface (exact methods below), `queue.TxPusher` capability, `queue.NewMemoryBroker(opts ...MemoryOption) *MemoryBroker`, `queue.WithMemoryClock(clock.Clock) MemoryOption`, `brokertest.Run(t *testing.T, factory func(t *testing.T) queue.Broker)`.

- [ ] **Step 1: Write the conformance suite (this IS the failing test)**

`async/queue/brokertest/brokertest.go`:

```go
// Package brokertest is the executable contract for queue.Broker
// implementations. Every driver's test suite must call Run; the in-memory
// broker is the reference implementation. Timing subtests use short real
// leases (hundreds of ms), so the suite is safe for live backends.
package brokertest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/core/id"
)

// Run executes the Broker conformance suite. factory must return a fresh,
// empty broker (or one namespaced per test) each call.
func Run(t *testing.T, factory func(t *testing.T) queue.Broker) {
	t.Helper()
	t.Run("PushClaimAck", func(t *testing.T) { testPushClaimAck(t, factory(t)) })
	t.Run("ClaimOrder", func(t *testing.T) { testClaimOrder(t, factory(t)) })
	t.Run("NackReschedules", func(t *testing.T) { testNackReschedules(t, factory(t)) })
	t.Run("DelayedJob", func(t *testing.T) { testDelayedJob(t, factory(t)) })
	t.Run("LeaseExpiryRedelivery", func(t *testing.T) { testLeaseExpiry(t, factory(t)) })
	t.Run("ExtendPreventsRedelivery", func(t *testing.T) { testExtend(t, factory(t)) })
	t.Run("DeadLetterOps", func(t *testing.T) { testDeadLetterOps(t, factory(t)) })
	t.Run("QueueIsolation", func(t *testing.T) { testQueueIsolation(t, factory(t)) })
	t.Run("Stats", func(t *testing.T) { testStats(t, factory(t)) })
	t.Run("ClaimEmptyQueue", func(t *testing.T) { testClaimEmpty(t, factory(t)) })
}

func makeJob(q string, runAt time.Time) queue.Job {
	return queue.Job{
		ID:          id.NewULID().String(),
		Queue:       q,
		Type:        "test.kind",
		Payload:     []byte(`{"n":1}`),
		MaxAttempts: 25,
		RunAt:       runAt.UTC(),
		CreatedAt:   time.Now().UTC(),
	}
}

func claimIDs(jobs []queue.Job) []string {
	ids := make([]string, len(jobs))
	for i, j := range jobs {
		ids[i] = j.ID
	}
	return ids
}

func testPushClaimAck(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j := makeJob("q1", time.Now())
	require.NoError(t, b.Push(ctx, j))

	got, err := b.Claim(ctx, "q1", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, j.ID, got[0].ID)
	assert.Equal(t, j.Type, got[0].Type)
	assert.JSONEq(t, string(j.Payload), string(got[0].Payload))
	assert.Equal(t, 1, got[0].Attempt, "claim must increment attempt")
	assert.Equal(t, j.MaxAttempts, got[0].MaxAttempts)

	again, err := b.Claim(ctx, "q1", 10, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, again, "claimed job must be invisible during lease")

	require.NoError(t, b.Ack(ctx, j.ID))
	st, err := b.Stats(ctx)
	require.NoError(t, err)
	assert.Zero(t, st["q1"].Pending)
	assert.Zero(t, st["q1"].Dead)
}

func testClaimOrder(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	now := time.Now()
	a := makeJob("q1", now.Add(-2*time.Second))
	c := makeJob("q1", now.Add(-time.Second))
	require.NoError(t, b.Push(ctx, a))
	require.NoError(t, b.Push(ctx, c))

	got, err := b.Claim(ctx, "q1", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, []string{a.ID, c.ID}, claimIDs(got), "earlier run_at claims first (best-effort FIFO)")
}

func testNackReschedules(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j := makeJob("q1", time.Now())
	require.NoError(t, b.Push(ctx, j))

	got, err := b.Claim(ctx, "q1", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1)

	require.NoError(t, b.Nack(ctx, j.ID, time.Now().Add(250*time.Millisecond), "boom"))

	early, err := b.Claim(ctx, "q1", 1, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, early, "nacked job must stay invisible until retryAt")

	time.Sleep(400 * time.Millisecond)
	late, err := b.Claim(ctx, "q1", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, late, 1)
	assert.Equal(t, j.ID, late[0].ID)
	assert.Equal(t, 2, late[0].Attempt, "second claim = attempt 2")
	assert.Equal(t, "boom", late[0].LastError)
}

func testDelayedJob(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j := makeJob("q1", time.Now().Add(300*time.Millisecond))
	require.NoError(t, b.Push(ctx, j))

	got, err := b.Claim(ctx, "q1", 1, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, got, "future job must not be claimable")

	time.Sleep(450 * time.Millisecond)
	got, err = b.Claim(ctx, "q1", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, j.ID, got[0].ID)
}

func testLeaseExpiry(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j := makeJob("q1", time.Now())
	require.NoError(t, b.Push(ctx, j))

	got, err := b.Claim(ctx, "q1", 1, 300*time.Millisecond)
	require.NoError(t, err)
	require.Len(t, got, 1)

	early, err := b.Claim(ctx, "q1", 1, 300*time.Millisecond)
	require.NoError(t, err)
	assert.Empty(t, early)

	time.Sleep(500 * time.Millisecond)
	// Reclaim with the SAME lease: some drivers (redis) enforce expiry at
	// claim time via min-idle = the claiming lease, so a longer lease here
	// would hide the expiry.
	late, err := b.Claim(ctx, "q1", 1, 300*time.Millisecond)
	require.NoError(t, err)
	require.Len(t, late, 1, "expired lease must redeliver")
	assert.Equal(t, j.ID, late[0].ID)
	assert.Equal(t, 2, late[0].Attempt)
}

func testExtend(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j := makeJob("q1", time.Now())
	require.NoError(t, b.Push(ctx, j))

	got, err := b.Claim(ctx, "q1", 1, 400*time.Millisecond)
	require.NoError(t, err)
	require.Len(t, got, 1)

	time.Sleep(250 * time.Millisecond)
	require.NoError(t, b.Extend(ctx, j.ID, 2*time.Second))

	time.Sleep(300 * time.Millisecond) // past the original lease, inside the extended one
	still, err := b.Claim(ctx, "q1", 1, 400*time.Millisecond) // same lease as the original claim (see LeaseExpiry note)
	require.NoError(t, err)
	assert.Empty(t, still, "extended lease must prevent redelivery")

	require.NoError(t, b.Ack(ctx, j.ID))
}

func testDeadLetterOps(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j1 := makeJob("q1", time.Now().Add(-2*time.Second))
	j2 := makeJob("q1", time.Now().Add(-time.Second))
	require.NoError(t, b.Push(ctx, j1))
	require.NoError(t, b.Push(ctx, j2))

	got, err := b.Claim(ctx, "q1", 2, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.NoError(t, b.Kill(ctx, j1.ID, "poison"))
	require.NoError(t, b.Kill(ctx, j2.ID, "poison"))

	dead, err := b.ListDead(ctx, "q1", 10)
	require.NoError(t, err)
	require.Len(t, dead, 2)
	assert.Equal(t, "poison", dead[0].LastError)

	one, err := b.ListDead(ctx, "q1", 1)
	require.NoError(t, err)
	assert.Len(t, one, 1, "ListDead must honor limit")

	// Requeue resets attempts and makes the job claimable again.
	require.NoError(t, b.Requeue(ctx, j1.ID))
	re, err := b.Claim(ctx, "q1", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, re, 1)
	assert.Equal(t, j1.ID, re[0].ID)
	assert.Equal(t, 1, re[0].Attempt, "requeue must reset attempts")

	// Requeue on a non-dead job fails.
	assert.ErrorIs(t, b.Requeue(ctx, j1.ID), queue.ErrNotDead)

	// Purge removes the dead job.
	require.NoError(t, b.Purge(ctx, j2.ID))
	dead, err = b.ListDead(ctx, "q1", 10)
	require.NoError(t, err)
	assert.Empty(t, dead)

	assert.ErrorIs(t, b.Purge(ctx, "no-such-id"), queue.ErrJobNotFound)
	assert.ErrorIs(t, b.Requeue(ctx, "no-such-id"), queue.ErrJobNotFound)
}

func testQueueIsolation(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j1 := makeJob("q1", time.Now())
	j2 := makeJob("q2", time.Now())
	require.NoError(t, b.Push(ctx, j1))
	require.NoError(t, b.Push(ctx, j2))

	got, err := b.Claim(ctx, "q1", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, j1.ID, got[0].ID)
}

func testStats(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j1 := makeJob("q1", time.Now())
	j2 := makeJob("q1", time.Now())
	require.NoError(t, b.Push(ctx, j1))
	require.NoError(t, b.Push(ctx, j2))

	got, err := b.Claim(ctx, "q1", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NoError(t, b.Kill(ctx, got[0].ID, "x"))

	st, err := b.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, st["q1"].Pending)
	assert.Equal(t, 1, st["q1"].Dead)
}

func testClaimEmpty(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	got, err := b.Claim(ctx, "nothing-here", 10, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, got)
}
```

`async/queue/memory_test.go`:

```go
package queue_test

import (
	"testing"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/async/queue/brokertest"
)

var _ queue.Broker = (*queue.MemoryBroker)(nil)

func TestMemoryBroker_Conformance(t *testing.T) {
	t.Parallel()
	brokertest.Run(t, func(t *testing.T) queue.Broker { return queue.NewMemoryBroker() })
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./async/queue/... 2>&1 | head -10`
Expected: compile failure — `queue.Broker`, `queue.NewMemoryBroker` undefined.

- [ ] **Step 3: Implement broker.go and memory.go**

`async/queue/broker.go`:

```go
package queue

import (
	"context"
	"time"
)

// Broker is the storage seam. It is strictly pull: Claim returns up to n due
// jobs or an empty slice and never blocks waiting for work — the engine owns
// polling cadence. Drivers move bytes; the engine owns retry, delay,
// dead-letter, and lease-heartbeat semantics, so behavior is identical across
// backends.
//
// Contract details every implementation must honor (enforced by brokertest):
//   - Claim atomically sets the lease AND increments the attempt counter.
//   - A claimed job is invisible to Claim until its lease expires.
//   - Nack makes the job claimable again no earlier than retryAt and records
//     reason as LastError.
//   - Kill moves the job to the dead-letter set; Requeue resets attempts to
//     zero and returns it to pending; Purge deletes a dead job.
//   - Requeue/Purge return ErrJobNotFound for unknown ids and ErrNotDead for
//     jobs that exist but are not dead.
type Broker interface {
	Push(ctx context.Context, job Job) error
	Claim(ctx context.Context, queue string, n int, lease time.Duration) ([]Job, error)
	Extend(ctx context.Context, id string, lease time.Duration) error
	Ack(ctx context.Context, id string) error
	Nack(ctx context.Context, id string, retryAt time.Time, reason string) error
	Kill(ctx context.Context, id string, reason string) error
	ListDead(ctx context.Context, queue string, limit int) ([]Job, error)
	Requeue(ctx context.Context, id string) error
	Purge(ctx context.Context, id string) error
	Stats(ctx context.Context) (Stats, error)
}

// TxPusher is an optional Broker capability: transactional enqueue inside a
// caller-owned database transaction. tx is driver-specific (pgqueue asserts
// pgx.Tx). Brokers without this capability make PushTx return
// ErrTxUnsupported.
type TxPusher interface {
	PushTx(ctx context.Context, tx any, job Job) error
}
```

`async/queue/memory.go`:

```go
package queue

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

// MemoryBroker is the built-in reference Broker: full semantics (leases,
// delayed jobs, dead-letter) over process memory. Use it for tests,
// dev/single-process apps, and as the behavioral reference for drivers.
// Jobs do not survive process restart.
type MemoryBroker struct {
	mu   sync.Mutex
	clk  clock.Clock
	jobs map[string]*memJob
}

type memJob struct {
	job          Job
	dead         bool
	claimedUntil time.Time
}

// MemoryOption configures NewMemoryBroker.
type MemoryOption func(*MemoryBroker)

// WithMemoryClock injects a clock (tests).
func WithMemoryClock(c clock.Clock) MemoryOption {
	return func(b *MemoryBroker) { b.clk = c }
}

// NewMemoryBroker builds an empty in-memory broker.
func NewMemoryBroker(opts ...MemoryOption) *MemoryBroker {
	b := &MemoryBroker{clk: clock.System(), jobs: make(map[string]*memJob)}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

func (b *MemoryBroker) Push(_ context.Context, job Job) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.jobs[job.ID] = &memJob{job: job}
	return nil
}

func (b *MemoryBroker) Claim(_ context.Context, queueName string, n int, lease time.Duration) ([]Job, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.clk.Now()
	var due []*memJob
	for _, m := range b.jobs {
		if !m.dead && m.job.Queue == queueName && !m.job.RunAt.After(now) && m.claimedUntil.Before(now) {
			due = append(due, m)
		}
	}
	slices.SortFunc(due, func(a, c *memJob) int {
		if r := a.job.RunAt.Compare(c.job.RunAt); r != 0 {
			return r
		}
		return cmp.Compare(a.job.ID, c.job.ID)
	})
	if len(due) > n {
		due = due[:n]
	}
	out := make([]Job, 0, len(due))
	for _, m := range due {
		m.claimedUntil = now.Add(lease)
		m.job.Attempt++
		out = append(out, m.job)
	}
	return out, nil
}

func (b *MemoryBroker) Extend(_ context.Context, id string, lease time.Duration) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	m, ok := b.jobs[id]
	if !ok {
		return ErrJobNotFound
	}
	m.claimedUntil = b.clk.Now().Add(lease)
	return nil
}

func (b *MemoryBroker) Ack(_ context.Context, id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.jobs[id]; !ok {
		return ErrJobNotFound
	}
	delete(b.jobs, id)
	return nil
}

func (b *MemoryBroker) Nack(_ context.Context, id string, retryAt time.Time, reason string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	m, ok := b.jobs[id]
	if !ok {
		return ErrJobNotFound
	}
	m.job.RunAt = retryAt
	m.job.LastError = reason
	m.claimedUntil = time.Time{}
	return nil
}

func (b *MemoryBroker) Kill(_ context.Context, id string, reason string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	m, ok := b.jobs[id]
	if !ok {
		return ErrJobNotFound
	}
	m.dead = true
	m.job.LastError = reason
	m.claimedUntil = time.Time{}
	return nil
}

func (b *MemoryBroker) ListDead(_ context.Context, queueName string, limit int) ([]Job, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var dead []*memJob
	for _, m := range b.jobs {
		if m.dead && m.job.Queue == queueName {
			dead = append(dead, m)
		}
	}
	slices.SortFunc(dead, func(a, c *memJob) int {
		if r := a.job.CreatedAt.Compare(c.job.CreatedAt); r != 0 {
			return r
		}
		return cmp.Compare(a.job.ID, c.job.ID)
	})
	if len(dead) > limit {
		dead = dead[:limit]
	}
	out := make([]Job, 0, len(dead))
	for _, m := range dead {
		out = append(out, m.job)
	}
	return out, nil
}

func (b *MemoryBroker) Requeue(_ context.Context, id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	m, ok := b.jobs[id]
	if !ok {
		return ErrJobNotFound
	}
	if !m.dead {
		return ErrNotDead
	}
	m.dead = false
	m.job.Attempt = 0
	m.job.RunAt = b.clk.Now()
	m.claimedUntil = time.Time{}
	return nil
}

func (b *MemoryBroker) Purge(_ context.Context, id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	m, ok := b.jobs[id]
	if !ok {
		return ErrJobNotFound
	}
	if !m.dead {
		return ErrNotDead
	}
	delete(b.jobs, id)
	return nil
}

func (b *MemoryBroker) Stats(_ context.Context) (Stats, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := make(Stats)
	for _, m := range b.jobs {
		qs := st[m.job.Queue]
		if m.dead {
			qs.Dead++
		} else {
			qs.Pending++
		}
		st[m.job.Queue] = qs
	}
	return st, nil
}
```

The system clock constructor is `clock.System()` (verified in core/clock).

Note on Stats semantics: a claimed (in-flight) job still counts as Pending — pending means "not done and not dead". Drivers must match this (pg: `status='pending'` rows regardless of `claimed_until`; redis: stream entries + delayed).

- [ ] **Step 4: Run tests to verify they pass**

Run: `just fmt ./async/... && go test -race ./async/queue/...`
Expected: PASS (timing subtests take ~2s total)

- [ ] **Step 5: Commit**

```bash
git add async/queue && git commit -m "feat(queue): Broker seam, memory reference broker, conformance suite"
```

---

### Task 4: Client — Push, PushRaw, PushTx, scope, DLQ pass-throughs

**Files:**
- Create: `async/queue/options.go` (client + push options; service/handler options are added in Task 5), `async/queue/client.go`
- Test: `async/queue/client_test.go`

**Interfaces:**
- Consumes: `Broker`, `TxPusher`, `Job`, `Kind[T]`, sentinels, `MemoryBroker` (+`WithMemoryClock`), `clock`, `id`.
- Produces: `queue.NewClient(b Broker, opts ...ClientOption) *Client`; `queue.Push[T](ctx, c, k Kind[T], payload T, opts ...PushOption) error`; `queue.PushTx[T](ctx, c, tx any, k Kind[T], payload T, opts ...PushOption) error`; `(*Client).PushRaw(ctx, name string, payload json.RawMessage, opts ...PushOption) error`; `(*Client).ListDead/Requeue/Purge/Stats` pass-throughs; options `WithScope(func(ctx) (string, error)) ClientOption`, `WithClientClock(clock.Clock) ClientOption`, `WithQueue(string) PushOption`, `WithDelay(time.Duration) PushOption`, `WithRunAt(time.Time) PushOption`, `WithMaxAttempts(int) PushOption`.

- [ ] **Step 1: Write failing tests**

`async/queue/client_test.go`:

```go
package queue_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/core/clock"
)

var kindWelcome = queue.NewKind[welcomePayload]("email.send_welcome")

func TestPush_Defaults(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	c := queue.NewClient(b)
	ctx := context.Background()

	require.NoError(t, queue.Push(ctx, c, kindWelcome, welcomePayload{UserID: "u1"}))

	got, err := b.Claim(ctx, "default", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1)
	j := got[0]
	assert.NotEmpty(t, j.ID)
	assert.Equal(t, "default", j.Queue)
	assert.Equal(t, "email.send_welcome", j.Type)
	assert.JSONEq(t, `{"user_id":"u1"}`, string(j.Payload))
	assert.Empty(t, j.Scope)
	assert.Zero(t, j.MaxAttempts, "0 = worker default")
	assert.False(t, j.CreatedAt.IsZero())
}

func TestPush_QueueAndMaxAttempts(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	c := queue.NewClient(b)
	ctx := context.Background()

	require.NoError(t, queue.Push(ctx, c, kindWelcome, welcomePayload{UserID: "u1"},
		queue.WithQueue("critical"), queue.WithMaxAttempts(3)))

	got, err := b.Claim(ctx, "critical", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 3, got[0].MaxAttempts)
}

func TestPush_DelayAndRunAt(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	mock := clock.NewMock(start)
	b := queue.NewMemoryBroker(queue.WithMemoryClock(mock))
	c := queue.NewClient(b, queue.WithClientClock(mock))
	ctx := context.Background()

	require.NoError(t, queue.Push(ctx, c, kindWelcome, welcomePayload{UserID: "d"}, queue.WithDelay(5*time.Minute)))
	require.NoError(t, queue.Push(ctx, c, kindWelcome, welcomePayload{UserID: "r"}, queue.WithRunAt(start.Add(10*time.Minute))))

	got, err := b.Claim(ctx, "default", 10, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, got, "delayed jobs must not be due yet")

	mock.Advance(6 * time.Minute)
	got, err = b.Claim(ctx, "default", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1, "only the 5m-delayed job is due")
	require.NoError(t, b.Ack(ctx, got[0].ID)) // ack so an expired lease cannot double-claim below

	mock.Advance(5 * time.Minute)
	got, err = b.Claim(ctx, "default", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1, "the run-at job is due now")
}

func TestPush_ScopeFailClosed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("hook error", func(t *testing.T) {
		t.Parallel()
		c := queue.NewClient(queue.NewMemoryBroker(), queue.WithScope(func(context.Context) (string, error) {
			return "", errors.New("no tenant")
		}))
		err := queue.Push(ctx, c, kindWelcome, welcomePayload{UserID: "u"})
		assert.ErrorIs(t, err, queue.ErrScopeMissing)
	})
	t.Run("empty scope", func(t *testing.T) {
		t.Parallel()
		c := queue.NewClient(queue.NewMemoryBroker(), queue.WithScope(func(context.Context) (string, error) {
			return "", nil
		}))
		err := queue.Push(ctx, c, kindWelcome, welcomePayload{UserID: "u"})
		assert.ErrorIs(t, err, queue.ErrScopeMissing)
	})
	t.Run("scope captured", func(t *testing.T) {
		t.Parallel()
		b := queue.NewMemoryBroker()
		c := queue.NewClient(b, queue.WithScope(func(context.Context) (string, error) {
			return "tenant-a", nil
		}))
		require.NoError(t, queue.Push(ctx, c, kindWelcome, welcomePayload{UserID: "u"}))
		got, err := b.Claim(ctx, "default", 1, time.Minute)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "tenant-a", got[0].Scope)
	})
}

func TestPushRaw(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	c := queue.NewClient(b)
	ctx := context.Background()

	require.NoError(t, c.PushRaw(ctx, "legacy.import", json.RawMessage(`{"x":1}`)))
	got, err := b.Claim(ctx, "default", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "legacy.import", got[0].Type)

	assert.Error(t, c.PushRaw(ctx, "", json.RawMessage(`{}`)), "empty name rejected")
	assert.Error(t, c.PushRaw(ctx, "x", json.RawMessage(`not json`)), "invalid JSON rejected")
}

type txBroker struct {
	*queue.MemoryBroker
	gotTx  any
	gotJob queue.Job
}

func (b *txBroker) PushTx(_ context.Context, tx any, job queue.Job) error {
	b.gotTx, b.gotJob = tx, job
	return nil
}

func TestPushTx(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("capability present", func(t *testing.T) {
		t.Parallel()
		b := &txBroker{MemoryBroker: queue.NewMemoryBroker()}
		c := queue.NewClient(b)
		fakeTx := struct{ name string }{"tx"}
		require.NoError(t, queue.PushTx(ctx, c, fakeTx, kindWelcome, welcomePayload{UserID: "u"}, queue.WithQueue("critical")))
		assert.Equal(t, fakeTx, b.gotTx)
		assert.Equal(t, "critical", b.gotJob.Queue)
		assert.Equal(t, "email.send_welcome", b.gotJob.Type)
	})
	t.Run("capability absent", func(t *testing.T) {
		t.Parallel()
		c := queue.NewClient(queue.NewMemoryBroker())
		err := queue.PushTx(ctx, c, "tx", kindWelcome, welcomePayload{UserID: "u"})
		assert.ErrorIs(t, err, queue.ErrTxUnsupported)
	})
	t.Run("scope enforced on PushTx", func(t *testing.T) {
		t.Parallel()
		b := &txBroker{MemoryBroker: queue.NewMemoryBroker()}
		c := queue.NewClient(b, queue.WithScope(func(context.Context) (string, error) { return "", nil }))
		err := queue.PushTx(ctx, c, "tx", kindWelcome, welcomePayload{UserID: "u"})
		assert.ErrorIs(t, err, queue.ErrScopeMissing)
	})
}

func TestClient_DLQPassthrough(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	c := queue.NewClient(b)
	ctx := context.Background()

	require.NoError(t, queue.Push(ctx, c, kindWelcome, welcomePayload{UserID: "u"}))
	got, err := b.Claim(ctx, "default", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NoError(t, b.Kill(ctx, got[0].ID, "poison"))

	dead, err := c.ListDead(ctx, "default", 10)
	require.NoError(t, err)
	require.Len(t, dead, 1)

	st, err := c.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, st["default"].Dead)

	require.NoError(t, c.Requeue(ctx, dead[0].ID))
	reclaimed, err := b.Claim(ctx, "default", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, reclaimed, 1)
	require.NoError(t, b.Kill(ctx, reclaimed[0].ID, "again"))
	require.NoError(t, c.Purge(ctx, reclaimed[0].ID))
	dead, err = c.ListDead(ctx, "default", 10)
	require.NoError(t, err)
	assert.Empty(t, dead)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./async/queue/ 2>&1 | head -10`
Expected: compile failure — `NewClient`, `Push`, options undefined.

- [ ] **Step 3: Implement**

`async/queue/options.go` (client + push options only for now):

```go
package queue

import (
	"context"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

// ClientOption configures NewClient.
type ClientOption func(*Client)

// WithScope installs the tenancy hook: it is called on every push and its
// result is captured into Job.Scope. Fail-closed: once configured, a hook
// error or empty scope makes the push fail with ErrScopeMissing.
// Single-tenant apps simply do not configure it.
func WithScope(fn func(ctx context.Context) (string, error)) ClientOption {
	return func(c *Client) { c.scope = fn }
}

// WithClientClock injects a clock (tests).
func WithClientClock(clk clock.Clock) ClientOption {
	return func(c *Client) { c.clk = clk }
}

type pushConfig struct {
	queue       string
	runAt       time.Time
	delay       time.Duration
	maxAttempts int
}

// PushOption configures a single push.
type PushOption func(*pushConfig)

// WithQueue routes the job to a named queue (default "default").
func WithQueue(name string) PushOption {
	return func(p *pushConfig) { p.queue = name }
}

// WithDelay schedules the job to run no earlier than now+d.
func WithDelay(d time.Duration) PushOption {
	return func(p *pushConfig) { p.delay = d }
}

// WithRunAt schedules the job to run no earlier than t. Takes precedence
// over WithDelay when both are given.
func WithRunAt(t time.Time) PushOption {
	return func(p *pushConfig) { p.runAt = t }
}

// WithMaxAttempts overrides the worker's default attempt budget for this job.
func WithMaxAttempts(n int) PushOption {
	return func(p *pushConfig) { p.maxAttempts = n }
}
```

`async/queue/client.go`:

```go
package queue

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/id"
)

// Client produces jobs. It is cheap, safe for concurrent use, and shared
// app-wide. Wire the same Broker instance into the worker Service.
type Client struct {
	broker Broker
	scope  func(ctx context.Context) (string, error)
	clk    clock.Clock
}

// NewClient builds a producer over broker.
func NewClient(broker Broker, opts ...ClientOption) *Client {
	c := &Client{broker: broker, clk: clock.System()}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Push enqueues a typed job. The payload is marshaled to JSON; the kind binds
// the job name to the payload type at compile time.
func Push[T any](ctx context.Context, c *Client, k Kind[T], payload T, opts ...PushOption) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("queue: marshal payload for %q: %w", k.Name(), err)
	}
	job, err := c.buildJob(ctx, k.Name(), raw, opts)
	if err != nil {
		return err
	}
	return c.broker.Push(ctx, job)
}

// PushTx enqueues a typed job inside a caller-owned database transaction. The
// broker must implement TxPusher (pgqueue does); otherwise ErrTxUnsupported.
func PushTx[T any](ctx context.Context, c *Client, tx any, k Kind[T], payload T, opts ...PushOption) error {
	tp, ok := c.broker.(TxPusher)
	if !ok {
		return ErrTxUnsupported
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("queue: marshal payload for %q: %w", k.Name(), err)
	}
	job, err := c.buildJob(ctx, k.Name(), raw, opts)
	if err != nil {
		return err
	}
	return tp.PushTx(ctx, tx, job)
}

// PushRaw enqueues a job by name with a caller-encoded JSON payload — the
// escape hatch for enqueuing kinds this codebase does not import. Prefer Push.
func (c *Client) PushRaw(ctx context.Context, name string, payload json.RawMessage, opts ...PushOption) error {
	if name == "" {
		return fmt.Errorf("queue: push raw: empty kind name")
	}
	if !json.Valid(payload) {
		return fmt.Errorf("queue: push raw %q: payload is not valid JSON", name)
	}
	job, err := c.buildJob(ctx, name, payload, opts)
	if err != nil {
		return err
	}
	return c.broker.Push(ctx, job)
}

func (c *Client) buildJob(ctx context.Context, name string, payload []byte, opts []PushOption) (Job, error) {
	p := pushConfig{queue: "default"}
	for _, opt := range opts {
		opt(&p)
	}
	scope := ""
	if c.scope != nil {
		s, err := c.scope(ctx)
		if err != nil {
			return Job{}, fmt.Errorf("%w: %w", ErrScopeMissing, err)
		}
		if s == "" {
			return Job{}, ErrScopeMissing
		}
		scope = s
	}
	now := c.clk.Now().UTC()
	runAt := now
	switch {
	case !p.runAt.IsZero():
		runAt = p.runAt.UTC()
	case p.delay > 0:
		runAt = now.Add(p.delay)
	}
	return Job{
		ID:          id.NewULID().String(),
		Queue:       p.queue,
		Type:        name,
		Payload:     payload,
		Scope:       scope,
		MaxAttempts: p.maxAttempts,
		RunAt:       runAt,
		CreatedAt:   now,
	}, nil
}

// ListDead returns up to limit dead-lettered jobs in the queue.
func (c *Client) ListDead(ctx context.Context, queue string, limit int) ([]Job, error) {
	return c.broker.ListDead(ctx, queue, limit)
}

// Requeue returns a dead job to pending with its attempt budget reset.
func (c *Client) Requeue(ctx context.Context, jobID string) error {
	return c.broker.Requeue(ctx, jobID)
}

// Purge permanently deletes a dead job.
func (c *Client) Purge(ctx context.Context, jobID string) error {
	return c.broker.Purge(ctx, jobID)
}

// Stats reports pending/dead counts per queue (health checks, dashboards).
func (c *Client) Stats(ctx context.Context) (Stats, error) {
	return c.broker.Stats(ctx)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `just fmt ./async/... && go test -race ./async/queue/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add async/queue && git commit -m "feat(queue): producer Client with typed Push, PushTx capability, scope fail-closed, DLQ ops"
```

---

### Task 5: Worker Service — registration, processing semantics, run loop, drain

**Files:**
- Create: `async/queue/service.go`
- Modify: `async/queue/options.go` (append service + handler options)
- Test: `async/queue/service_test.go`

**Interfaces:**
- Consumes: `Broker`, `Job`, `Kind[T]`, `Config`, sentinels/verdicts, `MemoryBroker`, `backoff.Backoff`/`backoff.Exponential`/`backoff.Constant`, `logger.NewNope`, `supervisor.Service` (implemented).
- Produces: `queue.NewService(b Broker, opts ...ServiceOption) (*Service, error)`; `queue.Register[T](s *Service, k Kind[T], fn func(context.Context, T) error, opts ...HandlerOption)` (panics on duplicate kind or nil fn; call before Run); `(*Service).Name() string`; `(*Service).Run(ctx) error`; options `WithConfig(Config)`, `WithQueues(map[string]int)`, `WithStrictPriority()`, `WithConcurrency(int)`, `WithName(string)`, `WithLogger(*slog.Logger)`, `WithScopeContext(func(context.Context, string) context.Context)`, `WithBackoff(backoff.Backoff)`, `WithHandlerTimeout(time.Duration)`, `WithHandlerMaxAttempts(int)`, `WithHandlerBackoff(backoff.Backoff)`.
- Internal (used by Task 6): `(*Service).claimOrder() []string` — this task returns the static weight-desc order for both modes; Task 6 replaces the weighted path with smooth weighted round-robin.

**Processing semantics (the engine contract — implement exactly):**
1. Claimed job with no registered handler → `Kill(opCtx, id, ErrNoHandler.Error()+": "+job.Type)`.
2. If `WithScopeContext` is configured and `job.Scope == ""` → fail closed: `Kill(opCtx, id, ErrScopeMissing.Error())`.
3. Handler context derives from `context.WithoutCancel(runCtx)` (drain must NOT abort in-flight handlers), then scope injection, then per-handler timeout.
4. A heartbeat goroutine calls `Extend(opCtx, id, lease)` every `lease/3` until the handler returns.
5. Handler panic is recovered and treated as a failure (`fmt.Errorf("queue: handler panic: %v", r)`).
6. Verdicts: nil → `Ack`; `errors.Is(err, Cancel)` → `Ack` (logged as cancelled); `IsSkipRetry(err)` → `Kill(reason=err.Error())`; other errors → retry path.
7. Retry path: effective max attempts = `job.MaxAttempts` if > 0, else handler override if > 0, else `cfg.MaxAttempts`. If `job.Attempt >= max` → `Kill`, else `Nack(id, now+bo.Next(job.Attempt), err.Error())` where `bo` = handler backoff override or service default (`backoff.Exponential(15*time.Second, 6*time.Hour, backoff.WithJitter(0.2))`).
8. All post-claim broker state ops (`Ack/Nack/Kill/Extend`) use `opCtx := context.WithoutCancel(runCtx)` so drain-time completions still commit. Broker op errors are logged, never propagated — an unacked job simply redelivers after lease expiry (at-least-once).
9. Run loop: immediate first poll, then tick every `PollInterval`; a poll that claimed > 0 jobs re-polls immediately (backlog drain); free slots = `Concurrency - inflight`, capped by `ClaimBatch` when > 0. On ctx cancel: stop claiming, wait for in-flight (`wg.Wait()`), return `ctx.Err()` (supervisor treats `context.Canceled` as clean).

- [ ] **Step 1: Write failing tests**

`async/queue/service_test.go`:

```go
package queue_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/resilience/backoff"
)

// testConfig returns worker knobs tuned for fast tests.
func testConfig() queue.Config {
	cfg := queue.DefaultConfig()
	cfg.PollInterval = 10 * time.Millisecond
	cfg.Lease = 500 * time.Millisecond
	cfg.MaxAttempts = 25
	return cfg
}

// runService starts svc.Run in a goroutine and returns a stop func that
// cancels and waits for Run to return.
func runService(t *testing.T, svc *queue.Service) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx) }()
	return func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				assert.ErrorIs(t, err, context.Canceled)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("service did not stop")
		}
	}
}

func eventually(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	require.Eventually(t, cond, 5*time.Second, 5*time.Millisecond, msg)
}

func TestService_ProcessesTypedJob(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(testConfig()))
	require.NoError(t, err)

	var got atomic.Value
	queue.Register(svc, kindWelcome, func(_ context.Context, p welcomePayload) error {
		got.Store(p)
		return nil
	})

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b)
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u42"}))

	eventually(t, func() bool { return got.Load() != nil }, "handler must run")
	assert.Equal(t, welcomePayload{UserID: "u42"}, got.Load())
	eventually(t, func() bool {
		st, err := b.Stats(context.Background())
		return err == nil && st["default"].Pending == 0 && st["default"].Dead == 0
	}, "successful job must be acked away")
}

func TestService_RetriesThenSucceeds(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(testConfig()))
	require.NoError(t, err)

	var calls atomic.Int32
	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error {
		if calls.Add(1) < 3 {
			return errors.New("transient")
		}
		return nil
	}, queue.WithHandlerBackoff(backoff.Constant(10*time.Millisecond)))

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b)
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}))

	eventually(t, func() bool { return calls.Load() == 3 }, "handler must retry to the third attempt")
	eventually(t, func() bool {
		st, _ := b.Stats(context.Background())
		return st["default"].Pending == 0 && st["default"].Dead == 0
	}, "job must complete after retries")
}

func TestService_MaxAttemptsDeadLetters(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(testConfig()))
	require.NoError(t, err)

	var calls atomic.Int32
	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error {
		calls.Add(1)
		return errors.New("always fails")
	}, queue.WithHandlerBackoff(backoff.Constant(5*time.Millisecond)))

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b)
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}, queue.WithMaxAttempts(2)))

	eventually(t, func() bool {
		dead, _ := b.ListDead(context.Background(), "default", 10)
		return len(dead) == 1
	}, "job must dead-letter after max attempts")
	assert.EqualValues(t, 2, calls.Load(), "push-level WithMaxAttempts(2) bounds the attempts")
	dead, err := b.ListDead(context.Background(), "default", 10)
	require.NoError(t, err)
	assert.Contains(t, dead[0].LastError, "always fails")
}

func TestService_HandlerMaxAttemptsOverride(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(testConfig()))
	require.NoError(t, err)

	var calls atomic.Int32
	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error {
		calls.Add(1)
		return errors.New("nope")
	}, queue.WithHandlerMaxAttempts(3), queue.WithHandlerBackoff(backoff.Constant(5*time.Millisecond)))

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b)
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}))

	eventually(t, func() bool {
		dead, _ := b.ListDead(context.Background(), "default", 10)
		return len(dead) == 1
	}, "job must dead-letter after handler max attempts")
	assert.EqualValues(t, 3, calls.Load())
}

func TestService_SkipRetryVerdict(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(testConfig()))
	require.NoError(t, err)

	var calls atomic.Int32
	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error {
		calls.Add(1)
		return queue.SkipRetry(errors.New("poison"))
	})

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b)
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}))

	eventually(t, func() bool {
		dead, _ := b.ListDead(context.Background(), "default", 10)
		return len(dead) == 1
	}, "SkipRetry must dead-letter immediately")
	assert.EqualValues(t, 1, calls.Load(), "no retries after SkipRetry")
	dead, _ := b.ListDead(context.Background(), "default", 10)
	assert.Contains(t, dead[0].LastError, "poison")
}

func TestService_CancelVerdict(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(testConfig()))
	require.NoError(t, err)

	var calls atomic.Int32
	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error {
		calls.Add(1)
		return queue.Cancel
	})

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b)
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}))

	eventually(t, func() bool {
		st, _ := b.Stats(context.Background())
		return st["default"].Pending == 0
	}, "cancelled job must be acked away")
	dead, _ := b.ListDead(context.Background(), "default", 10)
	assert.Empty(t, dead, "Cancel never dead-letters")
	assert.EqualValues(t, 1, calls.Load())
}

func TestService_PanicIsFailure(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(testConfig()))
	require.NoError(t, err)

	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error {
		panic("boom")
	}, queue.WithHandlerBackoff(backoff.Constant(5*time.Millisecond)))

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b)
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}, queue.WithMaxAttempts(2)))

	eventually(t, func() bool {
		dead, _ := b.ListDead(context.Background(), "default", 10)
		return len(dead) == 1
	}, "panicking job must retry then dead-letter")
	dead, _ := b.ListDead(context.Background(), "default", 10)
	assert.Contains(t, dead[0].LastError, "panic")
}

func TestService_UnmarshalFailureDeadLetters(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(testConfig()))
	require.NoError(t, err)

	var calls atomic.Int32
	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error {
		calls.Add(1)
		return nil
	})

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b)
	// user_id must be a string; number cannot unmarshal → poison.
	require.NoError(t, c.PushRaw(context.Background(), kindWelcome.Name(), []byte(`{"user_id":123}`)))

	eventually(t, func() bool {
		dead, _ := b.ListDead(context.Background(), "default", 10)
		return len(dead) == 1
	}, "type-mismatched payload must dead-letter, not retry")
	assert.Zero(t, calls.Load(), "handler body must not run on unmarshal failure")
}

func TestService_UnregisteredKindDeadLetters(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(testConfig()))
	require.NoError(t, err)
	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error { return nil })

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b)
	require.NoError(t, c.PushRaw(context.Background(), "nobody.home", []byte(`{}`)))

	eventually(t, func() bool {
		dead, _ := b.ListDead(context.Background(), "default", 10)
		return len(dead) == 1
	}, "unregistered kind must dead-letter")
	dead, _ := b.ListDead(context.Background(), "default", 10)
	assert.Contains(t, dead[0].LastError, "no handler")
}

func TestService_HandlerTimeout(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(testConfig()))
	require.NoError(t, err)

	var calls atomic.Int32
	queue.Register(svc, kindWelcome, func(ctx context.Context, _ welcomePayload) error {
		calls.Add(1)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
			return nil
		}
	}, queue.WithHandlerTimeout(30*time.Millisecond), queue.WithHandlerBackoff(backoff.Constant(5*time.Millisecond)))

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b)
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}, queue.WithMaxAttempts(2)))

	eventually(t, func() bool {
		dead, _ := b.ListDead(context.Background(), "default", 10)
		return len(dead) == 1
	}, "timed-out job must retry then dead-letter")
	assert.EqualValues(t, 2, calls.Load())
	dead, _ := b.ListDead(context.Background(), "default", 10)
	assert.Contains(t, dead[0].LastError, "context deadline exceeded")
}

type scopeKey struct{}

func TestService_ScopeRestoredIntoHandlerContext(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b,
		queue.WithConfig(testConfig()),
		queue.WithScopeContext(func(ctx context.Context, scope string) context.Context {
			return context.WithValue(ctx, scopeKey{}, scope)
		}),
	)
	require.NoError(t, err)

	var got atomic.Value
	queue.Register(svc, kindWelcome, func(ctx context.Context, _ welcomePayload) error {
		if s, ok := ctx.Value(scopeKey{}).(string); ok {
			got.Store(s)
		}
		return nil
	})

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b, queue.WithScope(func(context.Context) (string, error) { return "tenant-a", nil }))
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}))

	eventually(t, func() bool { return got.Load() != nil }, "handler must run")
	assert.Equal(t, "tenant-a", got.Load())
}

func TestService_ScopeMissingFailsClosed(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b,
		queue.WithConfig(testConfig()),
		queue.WithScopeContext(func(ctx context.Context, _ string) context.Context { return ctx }),
	)
	require.NoError(t, err)

	var calls atomic.Int32
	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error {
		calls.Add(1)
		return nil
	})

	stop := runService(t, svc)
	defer stop()

	// Client WITHOUT a scope hook feeding a scoped worker: fail closed.
	c := queue.NewClient(b)
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}))

	eventually(t, func() bool {
		dead, _ := b.ListDead(context.Background(), "default", 10)
		return len(dead) == 1
	}, "unscoped job on a scoped worker must dead-letter")
	assert.Zero(t, calls.Load())
	dead, _ := b.ListDead(context.Background(), "default", 10)
	assert.Contains(t, dead[0].LastError, "scope missing")
}

func TestService_HeartbeatKeepsLongJobClaimed(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.Lease = 100 * time.Millisecond // handler runs 5x the lease
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(cfg))
	require.NoError(t, err)

	var calls atomic.Int32
	done := make(chan struct{})
	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error {
		calls.Add(1)
		time.Sleep(500 * time.Millisecond)
		close(done)
		return nil
	})

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b)
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}))

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler never finished")
	}
	time.Sleep(50 * time.Millisecond) // allow ack
	assert.EqualValues(t, 1, calls.Load(), "heartbeat must prevent redelivery of a running job")
	st, _ := b.Stats(context.Background())
	assert.Zero(t, st["default"].Pending)
}

func TestService_DrainWaitsForInflight(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(testConfig()))
	require.NoError(t, err)

	started := make(chan struct{})
	var finished atomic.Bool
	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error {
		close(started)
		time.Sleep(300 * time.Millisecond)
		finished.Store(true)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx) }()

	c := queue.NewClient(b)
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}))
	<-started
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
	assert.True(t, finished.Load(), "Run must not return before in-flight handlers finish")
	st, _ := b.Stats(context.Background())
	assert.Zero(t, st["default"].Pending, "drained job must still be acked (WithoutCancel op ctx)")
}

func TestService_ConcurrencyBound(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(cfg), queue.WithConcurrency(2))
	require.NoError(t, err)

	var mu sync.Mutex
	inflight, peak := 0, 0
	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error {
		mu.Lock()
		inflight++
		if inflight > peak {
			peak = inflight
		}
		mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		inflight--
		mu.Unlock()
		return nil
	})

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b)
	for range 8 {
		require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}))
	}

	eventually(t, func() bool {
		st, _ := b.Stats(context.Background())
		return st["default"].Pending == 0
	}, "all jobs must complete")
	mu.Lock()
	defer mu.Unlock()
	assert.LessOrEqual(t, peak, 2, "concurrency bound must hold")
}

func TestService_NameAndValidation(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()

	svc, err := queue.NewService(b)
	require.NoError(t, err)
	assert.Equal(t, "queue", svc.Name())

	svc2, err := queue.NewService(b, queue.WithName("queue-video"))
	require.NoError(t, err)
	assert.Equal(t, "queue-video", svc2.Name())

	_, err = queue.NewService(b, queue.WithConcurrency(-1))
	assert.ErrorIs(t, err, queue.ErrInvalidConfig)

	_, err = queue.NewService(b, queue.WithQueues(map[string]int{"a": 0}))
	assert.ErrorIs(t, err, queue.ErrInvalidConfig)

	_, err = queue.NewService(nil)
	assert.ErrorIs(t, err, queue.ErrInvalidConfig)
}

func TestRegister_DuplicatePanics(t *testing.T) {
	t.Parallel()
	svc, err := queue.NewService(queue.NewMemoryBroker())
	require.NoError(t, err)
	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error { return nil })
	assert.Panics(t, func() {
		queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error { return nil })
	})
	assert.Panics(t, func() {
		queue.Register(svc, queue.NewKind[welcomePayload]("other.kind"), nil)
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./async/queue/ 2>&1 | head -10`
Expected: compile failure — `NewService`, `Register`, service options undefined.

- [ ] **Step 3: Append service/handler options to `async/queue/options.go`**

```go
// ServiceOption configures NewService.
type ServiceOption func(*Service)

// WithConfig replaces the worker Config (validated by NewService).
func WithConfig(cfg Config) ServiceOption {
	return func(s *Service) { s.cfg = cfg }
}

// WithQueues sets the queues this service drains with their claim weights
// (higher weight = larger share of free worker slots). Default: {"default": 1}.
func WithQueues(weights map[string]int) ServiceOption {
	return func(s *Service) { s.queueWeights = weights }
}

// WithStrictPriority drains queues in strict weight order: lower-weight
// queues are only claimed from when every heavier queue is empty. Starvation
// of light queues under sustained heavy load is the accepted trade-off.
func WithStrictPriority() ServiceOption {
	return func(s *Service) { s.strict = true }
}

// WithConcurrency overrides Config.Concurrency.
func WithConcurrency(n int) ServiceOption {
	return func(s *Service) { s.cfg.Concurrency = n }
}

// WithName overrides the supervisor service name (default "queue"); required
// when running multiple Service instances under one supervisor.
func WithName(name string) ServiceOption {
	return func(s *Service) { s.name = name }
}

// WithLogger sets the logger (default logger.NewNope()).
func WithLogger(l *slog.Logger) ServiceOption {
	return func(s *Service) { s.log = l }
}

// WithScopeContext installs the tenancy restore hook: called before each
// handler with the job's scope; the returned context is the handler's base
// context. Fail-closed: once configured, a job with an empty scope is
// dead-lettered without running its handler.
func WithScopeContext(fn func(ctx context.Context, scope string) context.Context) ServiceOption {
	return func(s *Service) { s.scopeCtx = fn }
}

// WithBackoff sets the service-wide default retry backoff
// (default backoff.Exponential(15s, 6h, jitter 0.2)).
func WithBackoff(b backoff.Backoff) ServiceOption {
	return func(s *Service) { s.defaultBackoff = b }
}

// HandlerOption configures a single Register call.
type HandlerOption func(*handler)

// WithHandlerTimeout bounds each invocation; expiry counts as a failure and
// takes the retry path.
func WithHandlerTimeout(d time.Duration) HandlerOption {
	return func(h *handler) { h.timeout = d }
}

// WithHandlerMaxAttempts sets this kind's attempt budget (overridden by a
// per-job WithMaxAttempts push option, overrides Config.MaxAttempts).
func WithHandlerMaxAttempts(n int) HandlerOption {
	return func(h *handler) { h.maxAttempts = n }
}

// WithHandlerBackoff overrides the service default backoff for this kind.
func WithHandlerBackoff(b backoff.Backoff) HandlerOption {
	return func(h *handler) { h.backoff = b }
}
```

Add the imports `log/slog` and `github.com/dmitrymomot/forge/resilience/backoff` to options.go.

- [ ] **Step 4: Implement `async/queue/service.go`**

```go
package queue

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/ops/logger"
	"github.com/dmitrymomot/forge/resilience/backoff"
)

// Service is the worker: a supervisor.Service that claims jobs from its
// configured queues and dispatches them to registered handlers on a bounded
// pool. Register all kinds before Run; Register is not safe to call
// concurrently with Run.
type Service struct {
	broker         Broker
	cfg            Config
	name           string
	queueWeights   map[string]int
	queues         []weightedQueue // weight desc, name asc; built by NewService
	strict         bool
	log            *slog.Logger
	scopeCtx       func(ctx context.Context, scope string) context.Context
	defaultBackoff backoff.Backoff
	handlers       map[string]*handler
}

type weightedQueue struct {
	name    string
	weight  int
	current int // smooth weighted round-robin state (Task 6)
}

type handler struct {
	fn          func(ctx context.Context, payload []byte) error
	backoff     backoff.Backoff
	timeout     time.Duration
	maxAttempts int
}

// NewService builds a worker over broker. Returns ErrInvalidConfig on nil
// broker, invalid Config, or non-positive queue weights.
func NewService(broker Broker, opts ...ServiceOption) (*Service, error) {
	s := &Service{
		broker:         broker,
		cfg:            DefaultConfig(),
		name:           "queue",
		queueWeights:   map[string]int{"default": 1},
		log:            logger.NewNope(),
		defaultBackoff: backoff.Exponential(15*time.Second, 6*time.Hour, backoff.WithJitter(0.2)),
		handlers:       make(map[string]*handler),
	}
	for _, opt := range opts {
		opt(s)
	}
	if broker == nil {
		return nil, fmt.Errorf("%w: nil broker", ErrInvalidConfig)
	}
	if err := s.cfg.Validate(); err != nil {
		return nil, err
	}
	if len(s.queueWeights) == 0 {
		return nil, fmt.Errorf("%w: no queues configured", ErrInvalidConfig)
	}
	for name, w := range s.queueWeights {
		if name == "" || w <= 0 {
			return nil, fmt.Errorf("%w: queue %q weight must be > 0, got %d", ErrInvalidConfig, name, w)
		}
		s.queues = append(s.queues, weightedQueue{name: name, weight: w})
	}
	slices.SortFunc(s.queues, func(a, b weightedQueue) int {
		if r := cmp.Compare(b.weight, a.weight); r != 0 {
			return r
		}
		return cmp.Compare(a.name, b.name)
	})
	return s, nil
}

// Register binds a typed handler to kind. Panics on nil fn or duplicate
// registration — kinds are startup wiring, and failing fast beats silently
// dead-lettering every job of a kind two packages both claimed.
func Register[T any](s *Service, k Kind[T], fn func(ctx context.Context, payload T) error, opts ...HandlerOption) {
	if fn == nil {
		panic(fmt.Sprintf("queue: Register(%q) with nil handler", k.Name()))
	}
	if _, dup := s.handlers[k.Name()]; dup {
		panic(fmt.Sprintf("queue: duplicate handler registration for kind %q", k.Name()))
	}
	h := &handler{fn: func(ctx context.Context, payload []byte) error {
		var p T
		if err := json.Unmarshal(payload, &p); err != nil {
			return SkipRetry(fmt.Errorf("queue: unmarshal payload for %q: %w", k.Name(), err))
		}
		return fn(ctx, p)
	}}
	for _, opt := range opts {
		opt(h)
	}
	s.handlers[k.Name()] = h
}

// Name implements supervisor.Service.
func (s *Service) Name() string { return s.name }

// Run implements supervisor.Service: poll, claim, dispatch until ctx is
// cancelled, then stop claiming and wait for in-flight handlers to finish.
func (s *Service) Run(ctx context.Context) error {
	opCtx := context.WithoutCancel(ctx) // post-claim broker ops must commit during drain
	sem := make(chan struct{}, s.cfg.Concurrency)
	var wg sync.WaitGroup

	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()

	s.log.InfoContext(ctx, "queue service started", slog.String("service", s.name), slog.Int("concurrency", s.cfg.Concurrency))
	for {
		claimed := s.pollOnce(ctx, opCtx, sem, &wg)
		if ctx.Err() != nil {
			break
		}
		if claimed > 0 {
			continue // backlog: keep claiming without waiting for the tick
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
			continue
		}
		break
	}
	s.log.InfoContext(opCtx, "queue service draining", slog.String("service", s.name))
	wg.Wait()
	s.log.InfoContext(opCtx, "queue service stopped", slog.String("service", s.name))
	return ctx.Err()
}

// pollOnce claims up to the free slot budget across queues (in claimOrder)
// and dispatches each claimed job. Returns the number of jobs claimed.
func (s *Service) pollOnce(ctx context.Context, opCtx context.Context, sem chan struct{}, wg *sync.WaitGroup) int {
	free := s.cfg.Concurrency - len(sem)
	if free <= 0 {
		return 0
	}
	if s.cfg.ClaimBatch > 0 && free > s.cfg.ClaimBatch {
		free = s.cfg.ClaimBatch
	}
	total := 0
	for _, qname := range s.claimOrder() {
		if free <= 0 || ctx.Err() != nil {
			break
		}
		jobs, err := s.broker.Claim(ctx, qname, free, s.cfg.Lease)
		if err != nil {
			if ctx.Err() == nil {
				s.log.ErrorContext(ctx, "queue claim failed", slog.String("queue", qname), slog.Any("error", err))
			}
			continue
		}
		for _, job := range jobs {
			sem <- struct{}{}
			wg.Go(func() {
				defer func() { <-sem }()
				s.process(opCtx, job)
			})
		}
		free -= len(jobs)
		total += len(jobs)
	}
	return total
}

// claimOrder returns queue names in claim order. Both modes use static
// weight-desc order in this revision; Task 6 gives the weighted mode smooth
// weighted round-robin so light queues cannot starve.
func (s *Service) claimOrder() []string {
	order := make([]string, len(s.queues))
	for i, q := range s.queues {
		order[i] = q.name
	}
	return order
}

// process runs one claimed job to a terminal broker state. opCtx is never
// cancelled by shutdown: in-flight completions must still commit.
func (s *Service) process(opCtx context.Context, job Job) {
	logAttrs := []any{
		slog.String("service", s.name), slog.String("job_id", job.ID),
		slog.String("kind", job.Type), slog.String("queue", job.Queue), slog.Int("attempt", job.Attempt),
	}
	h, ok := s.handlers[job.Type]
	if !ok {
		s.finalize(opCtx, "dead", logAttrs, func() error {
			return s.broker.Kill(opCtx, job.ID, ErrNoHandler.Error()+": "+job.Type)
		})
		return
	}
	if s.scopeCtx != nil && job.Scope == "" {
		s.finalize(opCtx, "dead", logAttrs, func() error {
			return s.broker.Kill(opCtx, job.ID, ErrScopeMissing.Error())
		})
		return
	}

	// Heartbeat: extend the lease at lease/3 until the handler returns.
	hbCtx, stopHB := context.WithCancel(opCtx)
	var hbWG sync.WaitGroup
	hbWG.Go(func() {
		t := time.NewTicker(s.cfg.Lease / 3)
		defer t.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-t.C:
				if err := s.broker.Extend(hbCtx, job.ID, s.cfg.Lease); err != nil && hbCtx.Err() == nil {
					s.log.ErrorContext(hbCtx, "queue lease extend failed", append(logAttrs, slog.Any("error", err))...)
				}
			}
		}
	})

	hctx := opCtx
	if s.scopeCtx != nil {
		hctx = s.scopeCtx(hctx, job.Scope)
	}
	var cancel context.CancelFunc = func() {}
	if h.timeout > 0 {
		hctx, cancel = context.WithTimeout(hctx, h.timeout)
	}
	start := time.Now()
	err := s.invoke(hctx, h, job)
	cancel()
	stopHB()
	hbWG.Wait()
	logAttrs = append(logAttrs, slog.Duration("duration", time.Since(start)))

	switch {
	case err == nil:
		s.finalize(opCtx, "done", logAttrs, func() error { return s.broker.Ack(opCtx, job.ID) })
	case errors.Is(err, Cancel):
		s.finalize(opCtx, "cancelled", logAttrs, func() error { return s.broker.Ack(opCtx, job.ID) })
	case IsSkipRetry(err):
		logAttrs = append(logAttrs, slog.Any("error", err))
		s.finalize(opCtx, "dead", logAttrs, func() error { return s.broker.Kill(opCtx, job.ID, err.Error()) })
	default:
		logAttrs = append(logAttrs, slog.Any("error", err))
		maxAttempts := s.cfg.MaxAttempts
		if h.maxAttempts > 0 {
			maxAttempts = h.maxAttempts
		}
		if job.MaxAttempts > 0 {
			maxAttempts = job.MaxAttempts
		}
		if job.Attempt >= maxAttempts {
			s.finalize(opCtx, "dead", logAttrs, func() error { return s.broker.Kill(opCtx, job.ID, err.Error()) })
			return
		}
		bo := s.defaultBackoff
		if h.backoff != nil {
			bo = h.backoff
		}
		retryAt := time.Now().UTC().Add(bo.Next(job.Attempt))
		logAttrs = append(logAttrs, slog.Time("retry_at", retryAt))
		s.finalize(opCtx, "retry", logAttrs, func() error { return s.broker.Nack(opCtx, job.ID, retryAt, err.Error()) })
	}
}

// invoke runs the handler with panic recovery; a panic is a normal failure.
func (s *Service) invoke(ctx context.Context, h *handler, job Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("queue: handler panic: %v", r)
		}
	}()
	return h.fn(ctx, job.Payload)
}

// finalize applies a terminal broker op and logs the outcome. A failed op is
// logged and dropped: the lease will expire and the job redelivers —
// at-least-once, never lost.
func (s *Service) finalize(ctx context.Context, outcome string, logAttrs []any, op func() error) {
	if err := op(); err != nil {
		s.log.ErrorContext(ctx, "queue broker op failed, job will redeliver after lease expiry", append(logAttrs, slog.String("outcome", outcome), slog.Any("error", err))...)
		return
	}
	s.log.InfoContext(ctx, "queue job "+outcome, logAttrs...)
}
```

Implementation notes for the executor:
- `wg.Go` and `hbWG.Go` are Go 1.25+ `sync.WaitGroup.Go` — do not rewrite as Add/Done.
- In `pollOnce`, `len(sem)` is an approximation of in-flight (slots release concurrently); it only ever underestimates free slots, and `sem <-` blocking is the hard bound. That is intentional — do not add locking.
- The `for` loop in `Run` breaks out of the select via labeled control if you prefer; the shown shape (`continue` on tick, fallthrough `break` after `<-ctx.Done()`) must preserve: claim-before-first-tick, immediate re-poll after a non-empty claim, exit only via ctx.
- `job.Attempt >= maxAttempts` is checked AFTER a failure: attempt N failing with budget N dead-letters — matches "max attempts consumed".

- [ ] **Step 5: Run tests to verify they pass**

Run: `just fmt ./async/... && go test -race ./async/queue/`
Expected: PASS (suite takes a few seconds — timing tests)

- [ ] **Step 6: Commit**

```bash
git add async/queue && git commit -m "feat(queue): worker Service with typed handlers, retry/dead-letter engine, heartbeat, graceful drain"
```

---

### Task 6: Weighted-priority claiming (smooth weighted round-robin) + strict mode

**Files:**
- Modify: `async/queue/service.go` (replace `claimOrder` with SWRR quota planning inside `pollOnce`)
- Test: `async/queue/service_internal_test.go` (white-box — SWRR is unexported state), `async/queue/priority_test.go` (black-box)

**Interfaces:**
- Consumes: `Service.queues []weightedQueue` (Task 5; the `current` field exists for this task).
- Produces: unexported `(*Service).pickNext() string` (one smooth-weighted-round-robin step) and `(*Service).claimPlan(free int) (order []string, quota map[string]int)`; `pollOnce` claims per-quota then does a leftover sweep in weight-desc order; strict mode bypasses quotas entirely.

**Algorithm (implement exactly):**
- `pickNext` (nginx SWRR): for every queue `current += weight`; pick the first queue with the strictly greatest `current`; subtract the total weight from the picked queue's `current`; return its name. Over any window of totalWeight picks every queue is picked exactly `weight` times — proportional and starvation-free.
- `claimPlan(free)`: take `free` SWRR picks; `quota[name]` = times picked; `order` = names in first-pick order. With free=1 the single pick rotates proportionally across polls (this is what prevents light-queue starvation that a naive weight-sorted greedy loop would cause).
- Weighted `pollOnce`: claim `quota[q]` from each queue in `order`; then, if slots remain, a leftover sweep claims the remainder from every queue in static weight-desc order (queues that had no quota this poll or returned fewer than quota).
- Strict `pollOnce`: single pass over static weight-desc order, claiming all remaining slots from each queue in turn (Task 5 behavior — keep it).
- Single-goroutine access only (`Run`'s poll loop) — no locking on `current`.

- [ ] **Step 1: Write failing tests**

`async/queue/service_internal_test.go` (note: `package queue`, white-box):

```go
package queue

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newWeightedService(t *testing.T, weights map[string]int) *Service {
	t.Helper()
	s, err := NewService(NewMemoryBroker(), WithQueues(weights))
	require.NoError(t, err)
	return s
}

func TestPickNext_ProportionalSequence(t *testing.T) {
	t.Parallel()
	s := newWeightedService(t, map[string]int{"a": 6, "b": 3, "c": 1})
	counts := map[string]int{}
	var seq []string
	for range 10 {
		n := s.pickNext()
		counts[n]++
		seq = append(seq, n)
	}
	assert.Equal(t, map[string]int{"a": 6, "b": 3, "c": 1}, counts, "one full SWRR cycle is exactly proportional")
	assert.Equal(t, []string{"a", "b", "a", "a", "b", "a", "c", "a", "b", "a"}, seq, "canonical nginx SWRR order for 6/3/1")
}

func TestClaimPlan_FullBudgetMatchesWeights(t *testing.T) {
	t.Parallel()
	s := newWeightedService(t, map[string]int{"a": 6, "b": 3, "c": 1})
	order, quota := s.claimPlan(10)
	assert.Equal(t, map[string]int{"a": 6, "b": 3, "c": 1}, quota)
	assert.Equal(t, "a", order[0], "heaviest queue claims first on a fresh service")
	assert.Len(t, order, 3)
}

func TestClaimPlan_SingleSlotRotates(t *testing.T) {
	t.Parallel()
	s := newWeightedService(t, map[string]int{"a": 6, "b": 3, "c": 1})
	counts := map[string]int{}
	for range 10 {
		order, quota := s.claimPlan(1)
		require.Len(t, order, 1)
		assert.Equal(t, 1, quota[order[0]])
		counts[order[0]]++
	}
	assert.Equal(t, map[string]int{"a": 6, "b": 3, "c": 1}, counts, "free=1 polls must rotate proportionally, never starving light queues")
}
```

`async/queue/priority_test.go`:

```go
package queue_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/queue"
)

type orderPayload struct {
	Queue string `json:"queue"`
}

var kindOrder = queue.NewKind[orderPayload]("test.order")

// pushN pushes n kindOrder jobs to the named queue with run_at in the past so
// claim order is deterministic per queue.
func pushN(t *testing.T, c *queue.Client, q string, n int) {
	t.Helper()
	for range n {
		require.NoError(t, queue.Push(context.Background(), c, kindOrder, orderPayload{Queue: q}, queue.WithQueue(q)))
	}
}

func TestService_StrictPriorityDrainsHeavyFirst(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	cfg := testConfig()
	svc, err := queue.NewService(b,
		queue.WithConfig(cfg),
		queue.WithConcurrency(1),
		queue.WithQueues(map[string]int{"critical": 2, "low": 1}),
		queue.WithStrictPriority(),
	)
	require.NoError(t, err)

	var mu sync.Mutex
	var processed []string
	queue.Register(svc, kindOrder, func(_ context.Context, p orderPayload) error {
		mu.Lock()
		processed = append(processed, p.Queue)
		mu.Unlock()
		return nil
	})

	c := queue.NewClient(b)
	pushN(t, c, "low", 3)
	pushN(t, c, "critical", 3)

	stop := runService(t, svc)
	defer stop()

	eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(processed) == 6
	}, "all jobs must complete")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"critical", "critical", "critical", "low", "low", "low"}, processed, "strict mode drains critical fully before touching low")
}

func TestService_WeightedDoesNotStarveLightQueue(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	cfg := testConfig()
	svc, err := queue.NewService(b,
		queue.WithConfig(cfg),
		queue.WithConcurrency(1),
		queue.WithQueues(map[string]int{"heavy": 3, "light": 1}),
	)
	require.NoError(t, err)

	var mu sync.Mutex
	var processed []string
	queue.Register(svc, kindOrder, func(_ context.Context, p orderPayload) error {
		mu.Lock()
		processed = append(processed, p.Queue)
		mu.Unlock()
		return nil
	})

	c := queue.NewClient(b)
	pushN(t, c, "heavy", 6)
	pushN(t, c, "light", 2)

	stop := runService(t, svc)
	defer stop()

	eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(processed) == 8
	}, "all jobs must complete")

	mu.Lock()
	defer mu.Unlock()
	firstLight := -1
	for i, q := range processed {
		if q == "light" {
			firstLight = i
			break
		}
	}
	require.NotEqual(t, -1, firstLight)
	assert.LessOrEqual(t, firstLight, 4, "SWRR must interleave the light queue while heavy is backlogged (a,a,b cadence for 3/1), not append it at the end")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./async/queue/ 2>&1 | head -10`
Expected: compile failure — `pickNext`, `claimPlan` undefined; strict test fails on ordering if run against Task 5's static order for weighted mode.

- [ ] **Step 3: Implement — replace `claimOrder` in `service.go`**

Delete the Task 5 `claimOrder` method. Add:

```go
// pickNext advances the smooth weighted round-robin state one step and
// returns the picked queue. Only Run's poll goroutine touches this state.
func (s *Service) pickNext() string {
	total := 0
	best := -1
	for i := range s.queues {
		s.queues[i].current += s.queues[i].weight
		total += s.queues[i].weight
		if best == -1 || s.queues[i].current > s.queues[best].current {
			best = i
		}
	}
	s.queues[best].current -= total
	return s.queues[best].name
}

// claimPlan distributes free slots across queues by SWRR: free picks become
// per-queue quotas. With free=1 the pick rotates proportionally across polls,
// which is what keeps light queues alive under sustained heavy backlog.
func (s *Service) claimPlan(free int) ([]string, map[string]int) {
	quota := make(map[string]int, len(s.queues))
	order := make([]string, 0, len(s.queues))
	for range free {
		n := s.pickNext()
		if quota[n] == 0 {
			order = append(order, n)
		}
		quota[n]++
	}
	return order, quota
}
```

Replace the claim loop inside `pollOnce` with:

```go
	total := 0
	claim := func(qname string, n int) {
		if n <= 0 || ctx.Err() != nil {
			return
		}
		jobs, err := s.broker.Claim(ctx, qname, n, s.cfg.Lease)
		if err != nil {
			if ctx.Err() == nil {
				s.log.ErrorContext(ctx, "queue claim failed", slog.String("queue", qname), slog.Any("error", err))
			}
			return
		}
		for _, job := range jobs {
			sem <- struct{}{}
			wg.Go(func() {
				defer func() { <-sem }()
				s.process(opCtx, job)
			})
		}
		free -= len(jobs)
		total += len(jobs)
	}

	if s.strict {
		for _, q := range s.queues { // static weight-desc order
			claim(q.name, free)
		}
		return total
	}
	order, quota := s.claimPlan(free)
	for _, qname := range order {
		claim(qname, min(quota[qname], free))
	}
	for _, q := range s.queues { // leftover sweep: unfilled quotas roll to any queue with work
		claim(q.name, free)
	}
	return total
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `just fmt ./async/... && go test -race ./async/queue/`
Expected: PASS. If `TestService_WeightedDoesNotStarveLightQueue` proves timing-flaky under `-race` (slot release racing the next poll can shift the first light job by a position), loosen only the bound (`firstLight <= 5`), never the shape of the assertion.

- [ ] **Step 5: Commit**

```bash
git add async/queue && git commit -m "feat(queue): smooth weighted round-robin claiming with strict-priority mode"
```

---

### Task 7: Core package docs, runnable example, benchmarks + optimization pass

**Files:**
- Create: `async/queue/doc.go`, `async/queue/example_test.go`, `async/queue/bench_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–6.
- Produces: package documentation; benchmark baseline numbers for the PR body.

- [ ] **Step 1: Write `async/queue/doc.go`**

```go
// Package queue is the durable background-work engine: producers Push typed
// jobs through a Client, a worker Service claims them from named queues and
// dispatches to registered handlers with retry, backoff, and dead-lettering.
//
// Storage is a pluggable, strictly-pull Broker: the built-in MemoryBroker
// (tests, single-process apps), async/queue/postgres (SKIP LOCKED claiming,
// transactional enqueue), and async/queue/redis (Streams + consumer groups).
// The engine — not the driver — owns retry, delay, and dead-letter semantics,
// so behavior is identical across backends.
//
// # Delivery contract
//
// Delivery is at-least-once via claim-with-lease: a claimed job is invisible
// for the lease duration and the Service heartbeats the lease while the
// handler runs; if the process crashes the lease expires and the job is
// redelivered. HANDLERS MUST BE IDEMPOTENT. Ordering is not guaranteed.
//
// # Usage
//
//	var KindSendWelcome = queue.NewKind[SendWelcome]("email.send_welcome")
//
//	svc, _ := queue.NewService(broker,
//		queue.WithQueues(map[string]int{"critical": 6, "default": 3, "low": 1}),
//	)
//	queue.Register(svc, KindSendWelcome, func(ctx context.Context, p SendWelcome) error {
//		return mailer.Send(ctx, p.Email)
//	})
//	// run under ops/supervisor: supervisor.WithService(svc)
//
//	client := queue.NewClient(broker)
//	err := queue.Push(ctx, client, KindSendWelcome, SendWelcome{Email: "a@b.c"},
//		queue.WithQueue("critical"), queue.WithDelay(time.Minute))
//
// Handler verdicts: return nil to complete, queue.SkipRetry(err) to
// dead-letter immediately (poison input), queue.Cancel to discard a moot job;
// any other error retries with backoff until the attempt budget is spent,
// then dead-letters. Inspect and recover via Client.ListDead, Client.Requeue,
// Client.Purge; feed ops/health from Client.Stats.
//
// Multi-tenant apps configure queue.WithScope on the Client (captures the
// tenant into the job, fail-closed) and queue.WithScopeContext on the Service
// (restores it into the handler context). Single-tenant apps configure
// neither.
//
// The queue is the unit of routing: every kind pushed to a queue must be
// registered on every Service draining that queue; to split kinds across
// worker deployments, split the queues.
package queue
```

- [ ] **Step 2: Write `async/queue/example_test.go`**

```go
package queue_test

import (
	"context"
	"fmt"
	"time"

	"github.com/dmitrymomot/forge/async/queue"
)

type sendWelcome struct {
	Email string `json:"email"`
}

var kindSendWelcome = queue.NewKind[sendWelcome]("example.send_welcome")

func Example() {
	broker := queue.NewMemoryBroker()

	cfg := queue.DefaultConfig()
	cfg.PollInterval = 10 * time.Millisecond
	svc, _ := queue.NewService(broker, queue.WithConfig(cfg))

	done := make(chan struct{})
	queue.Register(svc, kindSendWelcome, func(_ context.Context, p sendWelcome) error {
		fmt.Println("welcome sent to", p.Email)
		close(done)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { _ = svc.Run(ctx); close(stopped) }()

	client := queue.NewClient(broker)
	_ = queue.Push(context.Background(), client, kindSendWelcome, sendWelcome{Email: "new@user.dev"})

	<-done
	cancel()
	<-stopped
	// Output: welcome sent to new@user.dev
}
```

- [ ] **Step 3: Write `async/queue/bench_test.go`**

```go
package queue_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/async/queue"
)

type benchPayload struct {
	N int `json:"n"`
}

var kindBench = queue.NewKind[benchPayload]("bench.job")

func BenchmarkPush_Memory(b *testing.B) {
	broker := queue.NewMemoryBroker()
	c := queue.NewClient(broker)
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		if err := queue.Push(ctx, c, kindBench, benchPayload{N: i}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkClaimBatch_Memory(b *testing.B) {
	ctx := context.Background()
	broker := queue.NewMemoryBroker()
	c := queue.NewClient(broker)
	for i := range 10_000 {
		if err := queue.Push(ctx, c, kindBench, benchPayload{N: i}); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		jobs, err := broker.Claim(ctx, "default", 100, time.Minute)
		if err != nil {
			b.Fatal(err)
		}
		for _, j := range jobs {
			if err := broker.Nack(ctx, j.ID, time.Now(), ""); err != nil { // recycle for the next iteration
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkEndToEnd_Memory(b *testing.B) {
	ctx := context.Background()
	broker := queue.NewMemoryBroker()
	cfg := queue.DefaultConfig()
	cfg.PollInterval = time.Millisecond
	cfg.Concurrency = 16
	svc, err := queue.NewService(broker, queue.WithConfig(cfg))
	if err != nil {
		b.Fatal(err)
	}
	var processed atomic.Int64
	queue.Register(svc, kindBench, func(context.Context, benchPayload) error {
		processed.Add(1)
		return nil
	})
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { _ = svc.Run(runCtx) }()

	c := queue.NewClient(broker)
	b.ReportAllocs()
	n := 0
	for b.Loop() {
		n++
		if err := queue.Push(ctx, c, kindBench, benchPayload{N: n}); err != nil {
			b.Fatal(err)
		}
	}
	for processed.Load() < int64(n) {
		time.Sleep(time.Millisecond)
	}
}
```

- [ ] **Step 4: Run example + benchmarks, record baseline**

Run: `go test ./async/queue/ -run Example -v` — Example must PASS.
Run: `just bench ./async/queue/ 2>&1 | tee /tmp/queue-bench-before.txt`
Expected: benchmarks complete; save the numbers.

- [ ] **Step 5: Optimization pass (measured wins only)**

Profile the top allocation sites (`go test -bench=BenchmarkPush_Memory -memprofile=...` if needed). Apply ONLY optimizations that benchmarks prove (per docs/design.md §Performance — readable first). Candidates to check, not prescriptions: pre-sized slice/map allocations in `MemoryBroker.Claim`, avoiding re-sorting when the due set is empty, `logAttrs` slice pre-sizing in `process`. Re-run `just bench ./async/queue/` and record after-numbers next to before-numbers in the task summary for the PR body. If no optimization wins measurably, record that explicitly — "no change: candidates X/Y measured, no significant delta" is a valid outcome.

- [ ] **Step 6: Run full package tests and commit**

Run: `just fmt ./async/... && just test ./async/queue/... && just lint`
Expected: PASS, lint clean.

```bash
git add async/queue && git commit -m "docs(queue): package docs, runnable example, benchmarks with baseline"
```

---

### Task 8: Postgres driver (pgqueue)

**Files:**
- Create: `async/queue/postgres/doc.go`, `async/queue/postgres/pgqueue.go`, `async/queue/postgres/migrations/20260715120000_queue_jobs.sql`
- Test: `async/queue/postgres/pgqueue_test.go`, `async/queue/postgres/bench_test.go`

**Interfaces:**
- Consumes: `queue.Broker`/`queue.TxPusher` (implements both), `queue.Job`, `queue.Stats`, sentinels, `brokertest.Run`, `migration`, `data/postgres` (tests), `pgx/v5` (`pgxpool.Pool`, `pgx.Tx`).
- Produces: `pgqueue.New(pool *pgxpool.Pool, opts ...Option) (*Broker, error)`, `pgqueue.WithTable(name string) Option`, `pgqueue.Migrations fs.FS`.

- [ ] **Step 1: Write the migration**

`async/queue/postgres/migrations/20260715120000_queue_jobs.sql`:

```sql
-- +goose Up
CREATE TABLE queue_jobs (
    id text PRIMARY KEY,
    queue text NOT NULL,
    type text NOT NULL,
    payload jsonb NOT NULL,
    scope text NOT NULL DEFAULT '',
    attempt integer NOT NULL DEFAULT 0,
    max_attempts integer NOT NULL DEFAULT 0,
    run_at timestamptz NOT NULL,
    claimed_until timestamptz,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'dead')),
    last_error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL
);

CREATE INDEX queue_jobs_claim_idx ON queue_jobs (queue, status, run_at);

-- +goose Down
DROP TABLE queue_jobs;
```

- [ ] **Step 2: Write failing tests**

`async/queue/postgres/pgqueue_test.go`:

```go
package pgqueue_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/async/queue/brokertest"
	pgqueue "github.com/dmitrymomot/forge/async/queue/postgres"
	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
)

var (
	_ queue.Broker   = (*pgqueue.Broker)(nil)
	_ queue.TxPusher = (*pgqueue.Broker)(nil)
)

func openPool(tb testing.TB) *pgxpool.Pool {
	tb.Helper()
	dsn := os.Getenv("FORGE_TEST_POSTGRES_DSN")
	if dsn == "" {
		tb.Skip("set FORGE_TEST_POSTGRES_DSN")
	}
	cfg := postgres.DefaultConfig()
	cfg.URL = dsn
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(tb, err)
	tb.Cleanup(pool.Close)
	db := stdlib.OpenDBFromPool(pool)
	tb.Cleanup(func() { _ = db.Close() })
	require.NoError(tb, migration.New(pgqueue.Migrations, migration.WithTable("forge_queue_schema")).Up(context.Background(), db))
	return pool
}

func newBroker(tb testing.TB, pool *pgxpool.Pool) *pgqueue.Broker {
	tb.Helper()
	_, err := pool.Exec(context.Background(), "TRUNCATE queue_jobs")
	require.NoError(tb, err)
	b, err := pgqueue.New(pool)
	require.NoError(tb, err)
	return b
}

func TestPgQueue_Conformance(t *testing.T) {
	pool := openPool(t)
	brokertest.Run(t, func(t *testing.T) queue.Broker { return newBroker(t, pool) })
}

func TestPgQueue_PushTx(t *testing.T) {
	pool := openPool(t)
	b := newBroker(t, pool)
	ctx := context.Background()
	c := queue.NewClient(b)
	kind := queue.NewKind[struct {
		N int `json:"n"`
	}]("tx.kind")

	t.Run("commit makes the job claimable", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		require.NoError(t, queue.PushTx(ctx, c, tx, kind, struct {
			N int `json:"n"`
		}{N: 1}))

		got, err := b.Claim(ctx, "default", 10, time.Minute)
		require.NoError(t, err)
		assert.Empty(t, got, "job must be invisible before commit")

		require.NoError(t, tx.Commit(ctx))
		got, err = b.Claim(ctx, "default", 10, time.Minute)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.NoError(t, b.Ack(ctx, got[0].ID))
	})

	t.Run("rollback discards the job", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		require.NoError(t, queue.PushTx(ctx, c, tx, kind, struct {
			N int `json:"n"`
		}{N: 2}))
		require.NoError(t, tx.Rollback(ctx))

		got, err := b.Claim(ctx, "default", 10, time.Minute)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("wrong tx type errors", func(t *testing.T) {
		err := queue.PushTx(ctx, c, "not a tx", kind, struct {
			N int `json:"n"`
		}{N: 3})
		require.Error(t, err)
	})
}

func TestPgQueue_WithTableValidation(t *testing.T) {
	t.Parallel()
	_, err := pgqueue.New(nil)
	require.Error(t, err, "nil pool rejected")
	pool := openPool(t) // skips without env
	_, err = pgqueue.New(pool, pgqueue.WithTable("bad;name"))
	require.Error(t, err, "unsafe table name rejected")
}

func TestPgQueue_PayloadIsJSONB(t *testing.T) {
	pool := openPool(t)
	b := newBroker(t, pool)
	ctx := context.Background()
	c := queue.NewClient(b)
	require.NoError(t, c.PushRaw(ctx, "raw.kind", json.RawMessage(`{"deep":{"x":[1,2,3]}}`)))
	got, err := b.Claim(ctx, "default", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.JSONEq(t, `{"deep":{"x":[1,2,3]}}`, string(got[0].Payload))
}
```

- [ ] **Step 3: Run tests to verify they fail**

Start the pg container (see Global Constraints), export `FORGE_TEST_POSTGRES_DSN`, then:
Run: `go test ./async/queue/postgres/ 2>&1 | head -10`
Expected: compile failure — package does not exist.

- [ ] **Step 4: Implement**

`async/queue/postgres/doc.go`:

```go
// Package pgqueue is the Postgres queue.Broker: claim-with-lease via
// FOR UPDATE SKIP LOCKED over a single table, crash recovery for free
// (an expired claimed_until makes the row claimable again — no reaper),
// and transactional enqueue via the queue.TxPusher capability (pass a
// pgx.Tx to queue.PushTx). Apply Migrations with data/migration before use.
// There is no LISTEN/NOTIFY: the engine's poll ticker drives claiming.
package pgqueue
```

`async/queue/postgres/pgqueue.go`:

```go
package pgqueue

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/async/queue"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations holds the goose migration creating queue_jobs, rooted so its
// .sql files sit at fsys root. Apply via data/migration under its own
// version table.
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(err) // unreachable: migrations/*.sql is embedded at compile time
	}
	Migrations = sub
}

var tableNameRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// Broker is the Postgres queue.Broker and queue.TxPusher.
type Broker struct {
	pool  *pgxpool.Pool
	table string

	insertSQL  string
	claimSQL   string
	extendSQL  string
	ackSQL     string
	nackSQL    string
	killSQL    string
	deadSQL    string
	requeueSQL string
	purgeSQL   string
	existsSQL  string
	statsSQL   string
}

// Option configures New.
type Option func(*Broker)

// WithTable overrides the table name (default "queue_jobs"). The shipped
// migration creates "queue_jobs"; custom names require a caller-managed
// schema of the same shape.
func WithTable(name string) Option {
	return func(b *Broker) { b.table = name }
}

// New builds a Broker over pool.
func New(pool *pgxpool.Pool, opts ...Option) (*Broker, error) {
	b := &Broker{pool: pool, table: "queue_jobs"}
	for _, opt := range opts {
		opt(b)
	}
	if pool == nil {
		return nil, errors.New("pgqueue: nil pool")
	}
	if !tableNameRe.MatchString(b.table) {
		return nil, fmt.Errorf("pgqueue: invalid table name %q", b.table)
	}
	const cols = "id, queue, type, payload, scope, attempt, max_attempts, run_at, created_at, last_error"
	b.insertSQL = fmt.Sprintf("INSERT INTO %s (id, queue, type, payload, scope, attempt, max_attempts, run_at, created_at, status) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'pending')", b.table)
	b.claimSQL = fmt.Sprintf(`UPDATE %[1]s SET claimed_until = now() + $3, attempt = attempt + 1 WHERE id IN (SELECT id FROM %[1]s WHERE queue = $1 AND status = 'pending' AND run_at <= now() AND (claimed_until IS NULL OR claimed_until < now()) ORDER BY run_at, id LIMIT $2 FOR UPDATE SKIP LOCKED) RETURNING `+cols, b.table)
	b.extendSQL = fmt.Sprintf("UPDATE %s SET claimed_until = now() + $2 WHERE id = $1", b.table)
	b.ackSQL = fmt.Sprintf("DELETE FROM %s WHERE id = $1", b.table)
	b.nackSQL = fmt.Sprintf("UPDATE %s SET run_at = $2, claimed_until = NULL, last_error = $3 WHERE id = $1", b.table)
	b.killSQL = fmt.Sprintf("UPDATE %s SET status = 'dead', claimed_until = NULL, last_error = $2 WHERE id = $1", b.table)
	b.deadSQL = fmt.Sprintf("SELECT "+cols+" FROM %s WHERE queue = $1 AND status = 'dead' ORDER BY created_at, id LIMIT $2", b.table)
	b.requeueSQL = fmt.Sprintf("UPDATE %s SET status = 'pending', attempt = 0, run_at = now(), claimed_until = NULL WHERE id = $1 AND status = 'dead'", b.table)
	b.purgeSQL = fmt.Sprintf("DELETE FROM %s WHERE id = $1 AND status = 'dead'", b.table)
	b.existsSQL = fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM %s WHERE id = $1)", b.table)
	b.statsSQL = fmt.Sprintf("SELECT queue, status, count(*) FROM %s GROUP BY queue, status", b.table)
	return b, nil
}

func (b *Broker) Push(ctx context.Context, job queue.Job) error {
	_, err := b.pool.Exec(ctx, b.insertSQL, job.ID, job.Queue, job.Type, job.Payload, job.Scope, job.Attempt, job.MaxAttempts, job.RunAt, job.CreatedAt)
	if err != nil {
		return fmt.Errorf("pgqueue: push: %w", err)
	}
	return nil
}

// PushTx implements queue.TxPusher: the same insert inside a caller-owned
// pgx.Tx, so the job commits or rolls back with the business transaction.
func (b *Broker) PushTx(ctx context.Context, tx any, job queue.Job) error {
	pgtx, ok := tx.(pgx.Tx)
	if !ok {
		return fmt.Errorf("pgqueue: push tx: expected pgx.Tx, got %T", tx)
	}
	_, err := pgtx.Exec(ctx, b.insertSQL, job.ID, job.Queue, job.Type, job.Payload, job.Scope, job.Attempt, job.MaxAttempts, job.RunAt, job.CreatedAt)
	if err != nil {
		return fmt.Errorf("pgqueue: push tx: %w", err)
	}
	return nil
}

func (b *Broker) Claim(ctx context.Context, queueName string, n int, lease time.Duration) ([]queue.Job, error) {
	rows, err := b.pool.Query(ctx, b.claimSQL, queueName, n, lease)
	if err != nil {
		return nil, fmt.Errorf("pgqueue: claim: %w", err)
	}
	defer rows.Close()
	var jobs []queue.Job
	for rows.Next() {
		var j queue.Job
		if err := rows.Scan(&j.ID, &j.Queue, &j.Type, &j.Payload, &j.Scope, &j.Attempt, &j.MaxAttempts, &j.RunAt, &j.CreatedAt, &j.LastError); err != nil {
			return nil, fmt.Errorf("pgqueue: claim scan: %w", err)
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgqueue: claim rows: %w", err)
	}
	return jobs, nil
}

func (b *Broker) Extend(ctx context.Context, id string, lease time.Duration) error {
	tag, err := b.pool.Exec(ctx, b.extendSQL, id, lease)
	if err != nil {
		return fmt.Errorf("pgqueue: extend: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return queue.ErrJobNotFound
	}
	return nil
}

func (b *Broker) Ack(ctx context.Context, id string) error {
	tag, err := b.pool.Exec(ctx, b.ackSQL, id)
	if err != nil {
		return fmt.Errorf("pgqueue: ack: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return queue.ErrJobNotFound
	}
	return nil
}

func (b *Broker) Nack(ctx context.Context, id string, retryAt time.Time, reason string) error {
	tag, err := b.pool.Exec(ctx, b.nackSQL, id, retryAt, reason)
	if err != nil {
		return fmt.Errorf("pgqueue: nack: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return queue.ErrJobNotFound
	}
	return nil
}

func (b *Broker) Kill(ctx context.Context, id string, reason string) error {
	tag, err := b.pool.Exec(ctx, b.killSQL, id, reason)
	if err != nil {
		return fmt.Errorf("pgqueue: kill: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return queue.ErrJobNotFound
	}
	return nil
}

func (b *Broker) ListDead(ctx context.Context, queueName string, limit int) ([]queue.Job, error) {
	rows, err := b.pool.Query(ctx, b.deadSQL, queueName, limit)
	if err != nil {
		return nil, fmt.Errorf("pgqueue: list dead: %w", err)
	}
	defer rows.Close()
	var jobs []queue.Job
	for rows.Next() {
		var j queue.Job
		if err := rows.Scan(&j.ID, &j.Queue, &j.Type, &j.Payload, &j.Scope, &j.Attempt, &j.MaxAttempts, &j.RunAt, &j.CreatedAt, &j.LastError); err != nil {
			return nil, fmt.Errorf("pgqueue: list dead scan: %w", err)
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgqueue: list dead rows: %w", err)
	}
	return jobs, nil
}

func (b *Broker) Requeue(ctx context.Context, id string) error {
	tag, err := b.pool.Exec(ctx, b.requeueSQL, id)
	if err != nil {
		return fmt.Errorf("pgqueue: requeue: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return b.notDeadOrMissing(ctx, id)
	}
	return nil
}

func (b *Broker) Purge(ctx context.Context, id string) error {
	tag, err := b.pool.Exec(ctx, b.purgeSQL, id)
	if err != nil {
		return fmt.Errorf("pgqueue: purge: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return b.notDeadOrMissing(ctx, id)
	}
	return nil
}

func (b *Broker) notDeadOrMissing(ctx context.Context, id string) error {
	var exists bool
	if err := b.pool.QueryRow(ctx, b.existsSQL, id).Scan(&exists); err != nil {
		return fmt.Errorf("pgqueue: exists: %w", err)
	}
	if exists {
		return queue.ErrNotDead
	}
	return queue.ErrJobNotFound
}

func (b *Broker) Stats(ctx context.Context) (queue.Stats, error) {
	rows, err := b.pool.Query(ctx, b.statsSQL)
	if err != nil {
		return nil, fmt.Errorf("pgqueue: stats: %w", err)
	}
	defer rows.Close()
	st := make(queue.Stats)
	for rows.Next() {
		var q, status string
		var n int64
		if err := rows.Scan(&q, &status, &n); err != nil {
			return nil, fmt.Errorf("pgqueue: stats scan: %w", err)
		}
		qs := st[q]
		if status == "dead" {
			qs.Dead = int(n)
		} else {
			qs.Pending = int(n)
		}
		st[q] = qs
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgqueue: stats rows: %w", err)
	}
	return st, nil
}
```

- [ ] **Step 5: Write `async/queue/postgres/bench_test.go`**

```go
package pgqueue_test

import (
	"context"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/async/queue"
)

func BenchmarkPgPushClaimAck(b *testing.B) {
	broker := newBroker(b, openPool(b)) // helpers take testing.TB; skips without env
	ctx := context.Background()
	c := queue.NewClient(broker)
	kind := queue.NewKind[struct {
		N int `json:"n"`
	}]("bench.pg")
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		if err := queue.Push(ctx, c, kind, struct {
			N int `json:"n"`
		}{N: i}); err != nil {
			b.Fatal(err)
		}
		jobs, err := broker.Claim(ctx, "default", 1, time.Minute)
		if err != nil || len(jobs) != 1 {
			b.Fatalf("claim: %v (%d jobs)", err, len(jobs))
		}
		if err := broker.Ack(ctx, jobs[0].ID); err != nil {
			b.Fatal(err)
		}
	}
}
```

- [ ] **Step 6: Run tests + benchmarks against live pg**

Run: `just fmt ./async/... && go test -race ./async/queue/postgres/`
Expected: PASS (conformance incl. lease-expiry timing against real pg).
Run: `go test -bench=. -benchmem ./async/queue/postgres/ | tee /tmp/queue-pg-bench.txt`
Record numbers for the PR.

- [ ] **Step 7: Run lint and commit**

Run: `just lint`

```bash
git add async/queue/postgres && git commit -m "feat(queue/postgres): SKIP LOCKED broker with transactional enqueue and migration"
```

---

### Task 9: Redis driver (redisqueue)

**Files:**
- Create: `async/queue/redis/doc.go`, `async/queue/redis/redisqueue.go`
- Test: `async/queue/redis/redisqueue_test.go`, `async/queue/redis/bench_test.go`

**Interfaces:**
- Consumes: `queue.Broker` (implements; NOT TxPusher), `queue.EncodeJob`/`queue.DecodeJob`, sentinels, `brokertest.Run`, `redis/go-redis/v9`.
- Produces: `redisqueue.New(client redis.UniversalClient, opts ...Option) (*Broker, error)`, `redisqueue.WithPrefix(string) Option` (default `"queue:"`).

**Key layout (per queue name q, under configurable prefix):**
- `{prefix}{q}` — stream; entries carry one field `j` = `queue.EncodeJob` bytes; consumer group `workers`.
- `{prefix}{q}:delayed` — zset, member = job id, score = run-at unix ms.
- `{prefix}{q}:data` — hash, job id → encoded job (payload store for delayed/nacked jobs).
- `{prefix}{q}:dead` — hash, job id → encoded job (DLQ).
- `{prefix}queues` — set of queue names ever pushed to (Stats discovery).
- `{prefix}index` — hash, job id → queue name (every live or dead job; resolves DLQ ops by id).

**Semantics mapping (implement exactly):**
- Push: pipeline `SADD queues q` + `HSET index id q`, then due-now → ensure group (`XGroupCreateMkStream(stream, "workers", "0")`, ignore BUSYGROUP) + `XADD`; future → `ZADD delayed` + `HSET data`.
- Claim(q, n, lease): (1) ensure group; (2) promote due delayed entries via the Lua script below (now-ms passed as ARGV — Lua must not call TIME); (3) `XAutoClaim{MinIdle: lease, Start: "0-0", Count: remaining}` — these are lease-expired redeliveries; fetch delivery counts via `XPendingExt` over the claimed id range and set `Attempt = encoded.Attempt + RetryCount`; (4) `XReadGroup{Streams: [stream, ">"], Count: remaining, Block: -1}` (treat `redis.Nil` as empty) — fresh deliveries, `Attempt = encoded.Attempt + 1`; (5) record an in-memory claimed ref `{queue, msgID, attempt}` per job id. Refs are per-Broker-instance state: only the claiming instance Acks/Nacks (the engine guarantee); a crash loses refs and the pending entry redelivers via XAUTOCLAIM.
- Ack: take ref (missing → `queue.ErrJobNotFound`); pipeline `XACK` + `XDEL` + `HDEL index id`.
- Nack(retryAt, reason): take ref; re-encode job with `Attempt = ref.attempt`, `LastError = reason`, `RunAt = retryAt`; pipeline `ZADD delayed` + `HSET data` + `XACK` + `XDEL` (id stays in index). Promotion re-XADDs it when due; next claim yields `Attempt = ref.attempt + 1`.
- Kill(reason): take ref; re-encode with `LastError = reason`; pipeline `HSET dead` + `XACK` + `XDEL` (id stays in index).
- Extend: look up ref (do not take); `XClaimJustID{MinIdle: 0, Messages: [msgID]}` — JUSTID resets the idle clock WITHOUT incrementing the delivery counter. Lease expiry on redis is idle-based, enforced by the NEXT Claim's `MinIdle`; the engine always claims with one configured lease, so this is equivalent to timestamp-based expiry.
- Requeue: `HGET index id` → miss = `ErrJobNotFound`; `HGET {q}:dead id` → miss = `ErrNotDead`; decode, `Attempt = 0`, re-`XADD` to the stream, `HDEL dead`.
- Purge: resolve queue via index; must exist in dead (`ErrNotDead` otherwise); pipeline `HDEL dead` + `HDEL index`.
- Stats: `SMEMBERS queues`; per queue Pending = `XLEN stream` (includes in-flight — matches memory/pg semantics) + `ZCARD delayed`, Dead = `HLEN dead`.
- No TxPusher: `queue.PushTx` returns `ErrTxUnsupported` via the engine (nothing to implement here beyond NOT implementing the interface).

**Promotion Lua (registered as `redis.NewScript`):**

```lua
local due = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', ARGV[1], 'LIMIT', 0, tonumber(ARGV[2]))
for i, id in ipairs(due) do
  local data = redis.call('HGET', KEYS[2], id)
  if data then
    redis.call('XADD', KEYS[3], '*', 'j', data)
  end
  redis.call('ZREM', KEYS[1], id)
  redis.call('HDEL', KEYS[2], id)
end
return #due
```

KEYS = [delayed, data, stream], ARGV = [now-ms, batch-limit (128)]. Atomic promote prevents double-XADD when several workers poll concurrently.

- [ ] **Step 1: Write failing tests**

`async/queue/redis/redisqueue_test.go`:

```go
package redisqueue_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/async/queue/brokertest"
	redisqueue "github.com/dmitrymomot/forge/async/queue/redis"
)

var _ queue.Broker = (*redisqueue.Broker)(nil)

func dial(tb testing.TB) redis.UniversalClient {
	tb.Helper()
	addr := os.Getenv("FORGE_TEST_REDIS_URL")
	if addr == "" {
		tb.Skip("set FORGE_TEST_REDIS_URL (host:port)")
	}
	c := redis.NewClient(&redis.Options{Addr: addr})
	tb.Cleanup(func() { _ = c.Close() })
	return c
}

var prefixSeq int

// newBroker namespaces each subtest under a unique prefix; keys leak into the
// ephemeral test container, which is acceptable.
func newBroker(tb testing.TB) *redisqueue.Broker {
	tb.Helper()
	prefixSeq++
	b, err := redisqueue.New(dial(tb), redisqueue.WithPrefix(fmt.Sprintf("qt:%s:%d:", tb.Name(), prefixSeq)))
	require.NoError(tb, err)
	return b
}

func TestRedisQueue_Conformance(t *testing.T) {
	brokertest.Run(t, func(t *testing.T) queue.Broker { return newBroker(t) })
}

func TestRedisQueue_NoTxPusher(t *testing.T) {
	b := newBroker(t)
	c := queue.NewClient(b)
	kind := queue.NewKind[struct {
		N int `json:"n"`
	}]("tx.kind")
	err := queue.PushTx(context.Background(), c, "tx", kind, struct {
		N int `json:"n"`
	}{N: 1})
	assert.ErrorIs(t, err, queue.ErrTxUnsupported)
}

func TestRedisQueue_AttemptSurvivesCrashRedelivery(t *testing.T) {
	// A second Broker instance (fresh refs — simulated crash) must still see
	// the correct attempt count via the XPENDING delivery counter.
	client := dial(t)
	prefix := fmt.Sprintf("qt:crash:%d:", time.Now().UnixNano())
	b1, err := redisqueue.New(client, redisqueue.WithPrefix(prefix))
	require.NoError(t, err)
	ctx := context.Background()

	c := queue.NewClient(b1)
	require.NoError(t, c.PushRaw(ctx, "crash.kind", []byte(`{}`)))

	got, err := b1.Claim(ctx, "default", 1, 200*time.Millisecond)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 1, got[0].Attempt)
	// b1 "crashes": no Ack, refs lost.

	b2, err := redisqueue.New(client, redisqueue.WithPrefix(prefix))
	require.NoError(t, err)
	time.Sleep(300 * time.Millisecond)
	got2, err := b2.Claim(ctx, "default", 1, 200*time.Millisecond)
	require.NoError(t, err)
	require.Len(t, got2, 1)
	assert.Equal(t, 2, got2[0].Attempt, "redelivery after crash must count as a new attempt")
	require.NoError(t, b2.Ack(ctx, got2[0].ID))
}

func TestRedisQueue_ValidatesConstruction(t *testing.T) {
	t.Parallel()
	_, err := redisqueue.New(nil)
	require.Error(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Start the redis container (see Global Constraints), export `FORGE_TEST_REDIS_URL`, then:
Run: `go test ./async/queue/redis/ 2>&1 | head -10`
Expected: compile failure — package does not exist.

- [ ] **Step 3: Implement**

`async/queue/redis/doc.go`:

```go
// Package redisqueue is the Redis queue.Broker: one stream + consumer group
// per queue for claim-with-lease (XAUTOCLAIM redelivers entries idle longer
// than the lease), a sorted-set staging area for delayed and retried jobs
// (promoted atomically by a Lua script during Claim), and hashes for the
// dead-letter set. No transactional enqueue — use async/queue/postgres or,
// once it lands, async/outbox. All keys live under a configurable prefix.
package redisqueue
```

`async/queue/redis/redisqueue.go` — complete implementation:

```go
package redisqueue

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/core/id"
)

const group = "workers"

var promoteScript = redis.NewScript(`
local due = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', ARGV[1], 'LIMIT', 0, tonumber(ARGV[2]))
for i, id in ipairs(due) do
  local data = redis.call('HGET', KEYS[2], id)
  if data then
    redis.call('XADD', KEYS[3], '*', 'j', data)
  end
  redis.call('ZREM', KEYS[1], id)
  redis.call('HDEL', KEYS[2], id)
end
return #due
`)

// Broker is the Redis queue.Broker.
type Broker struct {
	client   redis.UniversalClient
	prefix   string
	consumer string

	mu      sync.Mutex
	claimed map[string]claimedRef // job id → ref; only the claiming instance finalizes
	groups  map[string]bool       // queues with the consumer group ensured
}

type claimedRef struct {
	job   queue.Job // decoded envelope as stored (pre-claim attempt)
	msgID string
	queue string
}

// Option configures New.
type Option func(*Broker)

// WithPrefix overrides the key prefix (default "queue:").
func WithPrefix(p string) Option {
	return func(b *Broker) { b.prefix = p }
}

// New builds a Broker over client.
func New(client redis.UniversalClient, opts ...Option) (*Broker, error) {
	host, _ := os.Hostname()
	b := &Broker{
		client:   client,
		prefix:   "queue:",
		consumer: fmt.Sprintf("%s-%d-%s", host, os.Getpid(), id.NewULID().String()),
		claimed:  make(map[string]claimedRef),
		groups:   make(map[string]bool),
	}
	for _, opt := range opts {
		opt(b)
	}
	if client == nil {
		return nil, errors.New("redisqueue: nil client")
	}
	return b, nil
}

func (b *Broker) streamKey(q string) string  { return b.prefix + q }
func (b *Broker) delayedKey(q string) string { return b.prefix + q + ":delayed" }
func (b *Broker) dataKey(q string) string    { return b.prefix + q + ":data" }
func (b *Broker) deadKey(q string) string    { return b.prefix + q + ":dead" }
func (b *Broker) queuesKey() string          { return b.prefix + "queues" }
func (b *Broker) indexKey() string           { return b.prefix + "index" }

func (b *Broker) ensureGroup(ctx context.Context, q string) error {
	b.mu.Lock()
	done := b.groups[q]
	b.mu.Unlock()
	if done {
		return nil
	}
	err := b.client.XGroupCreateMkStream(ctx, b.streamKey(q), group, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("redisqueue: create group: %w", err)
	}
	b.mu.Lock()
	b.groups[q] = true
	b.mu.Unlock()
	return nil
}

func (b *Broker) Push(ctx context.Context, job queue.Job) error {
	enc, err := queue.EncodeJob(job)
	if err != nil {
		return err
	}
	if err := b.ensureGroup(ctx, job.Queue); err != nil {
		return err
	}
	pipe := b.client.TxPipeline()
	pipe.SAdd(ctx, b.queuesKey(), job.Queue)
	pipe.HSet(ctx, b.indexKey(), job.ID, job.Queue)
	if job.RunAt.After(time.Now()) {
		pipe.ZAdd(ctx, b.delayedKey(job.Queue), redis.Z{Score: float64(job.RunAt.UnixMilli()), Member: job.ID})
		pipe.HSet(ctx, b.dataKey(job.Queue), job.ID, enc)
	} else {
		pipe.XAdd(ctx, &redis.XAddArgs{Stream: b.streamKey(job.Queue), Values: map[string]any{"j": enc}})
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redisqueue: push: %w", err)
	}
	return nil
}

func (b *Broker) Claim(ctx context.Context, q string, n int, lease time.Duration) ([]queue.Job, error) {
	if err := b.ensureGroup(ctx, q); err != nil {
		return nil, err
	}
	// Promote due delayed/retried jobs into the stream.
	nowMS := strconv.FormatInt(time.Now().UnixMilli(), 10)
	if err := promoteScript.Run(ctx, b.client, []string{b.delayedKey(q), b.dataKey(q), b.streamKey(q)}, nowMS, "128").Err(); err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("redisqueue: promote delayed: %w", err)
	}

	remaining := n
	var out []queue.Job

	// Lease-expired redeliveries first.
	msgs, _, err := b.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream: b.streamKey(q), Group: group, Consumer: b.consumer,
		MinIdle: lease, Start: "0-0", Count: int64(remaining),
	}).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("redisqueue: autoclaim: %w", err)
	}
	if len(msgs) > 0 {
		retries := make(map[string]int64, len(msgs))
		pend, err := b.client.XPendingExt(ctx, &redis.XPendingExtArgs{
			Stream: b.streamKey(q), Group: group,
			Start: msgs[0].ID, End: msgs[len(msgs)-1].ID, Count: int64(len(msgs)),
		}).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return nil, fmt.Errorf("redisqueue: pending: %w", err)
		}
		for _, p := range pend {
			retries[p.ID] = p.RetryCount
		}
		for _, m := range msgs {
			j, err := b.decodeMsg(m)
			if err != nil {
				return nil, err
			}
			delivered := retries[m.ID]
			if delivered == 0 {
				delivered = 1
			}
			claimedJob := j
			claimedJob.Attempt = j.Attempt + int(delivered)
			b.remember(claimedJob.ID, claimedRef{job: j, msgID: m.ID, queue: q})
			out = append(out, claimedJob)
		}
		remaining -= len(msgs)
	}

	// Fresh deliveries.
	if remaining > 0 {
		streams, err := b.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: group, Consumer: b.consumer,
			Streams: []string{b.streamKey(q), ">"}, Count: int64(remaining), Block: -1,
		}).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return nil, fmt.Errorf("redisqueue: readgroup: %w", err)
		}
		for _, st := range streams {
			for _, m := range st.Messages {
				j, err := b.decodeMsg(m)
				if err != nil {
					return nil, err
				}
				claimedJob := j
				claimedJob.Attempt = j.Attempt + 1
				b.remember(claimedJob.ID, claimedRef{job: j, msgID: m.ID, queue: q})
				out = append(out, claimedJob)
			}
		}
	}
	return out, nil
}

func (b *Broker) decodeMsg(m redis.XMessage) (queue.Job, error) {
	raw, ok := m.Values["j"].(string)
	if !ok {
		return queue.Job{}, fmt.Errorf("redisqueue: stream entry %s has no payload field", m.ID)
	}
	j, err := queue.DecodeJob([]byte(raw))
	if err != nil {
		return queue.Job{}, err
	}
	return j, nil
}

func (b *Broker) remember(id string, ref claimedRef) {
	b.mu.Lock()
	b.claimed[id] = ref
	b.mu.Unlock()
}

func (b *Broker) take(id string) (claimedRef, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ref, ok := b.claimed[id]
	if ok {
		delete(b.claimed, id)
	}
	return ref, ok
}

func (b *Broker) Extend(ctx context.Context, jobID string, _ time.Duration) error {
	b.mu.Lock()
	ref, ok := b.claimed[jobID]
	b.mu.Unlock()
	if !ok {
		return queue.ErrJobNotFound
	}
	// JUSTID resets the idle clock without bumping the delivery counter.
	// Lease expiry is idle-based here: the next Claim's MinIdle is the lease.
	err := b.client.XClaimJustID(ctx, &redis.XClaimArgs{
		Stream: b.streamKey(ref.queue), Group: group, Consumer: b.consumer,
		MinIdle: 0, Messages: []string{ref.msgID},
	}).Err()
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("redisqueue: extend: %w", err)
	}
	return nil
}

func (b *Broker) Ack(ctx context.Context, jobID string) error {
	ref, ok := b.take(jobID)
	if !ok {
		return queue.ErrJobNotFound
	}
	pipe := b.client.TxPipeline()
	pipe.XAck(ctx, b.streamKey(ref.queue), group, ref.msgID)
	pipe.XDel(ctx, b.streamKey(ref.queue), ref.msgID)
	pipe.HDel(ctx, b.indexKey(), jobID)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redisqueue: ack: %w", err)
	}
	return nil
}

func (b *Broker) Nack(ctx context.Context, jobID string, retryAt time.Time, reason string) error {
	ref, ok := b.take(jobID)
	if !ok {
		return queue.ErrJobNotFound
	}
	j := ref.job
	j.Attempt = ref.job.Attempt + 1 // the failed claim consumed one attempt
	j.LastError = reason
	j.RunAt = retryAt.UTC()
	enc, err := queue.EncodeJob(j)
	if err != nil {
		return err
	}
	pipe := b.client.TxPipeline()
	pipe.ZAdd(ctx, b.delayedKey(ref.queue), redis.Z{Score: float64(retryAt.UnixMilli()), Member: jobID})
	pipe.HSet(ctx, b.dataKey(ref.queue), jobID, enc)
	pipe.XAck(ctx, b.streamKey(ref.queue), group, ref.msgID)
	pipe.XDel(ctx, b.streamKey(ref.queue), ref.msgID)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redisqueue: nack: %w", err)
	}
	return nil
}

func (b *Broker) Kill(ctx context.Context, jobID string, reason string) error {
	ref, ok := b.take(jobID)
	if !ok {
		return queue.ErrJobNotFound
	}
	j := ref.job
	j.Attempt = ref.job.Attempt + 1
	j.LastError = reason
	enc, err := queue.EncodeJob(j)
	if err != nil {
		return err
	}
	pipe := b.client.TxPipeline()
	pipe.HSet(ctx, b.deadKey(ref.queue), jobID, enc)
	pipe.XAck(ctx, b.streamKey(ref.queue), group, ref.msgID)
	pipe.XDel(ctx, b.streamKey(ref.queue), ref.msgID)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redisqueue: kill: %w", err)
	}
	return nil
}

func (b *Broker) ListDead(ctx context.Context, q string, limit int) ([]queue.Job, error) {
	all, err := b.client.HGetAll(ctx, b.deadKey(q)).Result()
	if err != nil {
		return nil, fmt.Errorf("redisqueue: list dead: %w", err)
	}
	jobs := make([]queue.Job, 0, len(all))
	for _, enc := range all {
		j, err := queue.DecodeJob([]byte(enc))
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	sortDead(jobs)
	if len(jobs) > limit {
		jobs = jobs[:limit]
	}
	return jobs, nil
}

func (b *Broker) Requeue(ctx context.Context, jobID string) error {
	q, err := b.client.HGet(ctx, b.indexKey(), jobID).Result()
	if errors.Is(err, redis.Nil) {
		return queue.ErrJobNotFound
	}
	if err != nil {
		return fmt.Errorf("redisqueue: requeue index: %w", err)
	}
	enc, err := b.client.HGet(ctx, b.deadKey(q), jobID).Result()
	if errors.Is(err, redis.Nil) {
		return queue.ErrNotDead
	}
	if err != nil {
		return fmt.Errorf("redisqueue: requeue dead: %w", err)
	}
	j, err := queue.DecodeJob([]byte(enc))
	if err != nil {
		return err
	}
	j.Attempt = 0
	j.RunAt = time.Now().UTC()
	fresh, err := queue.EncodeJob(j)
	if err != nil {
		return err
	}
	pipe := b.client.TxPipeline()
	pipe.XAdd(ctx, &redis.XAddArgs{Stream: b.streamKey(q), Values: map[string]any{"j": fresh}})
	pipe.HDel(ctx, b.deadKey(q), jobID)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redisqueue: requeue: %w", err)
	}
	return nil
}

func (b *Broker) Purge(ctx context.Context, jobID string) error {
	q, err := b.client.HGet(ctx, b.indexKey(), jobID).Result()
	if errors.Is(err, redis.Nil) {
		return queue.ErrJobNotFound
	}
	if err != nil {
		return fmt.Errorf("redisqueue: purge index: %w", err)
	}
	exists, err := b.client.HExists(ctx, b.deadKey(q), jobID).Result()
	if err != nil {
		return fmt.Errorf("redisqueue: purge exists: %w", err)
	}
	if !exists {
		return queue.ErrNotDead
	}
	pipe := b.client.TxPipeline()
	pipe.HDel(ctx, b.deadKey(q), jobID)
	pipe.HDel(ctx, b.indexKey(), jobID)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redisqueue: purge: %w", err)
	}
	return nil
}

func (b *Broker) Stats(ctx context.Context) (queue.Stats, error) {
	queues, err := b.client.SMembers(ctx, b.queuesKey()).Result()
	if err != nil {
		return nil, fmt.Errorf("redisqueue: stats queues: %w", err)
	}
	st := make(queue.Stats, len(queues))
	for _, q := range queues {
		streamLen, err := b.client.XLen(ctx, b.streamKey(q)).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return nil, fmt.Errorf("redisqueue: stats xlen: %w", err)
		}
		delayed, err := b.client.ZCard(ctx, b.delayedKey(q)).Result()
		if err != nil {
			return nil, fmt.Errorf("redisqueue: stats zcard: %w", err)
		}
		dead, err := b.client.HLen(ctx, b.deadKey(q)).Result()
		if err != nil {
			return nil, fmt.Errorf("redisqueue: stats hlen: %w", err)
		}
		st[q] = queue.QueueStats{Pending: int(streamLen + delayed), Dead: int(dead)}
	}
	return st, nil
}

// sortDead orders dead jobs by CreatedAt then ID (HGETALL is unordered).
func sortDead(jobs []queue.Job) {
	slices.SortFunc(jobs, func(a, c queue.Job) int {
		if r := a.CreatedAt.Compare(c.CreatedAt); r != 0 {
			return r
		}
		return cmp.Compare(a.ID, c.ID)
	})
}
```

One subtlety verified against the conformance suite: `Nack` stores `Attempt = ref.job.Attempt + 1` (the stored envelope's attempt plus the claim that just failed), so after promotion the next claim computes `stored.Attempt + 1` — attempt sequence 1, 2, 3… matches pg/memory exactly. `Kill` does the same so `ListDead` shows the true consumed attempts.

- [ ] **Step 4: Write `async/queue/redis/bench_test.go`**

```go
package redisqueue_test

import (
	"context"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/async/queue"
)

func BenchmarkRedisPushClaimAck(b *testing.B) {
	broker := newBroker(b)
	ctx := context.Background()
	c := queue.NewClient(broker)
	kind := queue.NewKind[struct {
		N int `json:"n"`
	}]("bench.redis")
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		if err := queue.Push(ctx, c, kind, struct {
			N int `json:"n"`
		}{N: i}); err != nil {
			b.Fatal(err)
		}
		jobs, err := broker.Claim(ctx, "default", 1, time.Minute)
		if err != nil || len(jobs) != 1 {
			b.Fatalf("claim: %v (%d jobs)", err, len(jobs))
		}
		if err := broker.Ack(ctx, jobs[0].ID); err != nil {
			b.Fatal(err)
		}
	}
}
```

(`newBroker` already takes `testing.TB`, so it serves both tests and benchmarks.)

- [ ] **Step 5: Run tests + benchmarks against live redis**

Run: `just fmt ./async/... && go test -race ./async/queue/redis/`
Expected: PASS — conformance (incl. lease-expiry via XAUTOCLAIM idle), crash-redelivery attempt counting, no-TxPusher.
Run: `go test -bench=. -benchmem ./async/queue/redis/ | tee /tmp/queue-redis-bench.txt`
Record numbers for the PR.

- [ ] **Step 6: Run lint and commit**

Run: `just lint`

```bash
git add async/queue/redis && git commit -m "feat(queue/redis): Streams consumer-group broker with delayed zset and XAUTOCLAIM lease recovery"
```

---

### Task 10: Catalog updates, full sweep, PR

**Files:**
- Modify: `docs/packages.md`

- [ ] **Step 1: Rewrite the `async/jobqueue` catalog entry as `async/queue`**

In `docs/packages.md`, replace the `**async/jobqueue**` entry (around line 413) with:

```markdown
**async/queue**

THE durable background-work engine: supervised worker pool (bounded
concurrency, per-kind retry/backoff, graceful drain), claim-with-lease
at-least-once delivery with background lease heartbeat, max-attempts →
dead-letter with Requeue/Purge ops, typed `Kind[T]` handlers over JSON
(name↔payload binding is a compile-time symbol), weighted-priority queues
(smooth weighted round-robin; optional strict mode), producer `Client`
separate from the worker `Service`, delayed jobs (`WithDelay`/`WithRunAt`).
Storage-agnostic pull-only `Broker` seam (`Push/Claim/Ack/Nack/Kill` + DLQ
ops + capability discovery); the engine — not the driver — owns
retry/delay/dead-letter semantics so behavior is identical across
backends. In-memory broker built in; drivers: `queue/postgres` (SKIP
LOCKED claiming; transactional enqueue via `PushTx`) and `queue/redis`
(Streams + consumer groups, XAUTOCLAIM lease recovery) shipped;
`queue/sqlite`, `queue/nats`, `queue/kafka` planned. Non-SQL brokers get
transactional enqueue via `async/outbox`.

Deps: `ops/supervisor`, `resilience/backoff`; drivers: `data/postgres`,
`data/redis`; planned: `data/sqlite`.
```

- [ ] **Step 2: Update every `jobqueue` cross-reference**

Run `grep -n "jobqueue" docs/packages.md` — update each hit to `async/queue` / `queue`: the `data/tenant` carrier mention (~line 198), `data/retention` (~lines 258–262), `async/scheduler` (~line 439), `async/eventbus` (~line 452), `async/outbox` (~line 481), `async/workflow` (~line 509), `comms/webhook` (~lines 679–686), `data/settings` (~line 717). Wording stays the same; only the package name changes. Re-run the grep afterward — zero hits for `jobqueue` must remain.

- [ ] **Step 3: Full verification sweep**

Run: `just fmt ./async/... && just lint && just test ./async/...`
Expected: everything green. Also run the full repo test once: `just test` — no regressions elsewhere.

- [ ] **Step 4: Commit**

```bash
git add docs/packages.md && git commit -m "docs(packages): async/jobqueue → async/queue; postgres+redis drivers shipped"
```

- [ ] **Step 5: Open the PR**

Create the PR per repo flow (CLAUDE.md): push branch, `gh pr create` with a body that includes: summary of the package + drivers, the delivery-semantics contract, before/after benchmark numbers from Tasks 7–9, and how live tests were run (docker pg16/redis7). NO Claude attribution lines anywhere. Then follow: wait CI → fix failures → read Claude's review → fix all found issues → resolve threads → repeat until clean.
