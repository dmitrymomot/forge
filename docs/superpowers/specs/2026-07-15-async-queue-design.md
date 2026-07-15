# async/queue — Design

Date: 2026-07-15. Status: approved for planning.

## Summary

`async/queue` is THE durable background-work engine for forge and the backbone of the `async/` family (`eventbus`, `outbox`, `scheduler`, `workflow` all build on it). It ships a storage-agnostic pull-only `Broker` seam, a producer `Client`, a worker `Service` (a `supervisor.Service`), typed job kinds, weighted-priority queues, at-least-once claim-with-lease delivery, engine-owned retry/backoff/delay/dead-letter semantics, and three brokers: in-memory (built in), `async/queue/postgres`, and `async/queue/redis`.

The package was cataloged as `async/jobqueue`; it is renamed to `async/queue` because it is shared queue infrastructure, not only a job runner. All packages.md cross-references are updated as part of this work (see §Catalog updates).

## Scope of this PR

- `async/queue` — engine + `Broker` seam + memory broker + `Kind[T]` + DLQ ops API.
- `async/queue/postgres` — SKIP LOCKED claiming, `PushTx`, `migration.Migration`.
- `async/queue/redis` — Streams + consumer groups, `XAUTOCLAIM` lease reclaim, delay zset + mover.
- packages.md catalog updates (rename, drop LISTEN/NOTIFY claim, cross-reference fixes).

Out of scope (backlog, unchanged in catalog): `queue/sqlite`, `queue/nats`, `queue/kafka`, `async/eventbus` (separate package layered on this seam: publish = one Push per subscription queue), `async/outbox` (brings `PushTx`-equivalent to non-SQL brokers), per-tenant queue partitioning/fairness, metrics beyond `Stats`, dead-job auto-expiry, a `Waker` push-hint capability.

## Package layout

```
async/queue/            engine: Client, Service, Broker seam, Kind[T], typed handlers,
                        envelope codec, in-memory broker, DLQ ops
async/queue/postgres/   package pgqueue
async/queue/redis/      package redisqueue
```

## Core types

### Job envelope

`queue.Job`: `ID` (core/id), `Queue` (name, default `"default"`), `Type` (kind name), `Payload` (JSON bytes), `Scope` (tenant, empty when unscoped), `Attempt`, `MaxAttempts`, `RunAt`, `CreatedAt`, `LastError`. The wire encoding is versioned and stable — DLQ inspection and `Requeue` depend on it.

### Broker seam (strictly pull)

```go
type Broker interface {
    Push(ctx context.Context, job Job) error
    Claim(ctx context.Context, queue string, n int, lease time.Duration) ([]Job, error) // non-blocking
    Extend(ctx context.Context, id string, lease time.Duration) error                  // heartbeat
    Ack(ctx context.Context, id string) error                                          // done: delete
    Nack(ctx context.Context, id string, retryAt time.Time) error                      // back to pending at retryAt
    Kill(ctx context.Context, id string, reason string) error                          // → dead
    ListDead(ctx context.Context, queue string, limit int) ([]Job, error)
    Requeue(ctx context.Context, id string) error                                      // dead → pending, attempts reset
    Purge(ctx context.Context, id string) error                                        // delete dead job
    Stats(ctx context.Context) (Stats, error)                                          // pending/dead counts per queue
}
```

Seam rules:

- **Pull-only contract.** `Claim` returns up to n due jobs or empty, never blocks. The engine owns polling cadence. This keeps the seam implementable by any backend (sqlite, kafka later). Push-style wake-ups (pg LISTEN/NOTIFY, `XREADGROUP BLOCK`) are deliberately NOT used in v1; if sub-second dispatch ever matters, an optional `Waker` capability (type-asserted hint, correctness still pull) can be added without touching `Broker`.
- **Drivers move bytes; the engine owns semantics.** Retry policy, backoff math, delay compensation, max-attempts → dead, verdict handling all live in the engine so behavior is identical across backends. Capability discovery (optional interfaces, e.g. native-delay) lets the engine skip compensation where a backend handles something natively.
- `Claim` atomically sets the lease and increments the attempt counter.

### Kind[T] — typed job identity

```go
var KindSendWelcome = queue.NewKind[SendWelcome]("email.send_welcome")
```

A `Kind[T]` is the single declaration binding job name ↔ payload type. `Push` and `Register` accept only Kinds (no raw-string overloads): typos and producer/worker type drift become compile errors instead of dead-lettered jobs. Escape hatch for the rare cross-codebase enqueue: `client.PushRaw(ctx, name string, payload json.RawMessage, opts...)`.

### Client (producer)

```go
client := queue.NewClient(broker, opts...)
err := queue.Push(ctx, client, KindSendWelcome, SendWelcome{...}, pushOpts...)
```

- `queue.Push` is a package-level generic function (methods cannot have type params); it marshals the payload, applies scope, and calls `Broker.Push`.
- Push options: `WithQueue(name)` (default `"default"`), `WithDelay(d)`, `WithRunAt(t)`, `WithMaxAttempts(n)`.
- Client options: `WithScope(func(ctx) (string, error))` — tenancy, see §Tenancy.
- DLQ/ops pass-throughs on Client: `ListDead`, `Requeue`, `Purge`, `Stats`.
- Transactional enqueue goes through the Client so scope capture and marshaling are never bypassed: `queue.PushTx(ctx, client, tx, kind, payload, opts...)` where `tx any` is asserted by the broker via an optional `TxPusher` capability (`PushTx(ctx, tx any, job Job) error`). The postgres broker asserts `pgx.Tx`; brokers without the capability (memory, redis) return `ErrTxUnsupported` — non-SQL brokers get transactional enqueue later via `async/outbox`.

### Service (worker)

```go
svc, err := queue.NewService(broker, opts...)
queue.Register(svc, KindSendWelcome, handleFn, handlerOpts...)
supervisor.Run(ctx, supervisor.WithService(svc), ...)
```

- Implements `supervisor.Service` (blocking `Run(ctx)`; `Name()` defaults to `"queue"`, override via `WithName` when running multiple Service instances under one supervisor).
- Multiple Service instances over the same broker are first-class — same process or separate deployments (e.g. a dedicated worker draining only a `video` queue with its own concurrency). Claim-with-lease already makes competing workers safe. Rule: the queue is the unit of routing — every kind pushed to a queue must be registered on every service draining that queue; to split kinds across services, split the queues.
- Handlers are `func(ctx context.Context, p T) error`; `queue.Register` is package-level generic, panics on duplicate kind registration (wiring bug, fail fast at startup).
- Handler options: `WithHandlerTimeout(d)` (wraps handler ctx; timeout = failure → retry path), `WithHandlerMaxAttempts(n)`, `WithHandlerBackoff(policy)` (per-kind override of the service default).
- Service options: `WithQueues(map[string]int)` (weighted priorities), `WithStrictPriority()`, `WithConcurrency(n)`, `WithName(s)`, `WithLogger(l)`, `WithScopeContext(fn)`, plus config-backed knobs below.
- A job claimed for a kind with no registered handler is `Kill`ed to dead with reason `unregistered kind` (it can be `Requeue`d after a deploy that registers it).

## Delivery semantics & lifecycle

At-least-once via claim-with-lease. States: `pending → claimed → done | retrying → dead`.

1. `Push` → pending, `Attempt=0`, `RunAt` now or future (`WithDelay`/`WithRunAt`); retry scheduling reuses the same run-at mechanism — one code path.
2. `Claim` → claimed, attempt incremented, lease set (default 30s). While the handler runs, the engine heartbeats `Extend` at ~lease/3 — long jobs stay claimed with zero handler ceremony. Process crash ⇒ lease expires ⇒ redelivery. **Handlers must be idempotent** (documented loudly in doc.go).
3. Handler nil → `Ack`; done jobs are not retained (history/metrics are observability concerns, not queue state).
4. Handler error/panic → engine computes retryAt via `resilience/backoff` (exponential + jitter default; per-kind override) → `Nack`. Panics are recovered per job, logged, and count as failures.
5. Attempts exhausted (default max 25) → `Kill` → dead; retained until `Requeue` (resets attempts) or `Purge`. No auto-expiry in v1.

Handler verdicts recognized by the engine: `queue.SkipRetry(err)` — straight to dead (poison input); `queue.Cancel` — discard as done (job moot). Any other error retries.

Ordering: none guaranteed (weighted claiming, retries, competing consumers). Postgres claims `ORDER BY run_at, id` — best-effort FIFO, never contractual.

Weighted-priority queues: the claim loop distributes claims across configured queues proportionally to weights (e.g. `critical:6, default:3, low:1`); `WithStrictPriority()` switches to strict order (higher queue drained first, starvation accepted by choice). Single unconfigured queue = `default:1`, zero ceremony.

Shutdown: on ctx cancel the Service stops claiming, in-flight handlers finish within supervisor's shutdown window, heartbeats stop, unfinished jobs lapse their lease and redeliver later.

## Drivers

### async/queue/postgres (pgqueue)

- One table, default `queue_jobs` (configurable prefix), shipped as a `migration.Migration` like other pg stores. Columns: `id, queue, type, payload jsonb, scope, attempt, max_attempts, run_at, claimed_until, status (pending|dead), last_error, created_at`. Index `(queue, status, run_at)`.
- Claim: `UPDATE ... SET claimed_until = now()+lease, attempt = attempt+1 WHERE id IN (SELECT id ... WHERE queue=$1 AND status='pending' AND run_at <= now() AND (claimed_until IS NULL OR claimed_until < now()) ORDER BY run_at, id LIMIT n FOR UPDATE SKIP LOCKED) RETURNING *`. Crash recovery is free — expired `claimed_until` makes the row claimable; no reaper.
- No LISTEN/NOTIFY, no listener connection: the engine's poll ticker (default 1s) drives claiming; the claim query on an indexed empty table is sub-ms, so polling load is negligible.
- Implements the `TxPusher` capability: asserts the `tx any` to `pgx.Tx` and runs the same insert inside it.

### async/queue/redis (redisqueue)

Per queue name: stream `queue:{name}`, consumer group `workers`, delay zset `queue:{name}:delayed`, dead storage `queue:{name}:dead`.

- Push: due now → `XADD`; future → `ZADD` delayed. Mover: Lua script promotes due zset members into the stream atomically; runs inside the Service's poll tick (not a separate process).
- Claim: non-blocking `XREADGROUP` (no BLOCK — pull-only rule); lease reclaim via periodic `XAUTOCLAIM` (idle > lease) on the same tick. `XPENDING` delivery count cross-checks max-attempts. Consumer name `hostname+pid+rand` so dead consumers' pending entries are safely reclaimed.
- Ack: `XACK`+`XDEL`. Nack(retryAt): add to delay zset + ack stream entry. Kill: copy to dead + ack.
- No `TxPusher` capability — `queue.PushTx` returns `ErrTxUnsupported` (documented: use `async/outbox` when it lands).

### Memory broker (built in)

Mutexed maps + min-heap on run-at; full semantics including lease expiry and DLQ. It is the reference implementation: one shared conformance suite runs against memory, postgres, and redis.

## Tenancy

- `Client` option `WithScope(func(ctx) (string, error))` captures tenant into `Job.Scope` at push. Fail-closed: hook configured + error or empty scope ⇒ `Push` fails.
- `Service` option `WithScopeContext(func(ctx, scope) context.Context)` restores scope into the handler ctx before invocation (the planned `data/tenant` carrier plugs in here).
- Single-tenant apps configure neither and pay zero ceremony. Queues are global — all tenants share worker capacity; per-tenant partitioning/fairness is explicitly out of scope.

## Observability

- Logger option defaults to `logger.NewNope()` (repo rule — packages are silent unless the app wires a logger). Single-line slog attrs per lifecycle event (claimed, done, retry scheduled, dead) with job id/type/queue/attempt/duration; errors as single-line attrs.
- `Stats(ctx)` (pending/dead per queue) feeds `ops/health` checks. No metrics dependency in v1.

## Config

`queue.Config` with env tags per repo idiom — `QUEUE_CONCURRENCY` (default 10), `QUEUE_POLL_INTERVAL` (1s), `QUEUE_LEASE` (30s), `QUEUE_MAX_ATTEMPTS` (25), `QUEUE_CLAIM_BATCH` (derived from concurrency by default) — plus `DefaultConfig()` and `Validate()`; functional options override config values.

## Error taxonomy

Package-level sentinels: `ErrInvalidConfig`, `ErrNoHandler` (dead reason), `ErrJobNotFound` (DLQ ops), `ErrScopeMissing` (fail-closed push), `ErrNotDead` (Requeue/Purge on a non-dead job), `ErrTxUnsupported` (PushTx on a broker without the `TxPusher` capability), plus `SkipRetry`/`Cancel` verdicts. Driver errors wrap broker-native errors with `%w`.

## Testing

- **Conformance suite** (exported test helpers or internal shared suite): push/claim/ack/nack/kill/extend/requeue/purge/stats, lease expiry redelivery, delayed-job due-time, attempt counting — run against all three brokers. Postgres and redis via ephemeral docker (pg16, redis7), live, not mocked, per repo precedent (resilience, rbac).
- **Engine tests** against the memory broker: retry/backoff math, verdicts, panic → Nack, per-kind overrides, weighted claim distribution (statistical over many claims), strict priority, heartbeat extends long jobs, graceful drain, scope fail-closed both directions, unregistered-kind → dead, duplicate Register panics.
- Race-enabled throughout; black-box (`queue_test` package) except where unexported state demands otherwise.
- `bench_test.go` per repo rule: push throughput, claim batch, end-to-end dispatch latency on memory broker; post-benchmark optimization pass with before/after numbers in the PR.

## Catalog updates (docs/packages.md)

- Rename `async/jobqueue` → `async/queue`; rewrite the entry: pull-only Broker, weighted-priority queues, `Kind[T]`, drop the "LISTEN/NOTIFY" wording (SKIP LOCKED + poll; optional wake capability noted as future), drivers list marks postgres+redis shipped, sqlite/nats/kafka planned.
- Update all `async/jobqueue` cross-references (`scheduler`, `eventbus`, `eventrouter`, `outbox`, `workflow`, `retention`, `comms/webhook`, `data/tenant` carrier mention, `data/settings`) to `async/queue`.

## Dependencies

`ops/supervisor`, `resilience/backoff`, `core/id`, `ops/logger` (nope default); drivers: `data/postgres`, `data/redis`, `data/migration`.

## Rejected alternatives

- **Separate `workerpool` package** — the dispatch loop is entangled with claim/lease/ack semantics; the generic remainder already exists as `resilience/parallel`. One-consumer abstraction, rejected.
- **Redis sorted-set+list scheme (Sidekiq/Asynq style)** — Streams+`XAUTOCLAIM` gives lease redelivery and delivery counts natively with less custom Lua.
- **LISTEN/NOTIFY in v1** — ~1s latency win at the cost of a listener connection lifecycle; poll is sub-ms on indexed tables. Deferred behind a future optional `Waker` capability.
- **String job names** — runtime failure modes (typo → DLQ, type drift → garbage unmarshal) are the expensive kind in an at-least-once queue; `Kind[T]` makes them compile errors for one `var` line per job type.
- **Per-tenant queues / fairness scheduling** — scheduler complexity with no current consumer; tenant rides the envelope instead.
- **Alternative multi-queue topologies** — multiple named queues are in scope; what was rejected is how workers are allocated to them: (a) one-queue-per-`Service` (isolation only by composing N services) and (b) one Service with a fixed dedicated worker budget per queue (idle budgets waste capacity while other queues are backlogged). Chosen: one shared pool claiming across all configured queues by weight, so full capacity is always used and priority is a claim-order skew; strict mode covers hard priority.
