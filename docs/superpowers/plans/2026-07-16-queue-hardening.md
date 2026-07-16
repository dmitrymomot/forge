# async/queue Hardening & Performance Rework Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the approved spec `docs/superpowers/specs/2026-07-16-queue-hardening-design.md` — Broker v2 with lease fencing, two-table Postgres schema, redis Lua finalize + poison parking + maintenance, engine hardening (handler timeout, claim backoff, DLQ retention), bulk push, and benchmark comparison against `docs/superpowers/specs/2026-07-16-queue-bench-baseline.txt`.

**Architecture:** The `queue.Broker` seam changes shape once (Task 1) with the reference `MemoryBroker` and the `brokertest` conformance suite updated in the same commit; the new conformance subtests are the failing tests that the postgres (Task 2) and redis (Tasks 3–4) driver tasks make pass. Engine features land as independent tasks (5–9) against the memory broker. Benchmarks (10) and docs (11) close it out.

**Tech Stack:** Go 1.26, pgx/v5 (+pgxpool), go-redis/v9, goose migrations via `data/migration`, testcontainers via `testkit/{pgtest,redistest}`, testify.

## Global Constraints

- Work ONLY on the current branch `dm/async-queue-review-deb065`. Never switch or create branches.
- Runtime floors: **PostgreSQL >= 18** (`postgres:18-alpine` in testkit), **Redis >= 8** (`redis:8-alpine`).
- Clean slate: the existing migration file is rewritten in place (same filename); the `Broker` interface breaks freely; NO compatibility shims.
- Job IDs and claim tokens are **UUIDv7 strings** via `core/id`: `id.NewUUID().String()`. Never `id.NewULID()` in this package after Task 1.
- Go 1.26 idioms: `wg.Go(func(){...})`, `for range n`, `min`/`max` builtins, `new(expr)` where needed. Run `go tool modernize` mentally — no `ptr.To`-style helpers.
- After changing files in a package run `just fmt ./async/queue/...` (package-path form — single-file form trips a betteralign quirk).
- `just lint` must pass at the end of Tasks 4–11. **Between Tasks 1 and 4 the pg/redis driver packages are intentionally mid-rework**; Tasks 1–3 verify with the scoped commands given in their steps instead of repo-wide lint. This is the only sanctioned red window.
- Tests: black-box (`package queue_test`) unless testing unexported state (`service_internal_test.go` is the existing white-box file). Integration tests carry `//go:build integration` and run via `just test-integration <path>`; unit tier must stay Docker-free.
- Default loggers are `logger.NewNope()` (`ops/logger`), never `slog.Default()`.
- Structured logging: slog attrs only, single-line errors, no embedded stacks.
- No manual line wrapping in prose/commit bodies. No Claude attribution anywhere in git content.
- Timing-sensitive broker tests use the brokertest discipline: past-biased `RunAt` (`dueNow()`), poll-until-expected (`claimWithin`), never fixed-sleep-then-assert-once. Rationale: macOS Docker VM clock lags the host.
- Commit after every task with the message given in its final step.

## File Structure

| File | Responsibility |
|---|---|
| `async/queue/errors.go` | sentinels — gains `ErrLeaseLost` |
| `async/queue/job.go` | `Job`, `ClaimedJob`, `QueueStats` (+capped flags), `Stats` |
| `async/queue/broker.go` | `Broker` v2, `TxPusher`, `Maintainer` contracts |
| `async/queue/memory.go` | reference broker: per-queue buckets, fencing, died-at, retention |
| `async/queue/client.go` | producer: `Push`, `PushMany`, `PushTx`, `PushRaw`, DLQ passthrough |
| `async/queue/config.go` | worker knobs — gains `HandlerTimeout`, `DeadRetention` |
| `async/queue/options.go` | options — gains `WithServiceClock`, timeout set-flag |
| `async/queue/service.go` | worker engine: SWRR, heartbeat+cancel, backoff, sweep |
| `async/queue/brokertest/brokertest.go` | executable Broker contract |
| `async/queue/postgres/pgqueue.go` | two-table SQL driver |
| `async/queue/postgres/migrations/20260715120000_queue_jobs.sql` | rewritten schema |
| `async/queue/redis/redisqueue.go` | Streams driver: Lua fencing, dead ZSET, poison, Maintain |
| `async/queue/bench_test.go`, `postgres/bench_test.go`, `redis/bench_test.go` | benchmark suite |

---

### Task 1: Broker v2 contract, MemoryBroker, brokertest, engine threading

**Files:**
- Modify: `async/queue/errors.go`, `async/queue/job.go`, `async/queue/broker.go`
- Rewrite: `async/queue/memory.go`, `async/queue/brokertest/brokertest.go`
- Modify (mechanical threading): `async/queue/service.go`, `async/queue/client_test.go`, `async/queue/bench_test.go`, `async/queue/client.go`
- Test: `async/queue/memory_test.go` (unchanged file, now exercises the new suite)

**Interfaces:**
- Produces: `Broker` v2 (exact signatures below), `ClaimedJob{Job; Token string}`, `ErrLeaseLost`, `Maintainer`, `TxPusher{PushTx(ctx, tx any, jobs ...Job) error}`, `QueueStats{Pending, Dead int; PendingCapped, DeadCapped bool}`. `MemoryBroker` implements `Broker`. `brokertest.Run(t, factory)` covers fencing, batch push, died-at ordering, `PurgeDeadBefore`.
- Consumes: `core/id.NewUUID()`, `core/clock`.
- **Known interim state:** after this task `async/queue` and `async/queue/brokertest` build and pass unit tests; `async/queue/postgres` and `async/queue/redis` do NOT compile until Tasks 2–3. Verify with scoped commands only.

- [ ] **Step 1: Add `ErrLeaseLost` to `errors.go`**

Append to the `var (...)` block in `async/queue/errors.go`:

```go
	// ErrLeaseLost is returned by the token-fenced broker ops (Extend, Ack,
	// Nack, Kill) when the token no longer owns the job: the lease expired and
	// another claim took over, or the job was already finalized. Fenced ops
	// never return ErrJobNotFound — an unknown id is indistinguishable from an
	// already-finalized one.
	ErrLeaseLost = errors.New("queue: lease lost")
```

- [ ] **Step 2: Extend `job.go` with `ClaimedJob` and capped stats**

Replace the `QueueStats` declaration and add `ClaimedJob` after the `Job` type:

```go
// ClaimedJob is a Job plus the opaque fencing token proving ownership of the
// claim. Pass Token to Extend/Ack/Nack/Kill; once the lease expires and
// another worker claims the job, ops with the stale token return ErrLeaseLost
// and leave the new claim undisturbed.
type ClaimedJob struct {
	Job
	Token string
}

// QueueStats are per-queue counts reported by Broker.Stats. Backends that
// bound their counting (postgres, cap 10000) report at most the cap and set
// the corresponding Capped flag; memory and redis counts are exact.
type QueueStats struct {
	Pending       int
	Dead          int
	PendingCapped bool
	DeadCapped    bool
}
```

- [ ] **Step 3: Rewrite `broker.go` with the v2 contract**

Full file content:

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
//   - Push accepts a batch and is all-or-nothing; an empty batch is a no-op.
//   - Claim atomically sets the lease, stamps a fencing token, AND increments
//     the attempt counter. Claimed jobs return ordered by (run_at, id).
//   - A claimed job is invisible to Claim until its lease expires.
//   - Extend/Ack/Nack/Kill require the claim token and return ErrLeaseLost
//     when it no longer owns the job (lease lost to another claim, job already
//     finalized, or id unknown); a stale-token op must not disturb the state
//     of the current claim.
//   - Nack makes the job claimable again no earlier than retryAt and records
//     reason as LastError. Kill moves the job to the dead-letter store and
//     records the kill time.
//   - ListDead returns dead jobs ordered by kill time then id. Requeue resets
//     attempts to zero and returns a dead job to pending; Purge deletes a dead
//     job; both are unfenced (dead jobs have no lease) and return
//     ErrJobNotFound for unknown ids and ErrNotDead for live jobs.
//   - PurgeDeadBefore deletes dead jobs killed strictly before cutoff and
//     returns how many were removed.
type Broker interface {
	Push(ctx context.Context, jobs ...Job) error
	Claim(ctx context.Context, queue string, n int, lease time.Duration) ([]ClaimedJob, error)
	Extend(ctx context.Context, id, token string, lease time.Duration) error
	Ack(ctx context.Context, id, token string) error
	Nack(ctx context.Context, id, token string, retryAt time.Time, reason string) error
	Kill(ctx context.Context, id, token string, reason string) error
	ListDead(ctx context.Context, queue string, limit int) ([]Job, error)
	Requeue(ctx context.Context, id string) error
	Purge(ctx context.Context, id string) error
	PurgeDeadBefore(ctx context.Context, cutoff time.Time) (int, error)
	Stats(ctx context.Context) (Stats, error)
}

// TxPusher is an optional Broker capability: transactional enqueue inside a
// caller-owned database transaction. tx is driver-specific (pgqueue asserts
// pgx.Tx). Brokers without this capability make PushTx return
// ErrTxUnsupported.
type TxPusher interface {
	PushTx(ctx context.Context, tx any, jobs ...Job) error
}

// Maintainer is an optional Broker capability: periodic housekeeping the
// engine invokes from its sweep ticker (idle consumer cleanup, stale queue
// registry pruning). Implementations must be idempotent and safe to run from
// every worker instance concurrently.
type Maintainer interface {
	Maintain(ctx context.Context) error
}
```

- [ ] **Step 4: Rewrite `memory.go` (per-queue buckets, fencing, died-at, retention)**

Full file content:

```go
package queue

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/id"
)

// MemoryBroker is the built-in reference Broker: full semantics (leases,
// fencing tokens, delayed jobs, dead-letter, retention) over process memory.
// Use it for tests, dev/single-process apps, and as the behavioral reference
// for drivers. Storage is bucketed per queue, so Claim cost scales with the
// claimed queue's live set, not the whole broker. Jobs do not survive process
// restart.
type MemoryBroker struct {
	clk    clock.Clock
	queues map[string]*memQueue
	index  map[string]string // job id → queue name, for O(1) id-addressed ops
	mu     sync.Mutex
}

type memQueue struct {
	live map[string]*memJob
	dead map[string]*memJob
}

type memJob struct {
	claimedUntil time.Time
	diedAt       time.Time
	token        string
	job          Job
}

// MemoryOption configures NewMemoryBroker.
type MemoryOption func(*MemoryBroker)

// WithMemoryClock injects a clock (tests).
func WithMemoryClock(c clock.Clock) MemoryOption {
	return func(b *MemoryBroker) { b.clk = c }
}

// NewMemoryBroker builds an empty in-memory broker.
func NewMemoryBroker(opts ...MemoryOption) *MemoryBroker {
	b := &MemoryBroker{
		clk:    clock.System(),
		queues: make(map[string]*memQueue),
		index:  make(map[string]string),
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

func (b *MemoryBroker) bucket(q string) *memQueue {
	mq, ok := b.queues[q]
	if !ok {
		mq = &memQueue{live: make(map[string]*memJob), dead: make(map[string]*memJob)}
		b.queues[q] = mq
	}
	return mq
}

func (b *MemoryBroker) Push(_ context.Context, jobs ...Job) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, job := range jobs {
		b.bucket(job.Queue).live[job.ID] = &memJob{job: job}
		b.index[job.ID] = job.Queue
	}
	return nil
}

func (b *MemoryBroker) Claim(_ context.Context, queueName string, n int, lease time.Duration) ([]ClaimedJob, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	mq, ok := b.queues[queueName]
	if !ok {
		return nil, nil
	}
	now := b.clk.Now()
	due := make([]*memJob, 0, len(mq.live))
	for _, m := range mq.live {
		if !m.job.RunAt.After(now) && m.claimedUntil.Before(now) {
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
	token := id.NewUUID().String()
	out := make([]ClaimedJob, 0, len(due))
	for _, m := range due {
		m.claimedUntil = now.Add(lease)
		m.token = token
		m.job.Attempt++
		out = append(out, ClaimedJob{Job: m.job, Token: token})
	}
	return out, nil
}

// fenced returns the live job owned by token. A nil return means ErrLeaseLost
// for the caller: unknown id, dead job, cleared token, or token mismatch.
func (b *MemoryBroker) fenced(jobID, token string) *memJob {
	q, ok := b.index[jobID]
	if !ok {
		return nil
	}
	m, ok := b.queues[q].live[jobID]
	if !ok || token == "" || m.token != token {
		return nil
	}
	return m
}

func (b *MemoryBroker) Extend(_ context.Context, jobID, token string, lease time.Duration) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	m := b.fenced(jobID, token)
	if m == nil {
		return ErrLeaseLost
	}
	m.claimedUntil = b.clk.Now().Add(lease)
	return nil
}

func (b *MemoryBroker) Ack(_ context.Context, jobID, token string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	m := b.fenced(jobID, token)
	if m == nil {
		return ErrLeaseLost
	}
	delete(b.queues[b.index[jobID]].live, jobID)
	delete(b.index, jobID)
	return nil
}

func (b *MemoryBroker) Nack(_ context.Context, jobID, token string, retryAt time.Time, reason string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	m := b.fenced(jobID, token)
	if m == nil {
		return ErrLeaseLost
	}
	m.job.RunAt = retryAt
	m.job.LastError = reason
	m.claimedUntil = time.Time{}
	m.token = ""
	return nil
}

func (b *MemoryBroker) Kill(_ context.Context, jobID, token string, reason string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	m := b.fenced(jobID, token)
	if m == nil {
		return ErrLeaseLost
	}
	mq := b.queues[b.index[jobID]]
	delete(mq.live, jobID)
	m.job.LastError = reason
	m.claimedUntil = time.Time{}
	m.token = ""
	m.diedAt = b.clk.Now()
	mq.dead[jobID] = m
	return nil
}

func (b *MemoryBroker) ListDead(_ context.Context, queueName string, limit int) ([]Job, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	mq, ok := b.queues[queueName]
	if !ok {
		return nil, nil
	}
	dead := make([]*memJob, 0, len(mq.dead))
	for _, m := range mq.dead {
		dead = append(dead, m)
	}
	slices.SortFunc(dead, func(a, c *memJob) int {
		if r := a.diedAt.Compare(c.diedAt); r != 0 {
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

func (b *MemoryBroker) Requeue(_ context.Context, jobID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	q, ok := b.index[jobID]
	if !ok {
		return ErrJobNotFound
	}
	mq := b.queues[q]
	m, ok := mq.dead[jobID]
	if !ok {
		return ErrNotDead
	}
	delete(mq.dead, jobID)
	m.job.Attempt = 0
	m.job.RunAt = b.clk.Now()
	m.diedAt = time.Time{}
	mq.live[jobID] = m
	return nil
}

func (b *MemoryBroker) Purge(_ context.Context, jobID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	q, ok := b.index[jobID]
	if !ok {
		return ErrJobNotFound
	}
	mq := b.queues[q]
	if _, ok := mq.dead[jobID]; !ok {
		return ErrNotDead
	}
	delete(mq.dead, jobID)
	delete(b.index, jobID)
	return nil
}

func (b *MemoryBroker) PurgeDeadBefore(_ context.Context, cutoff time.Time) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, mq := range b.queues {
		for jobID, m := range mq.dead {
			if m.diedAt.Before(cutoff) {
				delete(mq.dead, jobID)
				delete(b.index, jobID)
				n++
			}
		}
	}
	return n, nil
}

func (b *MemoryBroker) Stats(_ context.Context) (Stats, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := make(Stats)
	for q, mq := range b.queues {
		if len(mq.live) == 0 && len(mq.dead) == 0 {
			continue
		}
		st[q] = QueueStats{Pending: len(mq.live), Dead: len(mq.dead)}
	}
	return st, nil
}
```

- [ ] **Step 5: Thread tokens through `service.go` (mechanical only)**

In `pollOnce`, the dispatch loop becomes (only the loop variable and process call change):

```go
		for _, cj := range jobs {
			sem <- struct{}{}
			wg.Go(func() {
				defer func() { <-sem }()
				s.process(opCtx, cj)
			})
		}
```

`process` signature and body: change to `func (s *Service) process(opCtx context.Context, cj ClaimedJob)`, add `job := cj.Job` as the first line (the rest of the body keeps using `job`), and thread the token into the four broker calls:

- heartbeat: `s.broker.Extend(hbCtx, job.ID, cj.Token, s.cfg.Lease)`
- ack paths: `s.broker.Ack(opCtx, job.ID, cj.Token)`
- kill paths (all three): `s.broker.Kill(opCtx, job.ID, cj.Token, ...)`
- retry path: `s.broker.Nack(opCtx, job.ID, cj.Token, retryAt, err.Error())`

No other engine behavior changes in this task.

- [ ] **Step 6: Switch client job IDs to UUIDv7**

In `async/queue/client.go` `buildJob`, change `ID: id.NewULID().String(),` to `ID: id.NewUUID().String(),`.

- [ ] **Step 7: Rewrite `brokertest/brokertest.go`**

Full file content:

```go
// Package brokertest is the executable contract for queue.Broker
// implementations. Every driver's test suite must call Run; the in-memory
// broker is the reference implementation. Timing subtests use short real
// leases (hundreds of ms) and poll for the expected outcome rather than
// asserting once after a fixed sleep, so the suite tolerates clock skew
// between the test process and a containerised database (e.g. a Docker VM
// clock that lags under load) and is safe for live backends.
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
	t.Run("BatchPush", func(t *testing.T) { testBatchPush(t, factory(t)) })
	t.Run("ClaimOrder", func(t *testing.T) { testClaimOrder(t, factory(t)) })
	t.Run("NackReschedules", func(t *testing.T) { testNackReschedules(t, factory(t)) })
	t.Run("DelayedJob", func(t *testing.T) { testDelayedJob(t, factory(t)) })
	t.Run("LeaseExpiryRedelivery", func(t *testing.T) { testLeaseExpiry(t, factory(t)) })
	t.Run("ExtendPreventsRedelivery", func(t *testing.T) { testExtend(t, factory(t)) })
	t.Run("Fencing", func(t *testing.T) { testFencing(t, factory(t)) })
	t.Run("DeadLetterOps", func(t *testing.T) { testDeadLetterOps(t, factory(t)) })
	t.Run("DeadOrderedByKillTime", func(t *testing.T) { testDeadOrder(t, factory(t)) })
	t.Run("PurgeDeadBefore", func(t *testing.T) { testPurgeDeadBefore(t, factory(t)) })
	t.Run("QueueIsolation", func(t *testing.T) { testQueueIsolation(t, factory(t)) })
	t.Run("Stats", func(t *testing.T) { testStats(t, factory(t)) })
	t.Run("ClaimEmptyQueue", func(t *testing.T) { testClaimEmpty(t, factory(t)) })
}

func makeJob(q string, runAt time.Time) queue.Job {
	return queue.Job{
		ID:          id.NewUUID().String(),
		Queue:       q,
		Type:        "test.kind",
		Payload:     []byte(`{"n":1}`),
		MaxAttempts: 25,
		RunAt:       runAt.UTC(),
		CreatedAt:   time.Now().UTC(),
	}
}

// dueNow returns a RunAt that is already in the past by a wide margin. RunAt is
// stamped from the test-process clock but visibility is decided by the database
// clock; biasing "claimable now" jobs into the past keeps them claimable even
// when the database clock lags the test process (e.g. a Docker VM under load).
func dueNow() time.Time { return time.Now().Add(-2 * time.Second) }

// claimWithin polls Claim until it returns at least want jobs or the deadline
// passes, tolerating clock skew and slow containers. It returns the claimed
// batch; on timeout it makes a final assertion so the failure names the queue.
func claimWithin(t *testing.T, b queue.Broker, q string, limit int, lease time.Duration, want int) []queue.ClaimedJob {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got, err := b.Claim(ctx, q, limit, lease)
		require.NoError(t, err)
		if len(got) >= want {
			return got
		}
		if time.Now().After(deadline) {
			require.Len(t, got, want, "queue %q: expected %d claimable job(s) within deadline", q, want)
			return got
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func claimIDs(jobs []queue.ClaimedJob) []string {
	ids := make([]string, len(jobs))
	for i, j := range jobs {
		ids[i] = j.ID
	}
	return ids
}

func testPushClaimAck(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j := makeJob("q1", dueNow())
	require.NoError(t, b.Push(ctx, j))

	got := claimWithin(t, b, "q1", 10, time.Minute, 1)
	assert.Equal(t, j.ID, got[0].ID)
	assert.Equal(t, j.Type, got[0].Type)
	assert.JSONEq(t, string(j.Payload), string(got[0].Payload))
	assert.Equal(t, 1, got[0].Attempt, "claim must increment attempt")
	assert.Equal(t, j.MaxAttempts, got[0].MaxAttempts)
	assert.NotEmpty(t, got[0].Token, "claim must stamp a fencing token")

	again, err := b.Claim(ctx, "q1", 10, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, again, "claimed job must be invisible during lease")

	require.NoError(t, b.Ack(ctx, j.ID, got[0].Token))
	st, err := b.Stats(ctx)
	require.NoError(t, err)
	assert.Zero(t, st["q1"].Pending)
	assert.Zero(t, st["q1"].Dead)

	require.NoError(t, b.Push(ctx), "empty batch push is a no-op")
}

func testBatchPush(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	now := time.Now()
	j1 := makeJob("q1", now.Add(-3*time.Second))
	j2 := makeJob("q1", now.Add(-2*time.Second))
	j3 := makeJob("q1", now.Add(-1*time.Second))
	require.NoError(t, b.Push(ctx, j1, j2, j3))

	got := claimWithin(t, b, "q1", 10, time.Minute, 3)
	assert.Equal(t, []string{j1.ID, j2.ID, j3.ID}, claimIDs(got), "batch push claims back in (run_at, id) order")
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
	j := makeJob("q1", dueNow())
	require.NoError(t, b.Push(ctx, j))

	got := claimWithin(t, b, "q1", 1, time.Minute, 1)
	require.Len(t, got, 1)

	require.NoError(t, b.Nack(ctx, j.ID, got[0].Token, time.Now().Add(250*time.Millisecond), "boom"))

	early, err := b.Claim(ctx, "q1", 1, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, early, "nacked job must stay invisible until retryAt")

	late := claimWithin(t, b, "q1", 1, time.Minute, 1)
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

	late := claimWithin(t, b, "q1", 1, time.Minute, 1)
	assert.Equal(t, j.ID, late[0].ID)
}

func testLeaseExpiry(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j := makeJob("q1", dueNow())
	require.NoError(t, b.Push(ctx, j))

	got := claimWithin(t, b, "q1", 1, 300*time.Millisecond, 1)
	require.Len(t, got, 1)

	early, err := b.Claim(ctx, "q1", 1, 300*time.Millisecond)
	require.NoError(t, err)
	assert.Empty(t, early)

	// Reclaim with the SAME lease: some drivers (redis) enforce expiry at
	// claim time via min-idle = the claiming lease, so a longer lease here
	// would hide the expiry. Poll so a lagging database clock only slows the
	// redelivery rather than failing the assertion.
	late := claimWithin(t, b, "q1", 1, 300*time.Millisecond, 1)
	assert.Equal(t, j.ID, late[0].ID)
	assert.Equal(t, 2, late[0].Attempt)
}

func testExtend(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j := makeJob("q1", dueNow())
	require.NoError(t, b.Push(ctx, j))

	got := claimWithin(t, b, "q1", 1, 400*time.Millisecond, 1)
	require.Len(t, got, 1)

	time.Sleep(250 * time.Millisecond)
	require.NoError(t, b.Extend(ctx, j.ID, got[0].Token, 2*time.Second))

	time.Sleep(300 * time.Millisecond)                        // past the original lease, inside the extended one
	still, err := b.Claim(ctx, "q1", 1, 400*time.Millisecond) // same lease as the original claim (see LeaseExpiry note)
	require.NoError(t, err)
	assert.Empty(t, still, "extended lease must prevent redelivery")

	require.NoError(t, b.Ack(ctx, j.ID, got[0].Token))
}

func testFencing(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j := makeJob("q1", dueNow())
	require.NoError(t, b.Push(ctx, j))

	first := claimWithin(t, b, "q1", 1, 300*time.Millisecond, 1)
	require.Len(t, first, 1)
	stale := first[0].Token

	// Let the lease expire and reclaim: the second claim owns the job now.
	second := claimWithin(t, b, "q1", 1, 300*time.Millisecond, 1)
	require.Len(t, second, 1)
	require.Equal(t, j.ID, second[0].ID)

	// Every fenced op with the stale token must refuse and leave the new
	// claim's state alone.
	assert.ErrorIs(t, b.Extend(ctx, j.ID, stale, time.Minute), queue.ErrLeaseLost)
	assert.ErrorIs(t, b.Ack(ctx, j.ID, stale), queue.ErrLeaseLost)
	assert.ErrorIs(t, b.Nack(ctx, j.ID, stale, time.Now(), "stale"), queue.ErrLeaseLost)
	assert.ErrorIs(t, b.Kill(ctx, j.ID, stale, "stale"), queue.ErrLeaseLost)

	// The job is still claimed by the second owner: invisible, not dead.
	invisible, err := b.Claim(ctx, "q1", 1, 300*time.Millisecond)
	require.NoError(t, err)
	assert.Empty(t, invisible, "stale-token ops must not release or kill the current claim")
	dead, err := b.ListDead(ctx, "q1", 10)
	require.NoError(t, err)
	assert.Empty(t, dead)

	// Fenced ops on an unknown id are also ErrLeaseLost, never ErrJobNotFound.
	assert.ErrorIs(t, b.Ack(ctx, "no-such-id", stale), queue.ErrLeaseLost)
	assert.ErrorIs(t, b.Extend(ctx, "no-such-id", stale, time.Minute), queue.ErrLeaseLost)

	// The live token still works.
	require.NoError(t, b.Ack(ctx, j.ID, second[0].Token))
}

func testDeadLetterOps(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j1 := makeJob("q1", time.Now().Add(-2*time.Second))
	j2 := makeJob("q1", time.Now().Add(-time.Second))
	require.NoError(t, b.Push(ctx, j1, j2))

	got, err := b.Claim(ctx, "q1", 2, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.NoError(t, b.Kill(ctx, got[0].ID, got[0].Token, "poison"))
	require.NoError(t, b.Kill(ctx, got[1].ID, got[1].Token, "poison"))

	dead, err := b.ListDead(ctx, "q1", 10)
	require.NoError(t, err)
	require.Len(t, dead, 2)
	assert.Equal(t, "poison", dead[0].LastError)

	one, err := b.ListDead(ctx, "q1", 1)
	require.NoError(t, err)
	assert.Len(t, one, 1, "ListDead must honor limit")

	// Requeue resets attempts and makes the job claimable again.
	require.NoError(t, b.Requeue(ctx, j1.ID))
	re := claimWithin(t, b, "q1", 10, time.Minute, 1)
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

func testDeadOrder(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j1 := makeJob("q1", time.Now().Add(-2*time.Second))
	j2 := makeJob("q1", time.Now().Add(-time.Second))
	require.NoError(t, b.Push(ctx, j1, j2))

	got := claimWithin(t, b, "q1", 2, time.Minute, 2)
	require.Len(t, got, 2)
	byID := map[string]queue.ClaimedJob{got[0].ID: got[0], got[1].ID: got[1]}

	// Kill j2 first, j1 second, with a gap wide enough that kill timestamps
	// differ even at millisecond resolution.
	require.NoError(t, b.Kill(ctx, j2.ID, byID[j2.ID].Token, "first-death"))
	time.Sleep(100 * time.Millisecond)
	require.NoError(t, b.Kill(ctx, j1.ID, byID[j1.ID].Token, "second-death"))

	dead, err := b.ListDead(ctx, "q1", 10)
	require.NoError(t, err)
	require.Len(t, dead, 2)
	assert.Equal(t, j2.ID, dead[0].ID, "ListDead orders by kill time, not creation or run order")
	assert.Equal(t, j1.ID, dead[1].ID)
}

func testPurgeDeadBefore(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j := makeJob("q1", dueNow())
	require.NoError(t, b.Push(ctx, j))
	got := claimWithin(t, b, "q1", 1, time.Minute, 1)
	require.NoError(t, b.Kill(ctx, j.ID, got[0].Token, "x"))

	// A cutoff far in the past removes nothing (skew-proof bounds: even a
	// lagging database clock is within ±1h of the test process).
	n, err := b.PurgeDeadBefore(ctx, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	assert.Zero(t, n)
	dead, err := b.ListDead(ctx, "q1", 10)
	require.NoError(t, err)
	require.Len(t, dead, 1)

	// A cutoff far in the future removes the dead job and reports the count.
	n, err = b.PurgeDeadBefore(ctx, time.Now().Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	dead, err = b.ListDead(ctx, "q1", 10)
	require.NoError(t, err)
	assert.Empty(t, dead)
}

func testQueueIsolation(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j1 := makeJob("q1", dueNow())
	j2 := makeJob("q2", dueNow())
	require.NoError(t, b.Push(ctx, j1))
	require.NoError(t, b.Push(ctx, j2))

	got := claimWithin(t, b, "q1", 10, time.Minute, 1)
	assert.Equal(t, j1.ID, got[0].ID)
}

func testStats(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j1 := makeJob("q1", dueNow())
	j2 := makeJob("q1", dueNow())
	require.NoError(t, b.Push(ctx, j1, j2))

	got := claimWithin(t, b, "q1", 1, time.Minute, 1)
	require.NoError(t, b.Kill(ctx, got[0].ID, got[0].Token, "x"))

	st, err := b.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, st["q1"].Pending)
	assert.Equal(t, 1, st["q1"].Dead)
	assert.False(t, st["q1"].PendingCapped, "counts this small are never capped")
	assert.False(t, st["q1"].DeadCapped)
}

func testClaimEmpty(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	got, err := b.Claim(ctx, "nothing-here", 10, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, got)
}
```

- [ ] **Step 8: Thread tokens through `client_test.go` and `bench_test.go`**

`client_test.go` — three mechanical edits:

1. In `TestPush_DelayAndRunAt`: `require.NoError(t, b.Ack(ctx, got[0].ID))` → `require.NoError(t, b.Ack(ctx, got[0].ID, got[0].Token))`
2. In `TestClient_DLQPassthrough`: both `b.Kill(ctx, got[0].ID, "poison")` and `b.Kill(ctx, reclaimed[0].ID, "again")` gain the claimed token: `b.Kill(ctx, got[0].ID, got[0].Token, "poison")` / `b.Kill(ctx, reclaimed[0].ID, reclaimed[0].Token, "again")`
3. The `txBroker` fake becomes variadic:

```go
func (b *txBroker) PushTx(_ context.Context, tx any, jobs ...queue.Job) error {
	b.gotTx, b.gotJob = tx, jobs[0]
	return nil
}
```

`bench_test.go` — in `BenchmarkClaimBatch_Memory` the recycle loop becomes:

```go
		for _, j := range jobs {
			if err := broker.Nack(ctx, j.ID, j.Token, time.Now(), ""); err != nil { // recycle for the next iteration
				b.Fatal(err)
			}
		}
```

- [ ] **Step 9: Build and run the unit tier (scoped)**

Run: `go build ./async/queue ./async/queue/brokertest && go test ./async/queue/ && just fmt ./async/queue/...`
Expected: PASS. (`./async/queue/postgres` and `./async/queue/redis` intentionally do not compile yet.)

- [ ] **Step 10: Commit**

```bash
git add async/queue
git commit -m "feat(queue)!: broker v2 contract with lease fencing, batch push, kill-time DLQ ordering

Claim returns ClaimedJob with a per-claim UUIDv7 fencing token; Extend/Ack/Nack/Kill are token-fenced and return ErrLeaseLost on lost ownership. Push is a variadic atomic batch. ListDead orders by kill time. New PurgeDeadBefore and optional Maintainer capability. MemoryBroker reworked to per-queue buckets; brokertest gains fencing, batch, dead-order, and retention conformance. pg/redis drivers rework in follow-up commits."
```

---

### Task 2: Postgres — two-table schema and driver rework

**Files:**
- Rewrite: `async/queue/postgres/migrations/20260715120000_queue_jobs.sql`
- Rewrite: `async/queue/postgres/pgqueue.go`
- Modify: `async/queue/postgres/pgqueue_test.go`, `async/queue/postgres/bench_test.go`
- Unchanged: `async/queue/postgres/validate_test.go` (nil-pool unit check still compiles)

**Interfaces:**
- Consumes: `queue.Broker` v2, `queue.ClaimedJob`, `queue.ErrLeaseLost`, `queue.ErrNotDead`, `queue.ErrJobNotFound`, `core/id.NewUUID`.
- Produces: `pgqueue.Broker` implementing `queue.Broker` + `queue.TxPusher`; dead table derived as `<table>_dead`; stats cap constant `statsCap = 10000`.

- [ ] **Step 1: Rewrite the migration**

Full content of `async/queue/postgres/migrations/20260715120000_queue_jobs.sql`:

```sql
-- +goose Up
CREATE TABLE queue_jobs (
    id            uuid PRIMARY KEY,
    claim_token   uuid,
    run_at        timestamptz NOT NULL,
    claimed_until timestamptz,
    created_at    timestamptz NOT NULL,
    attempt       integer NOT NULL DEFAULT 0,
    max_attempts  integer NOT NULL DEFAULT 0,
    queue         text NOT NULL,
    type          text NOT NULL,
    scope         text NOT NULL DEFAULT '',
    last_error    text NOT NULL DEFAULT '',
    payload       json NOT NULL
) WITH (fillfactor = 90, autovacuum_vacuum_scale_factor = 0.02, autovacuum_analyze_scale_factor = 0.02);

CREATE INDEX queue_jobs_claim_idx ON queue_jobs (queue, run_at);

CREATE TABLE queue_jobs_dead (
    id           uuid PRIMARY KEY,
    run_at       timestamptz NOT NULL,
    created_at   timestamptz NOT NULL,
    died_at      timestamptz NOT NULL,
    attempt      integer NOT NULL,
    max_attempts integer NOT NULL,
    queue        text NOT NULL,
    type         text NOT NULL,
    scope        text NOT NULL DEFAULT '',
    last_error   text NOT NULL DEFAULT '',
    payload      json NOT NULL
);

CREATE INDEX queue_jobs_dead_list_idx ON queue_jobs_dead (queue, died_at);
CREATE INDEX queue_jobs_dead_sweep_idx ON queue_jobs_dead (died_at);

-- +goose Down
DROP TABLE queue_jobs_dead;
DROP TABLE queue_jobs;
```

- [ ] **Step 2: Rewrite `pgqueue.go`**

Full file content:

```go
// Package pgqueue is the Postgres queue.Broker: SKIP LOCKED claiming with
// fencing tokens over a hot jobs table, a separate cold dead-letter table,
// bounded stats, and transactional enqueue (queue.TxPusher).
//
// Requires PostgreSQL >= 18 (distinct-queue enumeration in Stats leans on
// B-tree skip scan; earlier servers work but Stats may plan a seq scan).
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
	"github.com/dmitrymomot/forge/core/id"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations holds the goose migration creating queue_jobs and
// queue_jobs_dead, rooted so its .sql files sit at fsys root. Apply via
// data/migration under its own version table.
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(err) // unreachable: migrations/*.sql is embedded at compile time
	}
	Migrations = sub
}

var tableNameRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// statsCap bounds per-queue stats counting: counts are exact up to the cap;
// beyond it QueueStats reports the cap with the Capped flag set. Keeps Stats
// O(cap) via bounded index-only scans instead of O(table).
const statsCap = 10000

// Broker is the Postgres queue.Broker and queue.TxPusher.
type Broker struct {
	pool      *pgxpool.Pool
	table     string
	deadTable string

	insertSQL       string
	claimSQL        string
	extendSQL       string
	ackSQL          string
	nackSQL         string
	killSQL         string
	deadSQL         string
	requeueSQL      string
	purgeSQL        string
	purgeBeforeSQL  string
	existsSQL       string
	statsPendingSQL string
	statsDeadSQL    string
}

// Option configures New.
type Option func(*Broker)

// WithTable overrides the hot table name (default "queue_jobs"); the
// dead-letter table is always derived as "<table>_dead". The shipped migration
// creates the default pair; custom names require a caller-managed schema of
// the same shape.
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
	b.deadTable = b.table + "_dead"

	const liveCols = "id, queue, type, payload, scope, attempt, max_attempts, run_at, created_at, last_error"
	b.insertSQL = fmt.Sprintf(`INSERT INTO %s (id, queue, type, payload, scope, attempt, max_attempts, run_at, created_at)
SELECT u.id, u.queue, u.type, u.payload::json, u.scope, u.attempt, u.max_attempts, u.run_at, u.created_at
FROM unnest($1::uuid[], $2::text[], $3::text[], $4::text[], $5::text[], $6::int[], $7::int[], $8::timestamptz[], $9::timestamptz[])
AS u(id, queue, type, payload, scope, attempt, max_attempts, run_at, created_at)`, b.table)
	b.claimSQL = fmt.Sprintf(`WITH picked AS (
SELECT id FROM %[1]s
WHERE queue = $1 AND run_at <= now() AND (claimed_until IS NULL OR claimed_until < now())
ORDER BY run_at, id LIMIT $2
FOR UPDATE SKIP LOCKED
), claimed AS (
UPDATE %[1]s j SET claimed_until = now() + $3, claim_token = $4, attempt = j.attempt + 1
FROM picked WHERE j.id = picked.id
RETURNING j.id, j.queue, j.type, j.payload, j.scope, j.attempt, j.max_attempts, j.run_at, j.created_at, j.last_error
)
SELECT `+liveCols+` FROM claimed ORDER BY run_at, id`, b.table)
	b.extendSQL = fmt.Sprintf("UPDATE %s SET claimed_until = now() + $3 WHERE id = $1 AND claim_token = $2", b.table)
	b.ackSQL = fmt.Sprintf("DELETE FROM %s WHERE id = $1 AND claim_token = $2", b.table)
	b.nackSQL = fmt.Sprintf("UPDATE %s SET run_at = $3, claimed_until = NULL, claim_token = NULL, last_error = $4 WHERE id = $1 AND claim_token = $2", b.table)
	b.killSQL = fmt.Sprintf(`WITH d AS (
DELETE FROM %[1]s WHERE id = $1 AND claim_token = $2
RETURNING id, queue, type, payload, scope, attempt, max_attempts, run_at, created_at
)
INSERT INTO %[2]s (id, queue, type, payload, scope, attempt, max_attempts, run_at, created_at, died_at, last_error)
SELECT id, queue, type, payload, scope, attempt, max_attempts, run_at, created_at, now(), $3 FROM d`, b.table, b.deadTable)
	b.deadSQL = fmt.Sprintf("SELECT "+liveCols+" FROM %s WHERE queue = $1 ORDER BY died_at, id LIMIT $2", b.deadTable)
	b.requeueSQL = fmt.Sprintf(`WITH d AS (
DELETE FROM %[2]s WHERE id = $1
RETURNING id, queue, type, payload, scope, max_attempts, created_at, last_error
)
INSERT INTO %[1]s (id, queue, type, payload, scope, attempt, max_attempts, run_at, created_at, last_error)
SELECT id, queue, type, payload, scope, 0, max_attempts, now(), created_at, last_error FROM d`, b.table, b.deadTable)
	b.purgeSQL = fmt.Sprintf("DELETE FROM %s WHERE id = $1", b.deadTable)
	b.purgeBeforeSQL = fmt.Sprintf("DELETE FROM %s WHERE died_at < $1", b.deadTable)
	b.existsSQL = fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM %s WHERE id = $1)", b.table)
	b.statsPendingSQL = fmt.Sprintf(`SELECT q.queue, c.n FROM (SELECT DISTINCT queue FROM %[1]s) q
CROSS JOIN LATERAL (SELECT count(*) AS n FROM (SELECT 1 FROM %[1]s j WHERE j.queue = q.queue LIMIT %[2]d) t) c`, b.table, statsCap+1)
	b.statsDeadSQL = fmt.Sprintf(`SELECT q.queue, c.n FROM (SELECT DISTINCT queue FROM %[1]s) q
CROSS JOIN LATERAL (SELECT count(*) AS n FROM (SELECT 1 FROM %[1]s j WHERE j.queue = q.queue LIMIT %[2]d) t) c`, b.deadTable, statsCap+1)
	return b, nil
}

// pushArgs flattens jobs into the parallel arrays the unnest insert binds.
func pushArgs(jobs []queue.Job) []any {
	n := len(jobs)
	ids := make([]string, n)
	queues := make([]string, n)
	types := make([]string, n)
	payloads := make([]string, n)
	scopes := make([]string, n)
	attempts := make([]int32, n)
	maxAttempts := make([]int32, n)
	runAts := make([]time.Time, n)
	createdAts := make([]time.Time, n)
	for i, j := range jobs {
		ids[i] = j.ID
		queues[i] = j.Queue
		types[i] = j.Type
		payloads[i] = string(j.Payload)
		scopes[i] = j.Scope
		attempts[i] = int32(j.Attempt)
		maxAttempts[i] = int32(j.MaxAttempts)
		runAts[i] = j.RunAt
		createdAts[i] = j.CreatedAt
	}
	return []any{ids, queues, types, payloads, scopes, attempts, maxAttempts, runAts, createdAts}
}

func (b *Broker) Push(ctx context.Context, jobs ...queue.Job) error {
	if len(jobs) == 0 {
		return nil
	}
	if _, err := b.pool.Exec(ctx, b.insertSQL, pushArgs(jobs)...); err != nil {
		return fmt.Errorf("pgqueue: push: %w", err)
	}
	return nil
}

// PushTx implements queue.TxPusher: the same batch insert inside a
// caller-owned pgx.Tx, so the jobs commit or roll back with the business
// transaction.
func (b *Broker) PushTx(ctx context.Context, tx any, jobs ...queue.Job) error {
	pgtx, ok := tx.(pgx.Tx)
	if !ok {
		return fmt.Errorf("pgqueue: push tx: expected pgx.Tx, got %T", tx)
	}
	if len(jobs) == 0 {
		return nil
	}
	if _, err := pgtx.Exec(ctx, b.insertSQL, pushArgs(jobs)...); err != nil {
		return fmt.Errorf("pgqueue: push tx: %w", err)
	}
	return nil
}

func (b *Broker) Claim(ctx context.Context, queueName string, n int, lease time.Duration) ([]queue.ClaimedJob, error) {
	token := id.NewUUID().String()
	rows, err := b.pool.Query(ctx, b.claimSQL, queueName, n, lease, token)
	if err != nil {
		return nil, fmt.Errorf("pgqueue: claim: %w", err)
	}
	defer rows.Close()
	var jobs []queue.ClaimedJob
	for rows.Next() {
		var j queue.Job
		if err := rows.Scan(&j.ID, &j.Queue, &j.Type, &j.Payload, &j.Scope, &j.Attempt, &j.MaxAttempts, &j.RunAt, &j.CreatedAt, &j.LastError); err != nil {
			return nil, fmt.Errorf("pgqueue: claim scan: %w", err)
		}
		jobs = append(jobs, queue.ClaimedJob{Job: j, Token: token})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgqueue: claim rows: %w", err)
	}
	return jobs, nil
}

// fencedExec runs a token-guarded statement; zero affected rows means the
// token no longer owns the job.
func (b *Broker) fencedExec(ctx context.Context, op, sql string, args ...any) error {
	tag, err := b.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("pgqueue: %s: %w", op, err)
	}
	if tag.RowsAffected() == 0 {
		return queue.ErrLeaseLost
	}
	return nil
}

func (b *Broker) Extend(ctx context.Context, jobID, token string, lease time.Duration) error {
	return b.fencedExec(ctx, "extend", b.extendSQL, jobID, token, lease)
}

func (b *Broker) Ack(ctx context.Context, jobID, token string) error {
	return b.fencedExec(ctx, "ack", b.ackSQL, jobID, token)
}

func (b *Broker) Nack(ctx context.Context, jobID, token string, retryAt time.Time, reason string) error {
	return b.fencedExec(ctx, "nack", b.nackSQL, jobID, token, retryAt, reason)
}

func (b *Broker) Kill(ctx context.Context, jobID, token string, reason string) error {
	return b.fencedExec(ctx, "kill", b.killSQL, jobID, token, reason)
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

func (b *Broker) Requeue(ctx context.Context, jobID string) error {
	tag, err := b.pool.Exec(ctx, b.requeueSQL, jobID)
	if err != nil {
		return fmt.Errorf("pgqueue: requeue: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return b.notDeadOrMissing(ctx, jobID)
	}
	return nil
}

func (b *Broker) Purge(ctx context.Context, jobID string) error {
	tag, err := b.pool.Exec(ctx, b.purgeSQL, jobID)
	if err != nil {
		return fmt.Errorf("pgqueue: purge: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return b.notDeadOrMissing(ctx, jobID)
	}
	return nil
}

func (b *Broker) PurgeDeadBefore(ctx context.Context, cutoff time.Time) (int, error) {
	tag, err := b.pool.Exec(ctx, b.purgeBeforeSQL, cutoff)
	if err != nil {
		return 0, fmt.Errorf("pgqueue: purge dead before: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func (b *Broker) notDeadOrMissing(ctx context.Context, jobID string) error {
	var exists bool
	if err := b.pool.QueryRow(ctx, b.existsSQL, jobID).Scan(&exists); err != nil {
		return fmt.Errorf("pgqueue: exists: %w", err)
	}
	if exists {
		return queue.ErrNotDead
	}
	return queue.ErrJobNotFound
}

func (b *Broker) Stats(ctx context.Context) (queue.Stats, error) {
	st := make(queue.Stats)
	if err := b.statsInto(ctx, b.statsPendingSQL, st, false); err != nil {
		return nil, err
	}
	if err := b.statsInto(ctx, b.statsDeadSQL, st, true); err != nil {
		return nil, err
	}
	return st, nil
}

// statsInto merges one bounded count query into st. Counts run with LIMIT
// statsCap+1: a full statsCap+1 result means "more than the cap", reported as
// the cap with the Capped flag set.
func (b *Broker) statsInto(ctx context.Context, sql string, st queue.Stats, dead bool) error {
	rows, err := b.pool.Query(ctx, sql)
	if err != nil {
		return fmt.Errorf("pgqueue: stats: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var q string
		var n int64
		if err := rows.Scan(&q, &n); err != nil {
			return fmt.Errorf("pgqueue: stats scan: %w", err)
		}
		capped := n > statsCap
		if capped {
			n = statsCap
		}
		qs := st[q]
		if dead {
			qs.Dead, qs.DeadCapped = int(n), capped
		} else {
			qs.Pending, qs.PendingCapped = int(n), capped
		}
		st[q] = qs
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("pgqueue: stats rows: %w", err)
	}
	return nil
}
```

- [ ] **Step 3: Update `pgqueue_test.go`**

Edits to the existing file:

1. `newBroker`: truncate both tables — `"TRUNCATE queue_jobs"` → `"TRUNCATE queue_jobs, queue_jobs_dead"`.
2. `claimOne` return type: `queue.Job` → `queue.ClaimedJob` (its body already returns `got[0]`; change the zero return to `queue.ClaimedJob{}`).
3. In `TestPgQueue_PushTx` "commit makes the job claimable": `b.Ack(ctx, job.ID)` → `b.Ack(ctx, job.ID, job.Token)`.
4. Rename `TestPgQueue_PayloadIsJSONB` → `TestPgQueue_PayloadRoundTripsJSON` (column is `json` now; the assertion body is unchanged).
5. Append the capped-stats test:

```go
func TestPgQueue_StatsCapped(t *testing.T) {
	pool := openPool(t)
	b := newBroker(t, pool)
	ctx := context.Background()

	jobs := make([]queue.Job, 10001)
	for i := range jobs {
		jobs[i] = queue.Job{
			ID: id.NewUUID().String(), Queue: "bulk", Type: "cap.kind",
			Payload: []byte(`{}`), RunAt: time.Now().UTC(), CreatedAt: time.Now().UTC(),
		}
	}
	require.NoError(t, b.Push(ctx, jobs...))

	st, err := b.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 10000, st["bulk"].Pending, "count reports the cap, not the true size")
	assert.True(t, st["bulk"].PendingCapped)
	assert.False(t, st["bulk"].DeadCapped)
}
```

Add `"github.com/dmitrymomot/forge/core/id"` to the test file imports.

- [ ] **Step 4: Thread the pg benchmark**

In `postgres/bench_test.go`, `b.Ack(ctx, jobs[0].ID)` → `b.Ack(ctx, jobs[0].ID, jobs[0].Token)`. (Task 10 rewrites this file fully; this keeps it compiling.)

- [ ] **Step 5: Run pg integration tier**

Run: `just fmt ./async/queue/postgres/... && go build ./async/queue/postgres && just test-integration ./async/queue/postgres/`
Expected: PASS including the new Fencing/BatchPush/DeadOrderedByKillTime/PurgeDeadBefore conformance subtests and `TestPgQueue_StatsCapped`.

- [ ] **Step 6: Commit**

```bash
git add async/queue/postgres
git commit -m "feat(pgqueue)!: two-table schema with fencing tokens, batch insert, bounded stats

Hot queue_jobs (uuid v7 PK, HOT-friendly un-indexed lease columns, fillfactor 90, tuned autovacuum) + cold queue_jobs_dead moved to by CTE row-moves. Claim wraps SKIP LOCKED in a CTE with an outer ORDER BY (RETURNING order is not guaranteed). unnest batch push, died_at-ordered ListDead, PurgeDeadBefore sweep, stats via DISTINCT+LATERAL bounded counts capped at 10k. Requires PostgreSQL >= 18."
```

---

### Task 3: Redis — fencing Lua, dead ZSET, atomic Requeue/Purge

**Files:**
- Modify: `async/queue/redis/redisqueue.go` (claimedRef token, scripts, dead storage, PurgeDeadBefore)
- Modify: `async/queue/redis/redisqueue_test.go` (token threading), `async/queue/redis/bench_test.go` (token threading)

**Interfaces:**
- Consumes: `queue.Broker` v2, `queue.ErrLeaseLost`, `core/id.NewUUID`.
- Produces: key layout additions — `<prefix><q>:dead:idx` (ZSET, score = kill-time ms). `deadIdxKey(q string) string` helper. Lua scripts `ackScript`, `nackScript`, `killScript`, `extendScript`, `requeueScript`, `purgeScript`, `purgeDeadBeforeScript`.

- [ ] **Step 1: Token in claimedRef + fenced take**

In `redisqueue.go`: add `token string` to `claimedRef`; add key helper `func (b *Broker) deadIdxKey(q string) string { return b.prefix + q + ":dead:idx" }`.

In `Claim`, generate one token per call before the XAUTOCLAIM section: `token := id.NewUUID().String()`; both remember sites become `b.remember(claimedJob.ID, claimedRef{job: claimedJob.Job, msgID: m.ID, queue: q, token: token})` and both `out = append(...)` sites build `queue.ClaimedJob{Job: claimedJob.Job, Token: token}` (adjust local variable: `claimedJob` stays a `queue.Job` with the bumped Attempt). Claim's signature: `func (b *Broker) Claim(ctx context.Context, q string, n int, lease time.Duration) ([]queue.ClaimedJob, error)` with `var out []queue.ClaimedJob`.

Replace `take` with a fenced variant — crucial detail: a token mismatch means the entry belongs to a NEWER claim on this instance, so it must NOT be deleted:

```go
// take removes and returns the claimed ref for id IF token owns it. A
// mismatched token means the ref belongs to a newer claim on this instance —
// left untouched, the caller gets ErrLeaseLost.
func (b *Broker) take(id, token string) (claimedRef, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ref, ok := b.claimed[id]
	if !ok || ref.token != token {
		return claimedRef{}, false
	}
	delete(b.claimed, id)
	return ref, true
}
```

- [ ] **Step 2: Ownership-checked Lua finalize scripts**

Add the scripts at package level (next to `promoteScript`). Every script verifies PEL ownership first: `XPENDING <stream> <group> <msgID> <msgID> 1` returns `[[id, consumer, idle, deliveries]]`; an empty reply or a foreign consumer means the lease was lost.

```go
// Finalize scripts verify PEL ownership atomically before mutating: XPENDING
// for the exact message must name this consumer, otherwise the message was
// autoclaimed by another worker (or already finalized) and the op returns 0 →
// ErrLeaseLost. Plain XACK would succeed regardless of owner.
var ackScript = redis.NewScript(`
local p = redis.call('XPENDING', KEYS[1], ARGV[1], ARGV[3], ARGV[3], 1)
if #p == 0 or p[1][2] ~= ARGV[2] then return 0 end
redis.call('XACK', KEYS[1], ARGV[1], ARGV[3])
redis.call('XDEL', KEYS[1], ARGV[3])
redis.call('HDEL', KEYS[2], ARGV[4])
return 1
`)

var nackScript = redis.NewScript(`
local p = redis.call('XPENDING', KEYS[1], ARGV[1], ARGV[3], ARGV[3], 1)
if #p == 0 or p[1][2] ~= ARGV[2] then return 0 end
redis.call('ZADD', KEYS[2], ARGV[5], ARGV[4])
redis.call('HSET', KEYS[3], ARGV[4], ARGV[6])
redis.call('XACK', KEYS[1], ARGV[1], ARGV[3])
redis.call('XDEL', KEYS[1], ARGV[3])
return 1
`)

var killScript = redis.NewScript(`
local p = redis.call('XPENDING', KEYS[1], ARGV[1], ARGV[3], ARGV[3], 1)
if #p == 0 or p[1][2] ~= ARGV[2] then return 0 end
redis.call('HSET', KEYS[2], ARGV[4], ARGV[5])
redis.call('ZADD', KEYS[3], ARGV[6], ARGV[4])
redis.call('XACK', KEYS[1], ARGV[1], ARGV[3])
redis.call('XDEL', KEYS[1], ARGV[3])
return 1
`)

var extendScript = redis.NewScript(`
local p = redis.call('XPENDING', KEYS[1], ARGV[1], ARGV[3], ARGV[3], 1)
if #p == 0 or p[1][2] ~= ARGV[2] then return 0 end
redis.call('XCLAIM', KEYS[1], ARGV[1], ARGV[2], 0, ARGV[3], 'JUSTID')
return 1
`)

// requeueScript atomically moves a dead job back to the stream. HDEL-as-test:
// only the caller that actually removes the dead entry re-adds the job, so a
// concurrent double-requeue cannot duplicate it.
var requeueScript = redis.NewScript(`
if redis.call('HDEL', KEYS[1], ARGV[1]) == 0 then return 0 end
redis.call('ZREM', KEYS[2], ARGV[1])
redis.call('XADD', KEYS[3], '*', 'j', ARGV[2])
return 1
`)

var purgeScript = redis.NewScript(`
if redis.call('HDEL', KEYS[1], ARGV[1]) == 0 then return 0 end
redis.call('ZREM', KEYS[2], ARGV[1])
redis.call('HDEL', KEYS[3], ARGV[1])
return 1
`)

var purgeDeadBeforeScript = redis.NewScript(`
local ids = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])
for i = 1, #ids do
  redis.call('ZREM', KEYS[1], ids[i])
  redis.call('HDEL', KEYS[2], ids[i])
  redis.call('HDEL', KEYS[3], ids[i])
end
return #ids
`)
```

- [ ] **Step 3: Rewrite the finalize methods**

```go
func (b *Broker) Extend(ctx context.Context, jobID, token string, _ time.Duration) error {
	b.mu.Lock()
	ref, ok := b.claimed[jobID]
	b.mu.Unlock()
	if !ok || ref.token != token {
		return queue.ErrLeaseLost
	}
	// XCLAIM JUSTID (to ourselves, inside the ownership check) resets the idle
	// clock without bumping the delivery counter. Lease expiry is idle-based
	// here: the next Claim's MinIdle is the lease.
	n, err := extendScript.Run(ctx, b.client, []string{b.streamKey(ref.queue)}, group, b.consumer, ref.msgID).Int()
	if err != nil {
		return fmt.Errorf("redisqueue: extend: %w", err)
	}
	if n == 0 {
		b.mu.Lock()
		delete(b.claimed, jobID) // stale ref: message moved to another consumer
		b.mu.Unlock()
		return queue.ErrLeaseLost
	}
	return nil
}

func (b *Broker) Ack(ctx context.Context, jobID, token string) error {
	ref, ok := b.take(jobID, token)
	if !ok {
		return queue.ErrLeaseLost
	}
	n, err := ackScript.Run(ctx, b.client,
		[]string{b.streamKey(ref.queue), b.indexKey()},
		group, b.consumer, ref.msgID, jobID).Int()
	if err != nil {
		return fmt.Errorf("redisqueue: ack: %w", err)
	}
	if n == 0 {
		return queue.ErrLeaseLost
	}
	return nil
}

func (b *Broker) Nack(ctx context.Context, jobID, token string, retryAt time.Time, reason string) error {
	ref, ok := b.take(jobID, token)
	if !ok {
		return queue.ErrLeaseLost
	}
	j := ref.job
	// ref.job.Attempt already reflects the attempt the engine just consumed
	// (including crash redeliveries via XAUTOCLAIM), so persist it as-is.
	j.LastError = reason
	j.RunAt = retryAt.UTC()
	enc, err := queue.EncodeJob(j)
	if err != nil {
		return err
	}
	n, err := nackScript.Run(ctx, b.client,
		[]string{b.streamKey(ref.queue), b.delayedKey(ref.queue), b.dataKey(ref.queue)},
		group, b.consumer, ref.msgID, jobID, retryAt.UnixMilli(), enc).Int()
	if err != nil {
		return fmt.Errorf("redisqueue: nack: %w", err)
	}
	if n == 0 {
		return queue.ErrLeaseLost
	}
	return nil
}

func (b *Broker) Kill(ctx context.Context, jobID, token string, reason string) error {
	ref, ok := b.take(jobID, token)
	if !ok {
		return queue.ErrLeaseLost
	}
	j := ref.job // Attempt already reflects the consumed attempt (incl. crash redeliveries)
	j.LastError = reason
	enc, err := queue.EncodeJob(j)
	if err != nil {
		return err
	}
	n, err := killScript.Run(ctx, b.client,
		[]string{b.streamKey(ref.queue), b.deadKey(ref.queue), b.deadIdxKey(ref.queue)},
		group, b.consumer, ref.msgID, jobID, enc, time.Now().UnixMilli()).Int()
	if err != nil {
		return fmt.Errorf("redisqueue: kill: %w", err)
	}
	if n == 0 {
		return queue.ErrLeaseLost
	}
	return nil
}
```

- [ ] **Step 4: ZSET-backed ListDead, PurgeDeadBefore, script-based Requeue/Purge**

Replace `ListDead`, `Requeue`, `Purge` and delete the now-unused `sortDead` helper (and the `cmp`/`slices` imports if nothing else uses them):

```go
func (b *Broker) ListDead(ctx context.Context, q string, limit int) ([]queue.Job, error) {
	if limit <= 0 {
		return nil, nil
	}
	// The idx ZSET is scored by kill-time ms with lexicographic id tiebreak,
	// so a range read IS the ListDead order; O(limit), never O(DLQ).
	ids, err := b.client.ZRange(ctx, b.deadIdxKey(q), 0, int64(limit-1)).Result()
	if err != nil {
		return nil, fmt.Errorf("redisqueue: list dead range: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	encs, err := b.client.HMGet(ctx, b.deadKey(q), ids...).Result()
	if err != nil {
		return nil, fmt.Errorf("redisqueue: list dead fetch: %w", err)
	}
	jobs := make([]queue.Job, 0, len(encs))
	for _, enc := range encs {
		s, ok := enc.(string)
		if !ok {
			continue // purged between ZRANGE and HMGET
		}
		j, err := queue.DecodeJob([]byte(s))
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
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
	n, err := requeueScript.Run(ctx, b.client,
		[]string{b.deadKey(q), b.deadIdxKey(q), b.streamKey(q)}, jobID, fresh).Int()
	if err != nil {
		return fmt.Errorf("redisqueue: requeue: %w", err)
	}
	if n == 0 {
		return queue.ErrNotDead // lost a concurrent requeue/purge race
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
	n, err := purgeScript.Run(ctx, b.client,
		[]string{b.deadKey(q), b.deadIdxKey(q), b.indexKey()}, jobID).Int()
	if err != nil {
		return fmt.Errorf("redisqueue: purge: %w", err)
	}
	if n == 0 {
		return queue.ErrNotDead
	}
	return nil
}

func (b *Broker) PurgeDeadBefore(ctx context.Context, cutoff time.Time) (int, error) {
	queues, err := b.client.SMembers(ctx, b.queuesKey()).Result()
	if err != nil {
		return 0, fmt.Errorf("redisqueue: purge dead before: %w", err)
	}
	total := 0
	for _, q := range queues {
		// "(" makes the score bound exclusive: died_at < cutoff, not <=.
		n, err := purgeDeadBeforeScript.Run(ctx, b.client,
			[]string{b.deadIdxKey(q), b.deadKey(q), b.indexKey()},
			fmt.Sprintf("(%d", cutoff.UnixMilli())).Int()
		if err != nil {
			return total, fmt.Errorf("redisqueue: purge dead before %q: %w", q, err)
		}
		total += n
	}
	return total, nil
}
```

Also update `Push` to the variadic signature: encode and route each job inside the single `TxPipeline` (one `SAdd`+`HSet` pair per job plus its `ZAdd+HSet` or `XAdd`), calling `ensureGroup` once per distinct queue in the batch before the pipeline. Empty batch returns nil without touching redis.

- [ ] **Step 5: Thread tokens through redis tests and bench**

`redisqueue_test.go`: `b2.Ack(ctx, got2[0].ID)` → `b2.Ack(ctx, got2[0].ID, got2[0].Token)`; `b2.Nack(ctx, crashed[0].ID, time.Now(), "still failing")` → `b2.Nack(ctx, crashed[0].ID, crashed[0].Token, time.Now(), "still failing")`; `b2.Ack(ctx, retried[0].ID)` → `b2.Ack(ctx, retried[0].ID, retried[0].Token)`. NOTE for the crash tests: `b2` claims with fresh refs, so its tokens are valid — the assertions are otherwise unchanged. `bench_test.go`: same `Ack` threading as pg.

- [ ] **Step 6: Run redis integration tier**

Run: `just fmt ./async/queue/redis/... && go build ./async/queue/redis && just test-integration ./async/queue/redis/`
Expected: PASS including all new conformance subtests.

- [ ] **Step 7: Commit**

```bash
git add async/queue/redis
git commit -m "feat(redisqueue)!: ownership-fenced Lua finalize, kill-time dead index, atomic DLQ ops

Finalize ops verify PEL ownership (XPENDING consumer match) inside Lua before XACK/XDEL — a stale worker can no longer clobber a reclaimed job. Dead storage gains a kill-time ZSET index: ListDead is O(limit) instead of HGETALL, PurgeDeadBefore is a range sweep. Requeue/Purge become HDEL-as-test scripts, eliminating the concurrent double-requeue race. Push is a variadic atomic pipeline."
```

---

### Task 4: Redis — poison parking and Maintain

**Files:**
- Modify: `async/queue/redis/redisqueue.go`, `async/queue/redis/doc.go`
- Test: `async/queue/redis/redisqueue_test.go`

**Interfaces:**
- Consumes: `queue.Maintainer`, `ops/logger.NewNope`.
- Produces: `redisqueue.Broker` implements `queue.Maintainer`; options `WithLogger(*slog.Logger)`, `WithConsumerIdleCutoff(time.Duration)` (default 1h); key `<prefix><q>:poison` (list of raw undecodable entries); `poisonKey(q string) string` helper.

- [ ] **Step 1: Logger + idle-cutoff options**

Add fields `log *slog.Logger` and `idleCutoff time.Duration` to `Broker`; defaults in `New`: `log: logger.NewNope(), idleCutoff: time.Hour`. Options:

```go
// WithLogger sets the logger (default logger.NewNope()); used for poison
// parking and maintenance reporting.
func WithLogger(l *slog.Logger) Option {
	return func(b *Broker) { b.log = l }
}

// WithConsumerIdleCutoff overrides how long a consumer with no pending
// entries must be idle before Maintain deletes it (default 1h).
func WithConsumerIdleCutoff(d time.Duration) Option {
	return func(b *Broker) { b.idleCutoff = d }
}
```

Imports: `log/slog`, `github.com/dmitrymomot/forge/ops/logger`.

- [ ] **Step 2: Poison parking in Claim**

Add the key helper, script, and park method:

```go
func (b *Broker) poisonKey(q string) string { return b.prefix + q + ":poison" }

// poisonScript parks a raw undecodable entry and removes it from the stream so
// one bad entry (foreign XADD, future wire version) cannot wedge Claim forever.
var poisonScript = redis.NewScript(`
redis.call('RPUSH', KEYS[2], ARGV[3])
redis.call('XACK', KEYS[1], ARGV[1], ARGV[2])
redis.call('XDEL', KEYS[1], ARGV[2])
return 1
`)

// park moves an undecodable stream entry to the queue's poison list. The
// entry is already in this consumer's PEL (just read or autoclaimed), so the
// ack inside the script is ours to perform.
func (b *Broker) park(ctx context.Context, q string, m redis.XMessage, decErr error) {
	raw, _ := m.Values["j"].(string)
	if raw == "" {
		raw = fmt.Sprintf("unparseable stream entry %s: %v", m.ID, m.Values)
	}
	if err := poisonScript.Run(ctx, b.client, []string{b.streamKey(q), b.poisonKey(q)}, group, m.ID, raw).Err(); err != nil {
		b.log.ErrorContext(ctx, "redisqueue: poison park failed", slog.String("queue", q), slog.String("msg_id", m.ID), slog.Any("error", err))
		return
	}
	b.log.ErrorContext(ctx, "redisqueue: undecodable entry parked to poison list", slog.String("queue", q), slog.String("msg_id", m.ID), slog.Any("error", decErr))
}
```

In `Claim`, both decode sites change from `return nil, err` to park-and-continue:

```go
			j, err := b.decodeMsg(m)
			if err != nil {
				b.park(ctx, q, m, err)
				continue
			}
```

- [ ] **Step 3: Implement Maintain**

```go
// Maintain implements queue.Maintainer: deletes consumers that have no
// pending entries and have been idle past the cutoff (each process registers
// a unique consumer name, so restarts accumulate them forever otherwise), and
// prunes queues whose stream, delayed set, dead store, and poison list are all
// empty from the queues registry so Stats stops probing them. Safe to run
// concurrently from every worker instance.
func (b *Broker) Maintain(ctx context.Context) error {
	queues, err := b.client.SMembers(ctx, b.queuesKey()).Result()
	if err != nil {
		return fmt.Errorf("redisqueue: maintain queues: %w", err)
	}
	for _, q := range queues {
		consumers, err := b.client.XInfoConsumers(ctx, b.streamKey(q), group).Result()
		if err != nil && !strings.Contains(err.Error(), "NOGROUP") && !strings.Contains(err.Error(), "no such key") {
			return fmt.Errorf("redisqueue: maintain consumers %q: %w", q, err)
		}
		for _, c := range consumers {
			if c.Name == b.consumer || c.Pending > 0 || c.Idle < b.idleCutoff {
				continue
			}
			if err := b.client.XGroupDelConsumer(ctx, b.streamKey(q), group, c.Name).Err(); err != nil {
				return fmt.Errorf("redisqueue: maintain del consumer %q: %w", c.Name, err)
			}
		}
		empty, err := b.queueEmpty(ctx, q)
		if err != nil {
			return err
		}
		if empty {
			if err := b.client.SRem(ctx, b.queuesKey(), q).Err(); err != nil {
				return fmt.Errorf("redisqueue: maintain srem %q: %w", q, err)
			}
		}
	}
	return nil
}

func (b *Broker) queueEmpty(ctx context.Context, q string) (bool, error) {
	streamLen, err := b.client.XLen(ctx, b.streamKey(q)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return false, fmt.Errorf("redisqueue: maintain xlen %q: %w", q, err)
	}
	delayed, err := b.client.ZCard(ctx, b.delayedKey(q)).Result()
	if err != nil {
		return false, fmt.Errorf("redisqueue: maintain zcard %q: %w", q, err)
	}
	dead, err := b.client.HLen(ctx, b.deadKey(q)).Result()
	if err != nil {
		return false, fmt.Errorf("redisqueue: maintain hlen %q: %w", q, err)
	}
	poison, err := b.client.LLen(ctx, b.poisonKey(q)).Result()
	if err != nil {
		return false, fmt.Errorf("redisqueue: maintain llen %q: %w", q, err)
	}
	return streamLen == 0 && delayed == 0 && dead == 0 && poison == 0, nil
}
```

- [ ] **Step 4: Write the driver-specific tests**

Append to `redisqueue_test.go`:

```go
var _ queue.Maintainer = (*redisqueue.Broker)(nil)

func TestRedisQueue_PoisonEntryDoesNotWedgeClaim(t *testing.T) {
	client := dial(t)
	prefix := fmt.Sprintf("qt:poison:%s:", runID)
	b, err := redisqueue.New(client, redisqueue.WithPrefix(prefix))
	require.NoError(t, err)
	ctx := context.Background()
	c := queue.NewClient(b)

	// Establish the stream+group, drain it.
	require.NoError(t, c.PushRaw(ctx, "ok.kind", []byte(`{}`)))
	got, err := b.Claim(ctx, "default", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NoError(t, b.Ack(ctx, got[0].ID, got[0].Token))

	// A foreign producer XADDs garbage straight into the stream.
	require.NoError(t, client.XAdd(ctx, &redis.XAddArgs{
		Stream: prefix + "default", Values: map[string]any{"j": "not json at all"},
	}).Err())
	require.NoError(t, c.PushRaw(ctx, "ok.kind", []byte(`{"n":2}`)))

	// Claim parks the poison entry and still returns the good job.
	good, err := b.Claim(ctx, "default", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, good, 1)
	assert.Equal(t, "ok.kind", good[0].Type)
	require.NoError(t, b.Ack(ctx, good[0].ID, good[0].Token))

	parked, err := client.LRange(ctx, prefix+"default:poison", 0, -1).Result()
	require.NoError(t, err)
	require.Len(t, parked, 1)
	assert.Equal(t, "not json at all", parked[0])

	// The queue is fully drained: subsequent claims stay clean.
	again, err := b.Claim(ctx, "default", 10, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, again)
}

func TestRedisQueue_MaintainCleansConsumersAndStaleQueues(t *testing.T) {
	client := dial(t)
	prefix := fmt.Sprintf("qt:maintain:%s:", runID)
	ctx := context.Background()

	// b1 processes a job and "retires" (its consumer entry lingers).
	b1, err := redisqueue.New(client, redisqueue.WithPrefix(prefix), redisqueue.WithConsumerIdleCutoff(time.Millisecond))
	require.NoError(t, err)
	c := queue.NewClient(b1)
	require.NoError(t, c.PushRaw(ctx, "m.kind", []byte(`{}`), queue.WithQueue("tmpq")))
	got, err := b1.Claim(ctx, "tmpq", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NoError(t, b1.Ack(ctx, got[0].ID, got[0].Token))

	// b2 maintains: b1's consumer is idle with zero pending → deleted; tmpq is
	// completely empty → dropped from the queues registry.
	b2, err := redisqueue.New(client, redisqueue.WithPrefix(prefix), redisqueue.WithConsumerIdleCutoff(time.Millisecond))
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond) // let b1's consumer idle past the cutoff
	require.NoError(t, b2.Maintain(ctx))

	consumers, err := client.XInfoConsumers(ctx, prefix+"tmpq", "workers").Result()
	require.NoError(t, err)
	for _, cons := range consumers {
		assert.NotEqual(t, 0, cons.Pending, "zero-pending idle consumers must be deleted (only b2's own live consumer may remain)")
	}

	members, err := client.SMembers(ctx, prefix+"queues").Result()
	require.NoError(t, err)
	assert.NotContains(t, members, "tmpq", "fully-empty queue must leave the registry")

	// A later push simply re-registers the queue.
	require.NoError(t, c.PushRaw(ctx, "m.kind", []byte(`{}`), queue.WithQueue("tmpq")))
	st, err := b2.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, st["tmpq"].Pending)
}
```

- [ ] **Step 5: Run, lint repo-wide (red window closes here)**

Run: `just fmt ./async/queue/... && just test-integration ./async/queue/redis/ && go test ./... && just lint`
Expected: all PASS — the whole repo builds, unit tier green, lint green.

- [ ] **Step 6: Commit**

```bash
git add async/queue/redis
git commit -m "feat(redisqueue): poison parking and Maintain housekeeping

Undecodable stream entries are atomically parked to <prefix><queue>:poison instead of failing Claim forever — a foreign XADD or future wire version can no longer wedge the queue. Maintain (queue.Maintainer) deletes zero-pending consumers idle past a cutoff and prunes fully-empty queues from the registry so Stats stops probing retired queues."
```

---

### Task 5: Client — PushMany, empty-queue-name rejection

**Files:**
- Modify: `async/queue/client.go`
- Test: `async/queue/client_test.go`

**Interfaces:**
- Produces: `func PushMany[T any](ctx context.Context, c *Client, k Kind[T], payloads []T, opts ...PushOption) error`; `buildJob`/`buildJobs` reject `WithQueue("")`.

- [ ] **Step 1: Write the failing tests**

Append to `client_test.go`:

```go
func TestPushMany(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("batch claims back in order with unique ids", func(t *testing.T) {
		t.Parallel()
		b := queue.NewMemoryBroker()
		c := queue.NewClient(b)
		require.NoError(t, queue.PushMany(ctx, c, kindWelcome, []welcomePayload{{UserID: "a"}, {UserID: "b"}, {UserID: "c"}}))
		got, err := b.Claim(ctx, "default", 10, time.Minute)
		require.NoError(t, err)
		require.Len(t, got, 3)
		seen := map[string]bool{}
		for _, j := range got {
			assert.False(t, seen[j.ID], "ids must be unique")
			seen[j.ID] = true
		}
	})
	t.Run("scope hook runs once per batch", func(t *testing.T) {
		t.Parallel()
		b := queue.NewMemoryBroker()
		var hookCalls int
		c := queue.NewClient(b, queue.WithScope(func(context.Context) (string, error) {
			hookCalls++
			return "tenant-a", nil
		}))
		require.NoError(t, queue.PushMany(ctx, c, kindWelcome, []welcomePayload{{UserID: "a"}, {UserID: "b"}}))
		assert.Equal(t, 1, hookCalls, "one scope resolution per batch, not per job")
		got, err := b.Claim(ctx, "default", 10, time.Minute)
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, "tenant-a", got[0].Scope)
		assert.Equal(t, "tenant-a", got[1].Scope)
	})
	t.Run("empty slice is a no-op", func(t *testing.T) {
		t.Parallel()
		c := queue.NewClient(queue.NewMemoryBroker())
		require.NoError(t, queue.PushMany(ctx, c, kindWelcome, nil))
	})
}

func TestPush_EmptyQueueNameRejected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := queue.NewClient(queue.NewMemoryBroker())
	assert.Error(t, queue.Push(ctx, c, kindWelcome, welcomePayload{UserID: "u"}, queue.WithQueue("")))
	assert.Error(t, queue.PushMany(ctx, c, kindWelcome, []welcomePayload{{UserID: "u"}}, queue.WithQueue("")))
	assert.Error(t, c.PushRaw(ctx, "raw.kind", []byte(`{}`), queue.WithQueue("")))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./async/queue/ -run 'TestPushMany|TestPush_EmptyQueueNameRejected' -v`
Expected: FAIL — `undefined: queue.PushMany` and the empty-name case passes jobs through today.

- [ ] **Step 3: Implement**

In `client.go`, refactor `buildJob` into a shared base plus per-job factory, and add `PushMany`:

```go
// pushBase is the per-push-call state shared by every job in a batch: parsed
// options, resolved scope, and the batch timestamp.
type pushBase struct {
	now   time.Time
	scope string
	p     pushConfig
}

func (c *Client) pushBase(ctx context.Context, opts []PushOption) (pushBase, error) {
	p := pushConfig{queue: "default"}
	for _, opt := range opts {
		opt(&p)
	}
	if p.queue == "" {
		return pushBase{}, fmt.Errorf("queue: push: empty queue name")
	}
	scope := ""
	if c.scope != nil {
		s, err := c.scope(ctx)
		if err != nil {
			return pushBase{}, fmt.Errorf("%w: %w", ErrScopeMissing, err)
		}
		if s == "" {
			return pushBase{}, ErrScopeMissing
		}
		scope = s
	}
	return pushBase{p: p, scope: scope, now: c.clk.Now().UTC()}, nil
}

func (base pushBase) job(name string, payload []byte) Job {
	runAt := base.now
	switch {
	case !base.p.runAt.IsZero():
		runAt = base.p.runAt.UTC()
	case base.p.delay > 0:
		runAt = base.now.Add(base.p.delay)
	}
	return Job{
		ID:          id.NewUUID().String(),
		Queue:       base.p.queue,
		Type:        name,
		Payload:     payload,
		Scope:       base.scope,
		MaxAttempts: base.p.maxAttempts,
		RunAt:       runAt,
		CreatedAt:   base.now,
	}
}

func (c *Client) buildJob(ctx context.Context, name string, payload []byte, opts []PushOption) (Job, error) {
	base, err := c.pushBase(ctx, opts)
	if err != nil {
		return Job{}, err
	}
	return base.job(name, payload), nil
}

// PushMany enqueues one typed job per payload in a single atomic batch: one
// scope resolution, one option parse, one broker round trip. An empty slice
// is a no-op.
func PushMany[T any](ctx context.Context, c *Client, k Kind[T], payloads []T, opts ...PushOption) error {
	if len(payloads) == 0 {
		return nil
	}
	base, err := c.pushBase(ctx, opts)
	if err != nil {
		return err
	}
	jobs := make([]Job, 0, len(payloads))
	for i, p := range payloads {
		raw, err := json.Marshal(p)
		if err != nil {
			return fmt.Errorf("queue: marshal payload %d for %q: %w", i, k.Name(), err)
		}
		jobs = append(jobs, base.job(k.Name(), raw))
	}
	return c.broker.Push(ctx, jobs...)
}
```

`Push`, `PushTx`, `PushRaw` keep calling `buildJob` and are otherwise unchanged.

- [ ] **Step 4: Run tests**

Run: `just fmt ./async/queue/... && go test ./async/queue/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add async/queue/client.go async/queue/client_test.go
git commit -m "feat(queue): PushMany bulk enqueue and empty-queue-name rejection

One scope resolution and one atomic broker batch per PushMany call. WithQueue(\"\") now fails fast instead of routing jobs to a queue nothing drains."
```

---

### Task 6: Service — default HandlerTimeout

**Files:**
- Modify: `async/queue/config.go`, `async/queue/options.go`, `async/queue/service.go`
- Test: `async/queue/config_test.go`, `async/queue/service_test.go`

**Interfaces:**
- Produces: `Config.HandlerTimeout time.Duration` (default `10 * time.Minute`, `0` disables the default globally, negative invalid); handler gains `timeoutSet bool`; effective timeout = per-kind if set, else config.

- [ ] **Step 1: Write the failing tests**

Append to `service_test.go`:

```go
func TestService_DefaultHandlerTimeout(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.HandlerTimeout = 30 * time.Millisecond
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(cfg))
	require.NoError(t, err)

	queue.Register(svc, kindWelcome, func(ctx context.Context, _ welcomePayload) error {
		<-ctx.Done() // no per-kind timeout: the Config default must fire
		return ctx.Err()
	}, queue.WithHandlerBackoff(backoff.Constant(5*time.Millisecond)))

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b)
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}, queue.WithMaxAttempts(1)))

	eventually(t, func() bool {
		dead, _ := b.ListDead(context.Background(), "default", 10)
		return len(dead) == 1
	}, "config-default handler timeout must bound an unconfigured handler")
	dead, _ := b.ListDead(context.Background(), "default", 10)
	require.Len(t, dead, 1)
	assert.Contains(t, dead[0].LastError, "context deadline exceeded")
}

func TestService_HandlerTimeoutZeroOptsOut(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.HandlerTimeout = 20 * time.Millisecond
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(cfg))
	require.NoError(t, err)

	var done atomic.Bool
	queue.Register(svc, kindWelcome, func(ctx context.Context, _ welcomePayload) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(150 * time.Millisecond): // 7x the config default
			done.Store(true)
			return nil
		}
	}, queue.WithHandlerTimeout(0)) // explicit opt-out for a long-running kind

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b)
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}))

	eventually(t, func() bool { return done.Load() }, "WithHandlerTimeout(0) must disable the config default")
	dead, _ := b.ListDead(context.Background(), "default", 10)
	assert.Empty(t, dead)
}
```

Append to `config_test.go` (follow its existing table/assert style):

```go
func TestConfig_HandlerTimeout(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 10*time.Minute, queue.DefaultConfig().HandlerTimeout, "default handler timeout is 10m")

	cfg := queue.DefaultConfig()
	cfg.HandlerTimeout = 0
	assert.NoError(t, cfg.Validate(), "0 disables the default timeout")

	cfg.HandlerTimeout = -time.Second
	assert.ErrorIs(t, cfg.Validate(), queue.ErrInvalidConfig)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./async/queue/ -run 'TestService_DefaultHandlerTimeout|TestService_HandlerTimeoutZeroOptsOut|TestConfig_HandlerTimeout' -v`
Expected: FAIL — `cfg.HandlerTimeout` undefined.

- [ ] **Step 3: Implement**

`config.go`: add field + default + validation:

```go
	HandlerTimeout time.Duration `env:"QUEUE_HANDLER_TIMEOUT"`
```

`DefaultConfig()` gains `HandlerTimeout: 10 * time.Minute` (update its doc comment: "…, 10m handler timeout, …"). `Validate()` gains:

```go
	if c.HandlerTimeout < 0 {
		return fmt.Errorf("%w: HandlerTimeout must be >= 0 (0 disables the default), got %v", ErrInvalidConfig, c.HandlerTimeout)
	}
```

`service.go`: add `timeoutSet bool` to the `handler` struct. In `process`, the timeout block becomes:

```go
	timeout := s.cfg.HandlerTimeout
	if h.timeoutSet {
		timeout = h.timeout
	}
	var cancel context.CancelFunc = func() {}
	if timeout > 0 {
		hctx, cancel = context.WithTimeout(hctx, timeout)
	}
```

`options.go`: update `WithHandlerTimeout`:

```go
// WithHandlerTimeout bounds each invocation of this kind; expiry counts as a
// failure and takes the retry path. Overrides Config.HandlerTimeout;
// WithHandlerTimeout(0) disables the timeout entirely for kinds that
// legitimately run long.
func WithHandlerTimeout(d time.Duration) HandlerOption {
	return func(h *handler) { h.timeout, h.timeoutSet = d, true }
}
```

- [ ] **Step 4: Run tests**

Run: `just fmt ./async/queue/... && go test ./async/queue/`
Expected: PASS (including the pre-existing `TestService_HandlerTimeout`, which now sets `timeoutSet` through the same option).

- [ ] **Step 5: Commit**

```bash
git add async/queue/config.go async/queue/options.go async/queue/service.go async/queue/config_test.go async/queue/service_test.go
git commit -m "feat(queue): default 10m handler timeout with per-kind opt-out

A hung handler without WithHandlerTimeout used to leak its worker slot forever while the heartbeat kept the job invisible. Config.HandlerTimeout (default 10m) now bounds every handler; WithHandlerTimeout(0) opts a long-running kind out explicitly."
```

---

### Task 7: Service — lease-lost handling (finalize + heartbeat cancel)

**Files:**
- Modify: `async/queue/service.go`
- Test: `async/queue/service_test.go`

**Interfaces:**
- Consumes: `queue.ErrLeaseLost` from Task 1.
- Produces: heartbeat cancels the handler context on `ErrLeaseLost`; `finalize` logs lease-lost at warn and drops.

- [ ] **Step 1: Write the failing test**

Append to `service_test.go`:

```go
// leaseLostBroker simulates another worker stealing the job: every Extend
// reports the lease as lost.
type leaseLostBroker struct {
	*queue.MemoryBroker
}

func (b *leaseLostBroker) Extend(context.Context, string, string, time.Duration) error {
	return queue.ErrLeaseLost
}

func TestService_LeaseLostCancelsHandler(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.Lease = 60 * time.Millisecond // heartbeat ticks every 20ms
	b := &leaseLostBroker{MemoryBroker: queue.NewMemoryBroker()}
	svc, err := queue.NewService(b, queue.WithConfig(cfg))
	require.NoError(t, err)

	var cancelled atomic.Bool
	queue.Register(svc, kindWelcome, func(ctx context.Context, _ welcomePayload) error {
		select {
		case <-ctx.Done():
			cancelled.Store(true)
			return queue.Cancel // moot: someone else owns it now
		case <-time.After(5 * time.Second):
			return nil
		}
	})

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b)
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}))

	eventually(t, func() bool { return cancelled.Load() }, "heartbeat must cancel the handler context when the lease is lost")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./async/queue/ -run TestService_LeaseLostCancelsHandler -v`
Expected: FAIL (timeout: today a lost lease only logs; the handler keeps waiting the full 5s).

- [ ] **Step 3: Implement in `process`**

Build the handler context cancellable BEFORE starting the heartbeat and hand the cancel func to the heartbeat goroutine. The heartbeat section of `process` becomes:

```go
	hctx := opCtx
	if s.scopeCtx != nil {
		hctx = s.scopeCtx(hctx, job.Scope)
	}
	hctx, cancelHandler := context.WithCancel(hctx)

	// Heartbeat: extend the lease at lease/3 until the handler returns. A
	// lost lease means another worker owns the job now — cancel the handler
	// so the slot stops doing doomed work whose finalize would be rejected.
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
				err := s.broker.Extend(hbCtx, job.ID, cj.Token, s.cfg.Lease)
				switch {
				case err == nil:
				case errors.Is(err, ErrLeaseLost):
					s.log.WarnContext(hbCtx, "queue lease lost, cancelling handler", logAttrs...)
					cancelHandler()
					return
				case hbCtx.Err() == nil:
					s.log.ErrorContext(hbCtx, "queue lease extend failed", append(logAttrs, slog.Any("error", err))...)
				}
			}
		}
	})
```

The timeout wrapping from Task 6 then applies on top of the cancellable `hctx`; after `invoke` returns call `cancel()`, `cancelHandler()`, `stopHB()`, `hbWG.Wait()` (in that order).

In `finalize`, add the lease-lost branch before the generic failure branch:

```go
func (s *Service) finalize(ctx context.Context, outcome string, logAttrs []any, op func() error) {
	err := op()
	switch {
	case errors.Is(err, ErrLeaseLost):
		s.log.WarnContext(ctx, "queue lease lost, job owned elsewhere", append(logAttrs, slog.String("outcome", outcome))...)
	case err != nil:
		s.log.ErrorContext(ctx, "queue broker op failed, job will redeliver after lease expiry", append(logAttrs, slog.String("outcome", outcome), slog.Any("error", err))...)
	default:
		s.log.InfoContext(ctx, "queue job "+outcome, logAttrs...)
	}
}
```

- [ ] **Step 4: Run tests**

Run: `just fmt ./async/queue/... && go test ./async/queue/ -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add async/queue/service.go async/queue/service_test.go
git commit -m "feat(queue): cancel handler and stop heartbeating on lease loss

When Extend returns ErrLeaseLost another worker owns the job; the heartbeat now cancels the handler context so the slot frees immediately instead of finishing doomed work. finalize distinguishes lease-lost (warn, expected) from broker failures (error, will redeliver)."
```

---

### Task 8: Service — claim-error backoff, idle-sweep fix, clock seam

**Files:**
- Modify: `async/queue/service.go`, `async/queue/options.go`
- Test: `async/queue/service_test.go`

**Interfaces:**
- Produces: `WithServiceClock(clk clock.Clock)` option; `Service.clk` used for `retryAt` (and the Task 9 sweep cutoff); `pollOnce` returns `(claimed int, allErrored bool)`; poll wait doubles on all-error rounds up to `max(30s, PollInterval)`.

- [ ] **Step 1: Write the failing tests**

Append to `service_test.go`:

```go
// countingBroker counts Claim calls; optionally every claim errors.
type countingBroker struct {
	*queue.MemoryBroker
	claims atomic.Int64
	fail   atomic.Bool
}

func (b *countingBroker) Claim(ctx context.Context, q string, n int, lease time.Duration) ([]queue.ClaimedJob, error) {
	b.claims.Add(1)
	if b.fail.Load() {
		return nil, errors.New("broker down")
	}
	return b.MemoryBroker.Claim(ctx, q, n, lease)
}

func TestService_ClaimErrorBackoff(t *testing.T) {
	t.Parallel()
	cfg := testConfig() // 10ms poll
	b := &countingBroker{MemoryBroker: queue.NewMemoryBroker()}
	b.fail.Store(true)
	svc, err := queue.NewService(b, queue.WithConfig(cfg))
	require.NoError(t, err)
	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error { return nil })

	stop := runService(t, svc)
	time.Sleep(500 * time.Millisecond)
	failing := b.claims.Load()
	stop()

	// Naive 10ms cadence would make ~50 claims in 500ms; doubling backoff
	// (10,20,40,80,160,320ms…) allows at most ~7. Generous bound: < 15.
	assert.Less(t, failing, int64(15), "claim errors must widen the poll interval, got %d claims", failing)
	assert.GreaterOrEqual(t, failing, int64(3), "the service must keep retrying")
}

func TestService_ClaimBackoffResetsOnSuccess(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	b := &countingBroker{MemoryBroker: queue.NewMemoryBroker()}
	b.fail.Store(true)
	svc, err := queue.NewService(b, queue.WithConfig(cfg))
	require.NoError(t, err)

	var processed atomic.Bool
	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error {
		processed.Store(true)
		return nil
	})

	stop := runService(t, svc)
	defer stop()

	time.Sleep(150 * time.Millisecond) // let backoff widen
	b.fail.Store(false)
	c := queue.NewClient(b)
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}))

	eventually(t, func() bool { return processed.Load() }, "service must recover and process after the broker heals")
}

func TestService_IdlePollClaimsEachQueueOnce(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	b := &countingBroker{MemoryBroker: queue.NewMemoryBroker()}
	svc, err := queue.NewService(b, queue.WithConfig(cfg))
	require.NoError(t, err)
	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error { return nil })

	stop := runService(t, svc)
	time.Sleep(300 * time.Millisecond)
	claims := b.claims.Load()
	stop()

	// ~30 polls in 300ms at 10ms; one queue idle must claim once per poll
	// (the old leftover sweep claimed twice). Bound generously above 1x and
	// strictly below 2x.
	assert.Less(t, claims, int64(45), "idle polls must not double-claim, got %d claims for ~30 polls", claims)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./async/queue/ -run 'TestService_ClaimErrorBackoff|TestService_ClaimBackoffResetsOnSuccess|TestService_IdlePollClaimsEachQueueOnce' -v`
Expected: `TestService_ClaimErrorBackoff` FAILS (~50 claims), `TestService_IdlePollClaimsEachQueueOnce` FAILS (~60 claims). The reset test may pass already — keep it as a regression guard.

- [ ] **Step 3: Implement**

`options.go` — add (imports already include `clock` via the existing file):

```go
// WithServiceClock injects a clock used for retry scheduling and the
// retention sweep cutoff (tests). Tickers stay real time.
func WithServiceClock(clk clock.Clock) ServiceOption {
	return func(s *Service) { s.clk = clk }
}
```

`service.go` — add field `clk clock.Clock` to `Service`, default `clk: clock.System()` in `NewService` (import `github.com/dmitrymomot/forge/core/clock`). In `process`, `retryAt := time.Now().UTC().Add(bo.Next(job.Attempt))` → `retryAt := s.clk.Now().UTC().Add(bo.Next(job.Attempt))`.

Rework `Run`'s loop (replacing the ticker) and `pollOnce`:

```go
	maxWait := max(30*time.Second, s.cfg.PollInterval)
	wait := s.cfg.PollInterval
	for {
		claimed, allErrored := s.pollOnce(ctx, opCtx, sem, &wg)
		if ctx.Err() != nil {
			break
		}
		if allErrored {
			wait = min(wait*2, maxWait) // broker down: widen instead of hammering
		} else {
			wait = s.cfg.PollInterval
			if claimed > 0 {
				continue // backlog: keep claiming without waiting
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(wait):
			continue
		}
		break
	}
```

`pollOnce` returns `(int, bool)` and tracks per-queue outcomes:

```go
// pollOnce claims up to the free slot budget across queues (in claimOrder)
// and dispatches each claimed job. Returns the number of jobs claimed and
// whether every attempted claim errored (the signal to back off).
func (s *Service) pollOnce(ctx context.Context, opCtx context.Context, sem chan struct{}, wg *sync.WaitGroup) (int, bool) {
	free := s.cfg.Concurrency - len(sem)
	if free <= 0 {
		return 0, false
	}
	if s.cfg.ClaimBatch > 0 && free > s.cfg.ClaimBatch {
		free = s.cfg.ClaimBatch
	}
	total, attempted, errored := 0, 0, 0
	drained := make(map[string]bool, len(s.queues))
	claim := func(qname string, n int) {
		if n <= 0 || ctx.Err() != nil {
			return
		}
		attempted++
		jobs, err := s.broker.Claim(ctx, qname, n, s.cfg.Lease)
		if err != nil {
			errored++
			if ctx.Err() == nil {
				s.log.ErrorContext(ctx, "queue claim failed", slog.String("queue", qname), slog.Any("error", err))
			}
			return
		}
		if len(jobs) < n {
			drained[qname] = true // returned less than asked: proven empty
		}
		for _, cj := range jobs {
			sem <- struct{}{}
			wg.Go(func() {
				defer func() { <-sem }()
				s.process(opCtx, cj)
			})
		}
		free -= len(jobs)
		total += len(jobs)
	}

	if s.strict {
		for _, q := range s.queues { // static weight-desc order
			claim(q.name, free)
		}
		return total, attempted > 0 && errored == attempted
	}
	order, quota := s.claimPlan(free)
	for _, qname := range order {
		claim(qname, min(quota[qname], free))
	}
	if total > 0 { // leftover sweep only when something was claimed at all
		for _, q := range s.queues {
			if !drained[q.name] {
				claim(q.name, free)
			}
		}
	}
	return total, attempted > 0 && errored == attempted
}
```

- [ ] **Step 4: Run tests**

Run: `just fmt ./async/queue/... && go test ./async/queue/ -race`
Expected: PASS, including all pre-existing priority/SWRR tests (the sweep changes must not break `TestService_WeightedDoesNotStarveLightQueue`).

- [ ] **Step 5: Commit**

```bash
git add async/queue/service.go async/queue/options.go async/queue/service_test.go
git commit -m "feat(queue): claim-error poll backoff, idle sweep fix, service clock seam

Poll waits double on all-error rounds (cap max(30s, PollInterval)) so a fleet stops hammering a recovering broker. The leftover sweep now runs only after a productive first pass and skips proven-drained queues, halving idle broker load. retryAt derives from an injectable clock (WithServiceClock)."
```

---

### Task 9: Service — retention & maintenance sweep

**Files:**
- Modify: `async/queue/config.go`, `async/queue/service.go`
- Test: `async/queue/config_test.go`, `async/queue/service_internal_test.go` (white-box: needs the unexported sweep interval)

**Interfaces:**
- Produces: `Config.DeadRetention time.Duration` (default `720h`, `0` = keep forever, negative invalid); unexported `Service.sweepEvery` (default 5m); sweep goroutine calls `Maintain` (if implemented) then `PurgeDeadBefore(clk.Now().Add(-DeadRetention))`.

- [ ] **Step 1: Write the failing tests**

Append to `config_test.go`:

```go
func TestConfig_DeadRetention(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 720*time.Hour, queue.DefaultConfig().DeadRetention, "default DLQ retention is 30 days")

	cfg := queue.DefaultConfig()
	cfg.DeadRetention = 0
	assert.NoError(t, cfg.Validate(), "0 keeps dead jobs forever")

	cfg.DeadRetention = -time.Hour
	assert.ErrorIs(t, cfg.Validate(), queue.ErrInvalidConfig)
}
```

Append to `service_internal_test.go` (white-box package `queue`):

```go
type sweepSpyBroker struct {
	*MemoryBroker
	mu         sync.Mutex
	maintains  int
	purges     int
	lastCutoff time.Time
}

func (b *sweepSpyBroker) Maintain(context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.maintains++
	return nil
}

func (b *sweepSpyBroker) PurgeDeadBefore(ctx context.Context, cutoff time.Time) (int, error) {
	b.mu.Lock()
	b.purges++
	b.lastCutoff = cutoff
	b.mu.Unlock()
	return b.MemoryBroker.PurgeDeadBefore(ctx, cutoff)
}

func TestSweepLoop_MaintainsAndPurges(t *testing.T) {
	t.Parallel()
	fixed := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	b := &sweepSpyBroker{MemoryBroker: NewMemoryBroker()}
	cfg := DefaultConfig()
	cfg.PollInterval = 10 * time.Millisecond
	cfg.DeadRetention = 24 * time.Hour
	s, err := NewService(b, WithConfig(cfg), WithServiceClock(clock.NewMock(fixed)))
	require.NoError(t, err)
	s.sweepEvery = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	require.Eventually(t, func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.maintains >= 2 && b.purges >= 2
	}, 5*time.Second, 5*time.Millisecond, "sweep must invoke Maintain and PurgeDeadBefore repeatedly")

	cancel()
	<-done
	b.mu.Lock()
	defer b.mu.Unlock()
	assert.Equal(t, fixed.Add(-24*time.Hour), b.lastCutoff, "cutoff = clock now - DeadRetention")
}

func TestSweepLoop_RetentionZeroSkipsPurge(t *testing.T) {
	t.Parallel()
	b := &sweepSpyBroker{MemoryBroker: NewMemoryBroker()}
	cfg := DefaultConfig()
	cfg.PollInterval = 10 * time.Millisecond
	cfg.DeadRetention = 0
	s, err := NewService(b, WithConfig(cfg))
	require.NoError(t, err)
	s.sweepEvery = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	require.Eventually(t, func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.maintains >= 2
	}, 5*time.Second, 5*time.Millisecond, "Maintain still runs with retention disabled")

	cancel()
	<-done
	b.mu.Lock()
	defer b.mu.Unlock()
	assert.Zero(t, b.purges, "DeadRetention=0 must never purge")
}
```

Add the needed imports to `service_internal_test.go`: `context`, `sync`, `time`, `github.com/dmitrymomot/forge/core/clock`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./async/queue/ -run 'TestConfig_DeadRetention|TestSweepLoop' -v`
Expected: FAIL — `DeadRetention` and `sweepEvery` undefined.

- [ ] **Step 3: Implement**

`config.go`: add `DeadRetention time.Duration \`env:"QUEUE_DEAD_RETENTION"\``, `DefaultConfig` gains `DeadRetention: 720 * time.Hour` (update doc comment), `Validate` gains:

```go
	if c.DeadRetention < 0 {
		return fmt.Errorf("%w: DeadRetention must be >= 0 (0 keeps dead jobs forever), got %v", ErrInvalidConfig, c.DeadRetention)
	}
```

`service.go`: add `sweepEvery time.Duration` field, set `sweepEvery: 5 * time.Minute` in `NewService`. In `Run`, after the "service started" log line and before the poll loop, start the sweep when there is anything to do (`rand/v2` import for the jitter):

```go
	maintainer, hasMaintain := s.broker.(Maintainer)
	if hasMaintain || s.cfg.DeadRetention > 0 {
		wg.Go(func() { s.sweepLoop(ctx, maintainer) })
	}
```

```go
// sweepLoop runs low-frequency housekeeping: broker Maintain (when
// implemented) and DLQ retention. The first run is jittered so a fleet
// restarting together does not sweep in lockstep; both ops are idempotent and
// cheap, so every instance runs them without leader election.
func (s *Service) sweepLoop(ctx context.Context, maintainer Maintainer) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(rand.N(s.sweepEvery)):
	}
	t := time.NewTicker(s.sweepEvery)
	defer t.Stop()
	for {
		s.sweepOnce(ctx, maintainer)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) sweepOnce(ctx context.Context, maintainer Maintainer) {
	if maintainer != nil {
		if err := maintainer.Maintain(ctx); err != nil && ctx.Err() == nil {
			s.log.ErrorContext(ctx, "queue maintenance failed", slog.String("service", s.name), slog.Any("error", err))
		}
	}
	if s.cfg.DeadRetention <= 0 {
		return
	}
	n, err := s.broker.PurgeDeadBefore(ctx, s.clk.Now().Add(-s.cfg.DeadRetention))
	if err != nil {
		if ctx.Err() == nil {
			s.log.ErrorContext(ctx, "queue dead retention purge failed", slog.String("service", s.name), slog.Any("error", err))
		}
		return
	}
	if n > 0 {
		s.log.InfoContext(ctx, "queue dead jobs purged by retention", slog.String("service", s.name), slog.Int("purged", n), slog.Duration("retention", s.cfg.DeadRetention))
	}
}
```

Note the test relies on `sweepOnce` running immediately after the jitter delay (which `rand.N(20ms)` keeps under 20ms) and then once per tick — the loop above does exactly that.

- [ ] **Step 4: Run tests**

Run: `just fmt ./async/queue/... && go test ./async/queue/ -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add async/queue/config.go async/queue/service.go async/queue/config_test.go async/queue/service_internal_test.go
git commit -m "feat(queue): retention and maintenance sweep

Every worker runs a jittered ~5m sweep: broker Maintain (redis consumer/queue-registry hygiene) plus PurgeDeadBefore(now - Config.DeadRetention). Default retention 30d; 0 keeps dead jobs forever. Idempotent and cheap, so no leader election."
```

---

### Task 10: Benchmark suite + baseline comparison + optimization pass

**Files:**
- Rewrite: `async/queue/bench_test.go`, `async/queue/postgres/bench_test.go`, `async/queue/redis/bench_test.go`
- Create: `docs/superpowers/specs/2026-07-16-queue-bench-after.txt`

**Interfaces:**
- Consumes: everything shipped in Tasks 1–9; baseline `docs/superpowers/specs/2026-07-16-queue-bench-baseline.txt`.

- [ ] **Step 1: Rewrite `async/queue/bench_test.go`**

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

func BenchmarkPushMany_Memory(b *testing.B) {
	for _, size := range []int{100, 10000} {
		b.Run(sizeName(size), func(b *testing.B) {
			broker := queue.NewMemoryBroker()
			c := queue.NewClient(broker)
			ctx := context.Background()
			payloads := make([]benchPayload, size)
			for i := range payloads {
				payloads[i] = benchPayload{N: i}
			}
			b.ReportAllocs()
			for b.Loop() {
				if err := queue.PushMany(ctx, c, kindBench, payloads); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func sizeName(n int) string {
	if n >= 1000 {
		return "10k"
	}
	return "100"
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
			if err := broker.Nack(ctx, j.ID, j.Token, time.Now(), ""); err != nil { // recycle for the next iteration
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkEndToEnd_Memory MUST be run with -benchtime=5000x: with time-based
// benchtime the push loop outruns the drain workers and the drain wait after
// the loop dominates (see the baseline file's note).
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

- [ ] **Step 2: Rewrite `postgres/bench_test.go`**

```go
//go:build integration

package pgqueue_test

import (
	"context"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/core/id"
)

// Past-biased RunAt throughout: visibility is decided by the database clock,
// which can lag the test process on a Docker VM (see brokertest.dueNow).
func benchJob(q string) queue.Job {
	return queue.Job{
		ID: id.NewUUID().String(), Queue: q, Type: "bench.pg", Payload: []byte(`{"n":1}`),
		RunAt: time.Now().UTC().Add(-2 * time.Second), CreatedAt: time.Now().UTC(),
	}
}

func BenchmarkPgPushClaimAck(b *testing.B) {
	broker := newBroker(b, openPool(b))
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if err := broker.Push(ctx, benchJob("default")); err != nil {
			b.Fatal(err)
		}
		jobs, err := broker.Claim(ctx, "default", 1, time.Minute)
		if err != nil || len(jobs) != 1 {
			b.Fatalf("claim: %v (%d jobs)", err, len(jobs))
		}
		if err := broker.Ack(ctx, jobs[0].ID, jobs[0].Token); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPgPushMany(b *testing.B) {
	for _, size := range []int{100, 10000} {
		name := "100"
		if size == 10000 {
			name = "10k"
		}
		b.Run(name, func(b *testing.B) {
			broker := newBroker(b, openPool(b))
			ctx := context.Background()
			jobs := make([]queue.Job, size)
			b.ReportAllocs()
			for b.Loop() {
				for i := range jobs {
					jobs[i] = benchJob("bulk")
				}
				if err := broker.Push(ctx, jobs...); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
```

- [ ] **Step 3: Rewrite `redis/bench_test.go`**

Same shape as pg (keep the existing per-process-unique prefix via `newBroker(b)`): `BenchmarkRedisPushClaimAck` (push one `benchJob`-style job via `broker.Push`, claim 1, `Ack(id, token)`) and `BenchmarkRedisListDead` — seed once before `b.Loop()`: push 5000 jobs in one batch, claim in batches of 100 and `Kill` each with its token; then measure `broker.ListDead(ctx, "default", 50)` per iteration with `b.ReportAllocs()`. Use `queue.Job` literals with `id.NewUUID().String()` ids and past-biased RunAt like the pg file.

- [ ] **Step 4: Run everything and record**

Run in order, appending outputs to `docs/superpowers/specs/2026-07-16-queue-bench-after.txt` (header: commit hash, machine, container images, date; same format as the baseline file):

1. `just bench ./async/queue/` — Push/PushMany/ClaimBatch (skip EndToEnd here; it needs the pinned count)
2. `go test -bench='BenchmarkEndToEnd_Memory' -benchmem -benchtime=5000x ./async/queue/`
3. `just bench-integration ./async/queue/postgres/`
4. `just bench-integration ./async/queue/redis/`

Compare against `2026-07-16-queue-bench-baseline.txt`. Expected: `ClaimBatch_Memory` drops from ~1.15ms/op by an order of magnitude (per-queue buckets); `PgPushClaimAck`/`RedisPushClaimAck` stay within ~2x of baseline (fencing adds a WHERE clause / Lua ownership check — small); `PushMany` at 10k lands far below 10k× the single-push cost.

- [ ] **Step 5: Post-bench optimization pass (measured wins only)**

If `PgPushClaimAck` regressed >2x: `EXPLAIN ANALYZE` the claim CTE and check the `DISTINCT queue` plan in Stats (skip scan vs seq scan); if Stats seq-scans, swap `statsPendingSQL`/`statsDeadSQL` to the recursive-CTE loose scan and re-measure. If `PushMany/10k` disappoints, prototype `pgx.CopyFrom` above a size threshold and keep it only if the bench shows a win. Apply nothing without a before/after number in the results file.

- [ ] **Step 6: Commit**

```bash
git add async/queue/bench_test.go async/queue/postgres/bench_test.go async/queue/redis/bench_test.go docs/superpowers/specs/2026-07-16-queue-bench-after.txt
git commit -m "bench(queue): post-rework benchmark suite and results vs baseline

PushMany at 100/10k on all three brokers, redis ListDead against a seeded DLQ, token-threaded claim cycles. Results recorded in docs/superpowers/specs/2026-07-16-queue-bench-after.txt against the pre-rework baseline."
```

---

### Task 11: Documentation and final sweep

**Files:**
- Modify: `async/queue/doc.go`, `async/queue/postgres/doc.go`, `async/queue/redis/doc.go`, `async/queue/example_test.go`

**Interfaces:** none new — docs must match the shipped code exactly.

- [ ] **Step 1: Update `async/queue/doc.go`**

Rework the delivery-contract and usage sections (keep the overall structure):

- Delivery contract paragraph: claim-with-lease + heartbeat as before, PLUS: "Claims carry a fencing token: a worker whose lease was lost cannot ack, retry, kill, or extend the job anymore (ErrLeaseLost), so duplicate execution is confined to true crash-mid-handler redelivery. HANDLERS MUST STILL BE IDEMPOTENT."
- New paragraph after the verdicts: "Every handler runs under Config.HandlerTimeout (default 10m) unless its kind sets WithHandlerTimeout — WithHandlerTimeout(0) opts a long-running kind out. Dead-lettered jobs are purged after Config.DeadRetention (default 30 days; 0 keeps them forever) by a sweep every worker instance runs; the sweep also drives optional broker housekeeping (Maintainer)."
- Usage block: add one `PushMany` line after the `Push` example: `err = queue.PushMany(ctx, client, KindSendWelcome, batch)      // bulk enqueue, one round trip`.

- [ ] **Step 2: Update driver package docs**

`postgres/doc.go`: state the two-table layout (`queue_jobs` hot + `queue_jobs_dead` cold), PostgreSQL >= 18 floor, bounded Stats (exact to 10k, then capped+flagged), and that `WithTable` derives `<table>_dead`. `redis/doc.go`: state Redis >= 8 floor, the key layout including `:dead:idx` and `:poison` (undecodable entries parked here for manual inspection — check it in ops runbooks), and Maintain semantics.

- [ ] **Step 3: Extend `example_test.go`**

The existing `Example()` compiles unchanged. Append a compile-checked bulk example:

```go
func ExamplePushMany() {
	broker := queue.NewMemoryBroker()
	client := queue.NewClient(broker)

	batch := []sendWelcome{{Email: "a@user.dev"}, {Email: "b@user.dev"}}
	if err := queue.PushMany(context.Background(), client, kindSendWelcome, batch); err != nil {
		panic(err)
	}

	st, _ := broker.Stats(context.Background())
	fmt.Println("pending:", st["default"].Pending)
	// Output: pending: 2
}
```

- [ ] **Step 4: Full verification sweep**

Run: `just fmt ./async/queue/... && go test ./... && just test-integration ./async/queue/... && just lint`
Expected: everything PASS/green.

- [ ] **Step 5: Commit**

```bash
git add async/queue
git commit -m "docs(queue): fencing contract, timeout/retention defaults, runtime floors

doc.go documents the fenced at-least-once contract (duplicates only on true crash), the 10m default handler timeout, 30d DLQ retention, and PushMany. Driver docs state the PostgreSQL >= 18 / Redis >= 8 floors, the two-table layout, and the redis poison key."
```

---

## Plan Self-Review (completed)

- **Spec coverage:** all 13 inventory items + 3 minors map to tasks: poison wedge→T4, fencing→T1–3+7, RETURNING order→T2, ListDead O(DLQ)→T3, consumer accumulation→T4, Stats scan→T2, bloat/two-table→T2, hung handler→T6, claim backoff→T8, bulk push→T1/T2/T3/T5, stale queues→T4, memory O(N)→T1, retention→T9 (+T1–3 broker ops), idle sweep→T8, clock seam→T8, WithQueue("")→T5. Benchmarks→T10, docs→T11.
- **Type consistency:** `ClaimedJob{Job; Token string}`; fenced ops take `(ctx, id, token, ...)` in the order Extend/Ack/Nack/Kill everywhere; `PurgeDeadBefore(ctx, cutoff) (int, error)`; `TxPusher.PushTx(ctx, tx, jobs ...Job)`; `Maintainer.Maintain(ctx) error` — verified identical across Tasks 1, 2, 3, 4, 7, 9.
- **Known intentional deviation:** repo-wide `just lint`/`go build ./...` is red between the Task 1 and Task 4 commits (driver packages mid-rework); each task's scoped verification commands are authoritative until the Task 4 sweep closes the window.
