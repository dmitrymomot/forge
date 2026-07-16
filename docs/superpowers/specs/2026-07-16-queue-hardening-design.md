# async/queue hardening & performance rework — design

Date: 2026-07-16. Status: approved for planning.

## Context & goals

`async/queue` (shipped in PR #61) is the durable job engine every other part of the application stack will build on. Two review passes found no critical bugs but a set of robustness findings, production bottlenecks, and silent-failure modes. This rework fixes **all** of them, plus reworks the Postgres schema for optimal sustained performance.

Constraints decided up front:

- **Clean slate**: nothing is deployed. The existing migration is rewritten in place; the `Broker` interface breaks freely. No compatibility shims.
- Job IDs move from ULID-in-text to **UUIDv7** (`core/id.NewUUID()`) in native `uuid` columns — 16-byte sortable keys, append-friendly B-trees.
- **Full lease fencing** (claim tokens, `ErrLeaseLost`): duplicates shrink to true-crash cases only.
- **Bounded Stats**: counts are exact up to a cap (Postgres cap 10,000), never O(table).
- **DLQ retention**: engine-driven sweep, default **30 days**, `0` = keep forever.
- **Default handler timeout**: **10 minutes**, per-kind override, explicit opt-out.

## Issue inventory this design resolves

Correctness/robustness (review round 1):

1. Redis: undecodable stream entry wedges Claim forever → poison parking.
2. No lease-ownership fencing on Ack/Nack/Kill/Extend (all brokers) → claim tokens.
3. pg Claim relies on unguaranteed `UPDATE ... RETURNING` order → CTE + outer `ORDER BY`.
4. Redis `ListDead` is `HGETALL` O(DLQ) → ZSET index, O(limit).
5. Redis consumer names accumulate forever → `Maintainer` cleanup.

Production concerns (review round 2):

6. pg `Stats` full-table scan wired into health checks → bounded index-only counts.
7. pg queue-table bloat; dead rows share the hot table/index forever → two-table schema, HOT-friendly columns, fillfactor, autovacuum tuning.
8. Hung handler with no timeout silently leaks a worker slot forever → default `HandlerTimeout`.
9. No backoff on claim errors during broker outage → exponential poll backoff.
10. No bulk enqueue → variadic `Push`, `PushMany`, batch SQL/pipelines.
11. Redis `Stats` probes every queue ever seen → stale-queue cleanup in `Maintainer`.
12. MemoryBroker Claim is O(all jobs, all queues) → per-queue buckets.
13. DLQ unbounded retention → `PurgeDeadBefore` + `Config.DeadRetention`.

Minors: idle double-claim sweep, no Service clock seam, `WithQueue("")` accepted silently.

## 1. Postgres schema & SQL

Two tables replace the single `queue_jobs`. The existing migration file is rewritten (same filename).

```sql
CREATE TABLE queue_jobs (            -- hot: pending + claimed only
    id            uuid PRIMARY KEY,  -- UUIDv7
    claim_token   uuid,              -- NULL when unclaimed
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

CREATE TABLE queue_jobs_dead (       -- cold DLQ
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
CREATE INDEX queue_jobs_dead_list_idx  ON queue_jobs_dead (queue, died_at);
CREATE INDEX queue_jobs_dead_sweep_idx ON queue_jobs_dead (died_at);
```

Rationale:

- **No `status` column** — liveness is table membership. The hot table holds only the working set; vacuum keeps up; the claim index stays tiny regardless of DLQ size.
- **`claimed_until`/`claim_token`/`attempt`/`last_error` deliberately un-indexed** so claim/extend/ack-path updates are HOT-eligible (with `fillfactor=90`); only `Nack` (touches `run_at`) writes the index.
- **`payload json`, not `jsonb`** — broker never queries into payloads; `json` stores validated text verbatim (no decompose/rebuild CPU); ops can still cast to `jsonb` ad hoc. Client-side validation already guarantees valid JSON.
- Fixed-width columns first for alignment; UUIDv7 keeps inserts append-mostly.

SQL operations (all statements pre-built in `New` as today):

- **Claim**: `WITH claimed AS (UPDATE ... SET claimed_until = now() + $3, claim_token = $4, attempt = attempt + 1 WHERE id IN (SELECT id ... WHERE queue = $1 AND run_at <= now() AND (claimed_until IS NULL OR claimed_until < now()) ORDER BY run_at, id LIMIT $2 FOR UPDATE SKIP LOCKED) RETURNING ...) SELECT ... FROM claimed ORDER BY run_at, id`. One client-generated UUIDv7 token per Claim call, shared by the batch (fencing checks `id AND token`, so sharing is safe). The outer SELECT fixes the unguaranteed-RETURNING-order bug.
- **Ack**: `DELETE ... WHERE id = $1 AND claim_token = $2`; 0 rows → `ErrLeaseLost` (or already finalized — indistinguishable and equivalent for the caller).
- **Nack**: `UPDATE ... SET run_at = $3, claimed_until = NULL, claim_token = NULL, last_error = $4 WHERE id = $1 AND claim_token = $2`; 0 rows → `ErrLeaseLost`.
- **Extend**: `UPDATE ... SET claimed_until = now() + $3 WHERE id = $1 AND claim_token = $2`; 0 rows → `ErrLeaseLost`.
- **Kill**: single CTE row-move: `WITH d AS (DELETE FROM queue_jobs WHERE id = $1 AND claim_token = $2 RETURNING ...) INSERT INTO queue_jobs_dead (..., died_at) SELECT ..., now() FROM d`; 0 rows → `ErrLeaseLost`.
- **Requeue** (unfenced, DLQ op): CTE move back with `attempt = 0`, `run_at = now()`, `last_error` preserved; 0 rows → exists-check in hot table → `ErrNotDead` : `ErrJobNotFound`.
- **Purge** (unfenced): `DELETE FROM queue_jobs_dead WHERE id = $1`; 0 rows → same disambiguation.
- **Push (batch)**: single `INSERT ... SELECT unnest($1::uuid[], $2::text[], ...)` — atomic, one round trip for any N. `CopyFrom` above a size threshold only if benchmarks justify it.
- **Stats**: loose index scan (recursive CTE) enumerates distinct queues from `queue_jobs_claim_idx`, then a `LATERAL` bounded count per queue: `(SELECT count(*) FROM (SELECT 1 FROM queue_jobs j WHERE j.queue = q.queue LIMIT 10001) t)` — index-only, O(cap) worst case. Same pattern on the dead table. Cap = 10,000; above it the count reports the cap with the capped flag set.
- **PurgeDeadBefore**: `DELETE FROM queue_jobs_dead WHERE died_at < $1` (sweep index), returns affected count.

Go-side `Job.ID` and tokens remain `string`; pgx binds them to `uuid` natively.

## 2. Broker v2 contract

```go
type Broker interface {
    Push(ctx context.Context, jobs ...Job) error                  // atomic batch; no-op on empty
    Claim(ctx context.Context, queue string, n int, lease time.Duration) ([]ClaimedJob, error)
    Extend(ctx context.Context, id, token string, lease time.Duration) error
    Ack(ctx context.Context, id, token string) error
    Nack(ctx context.Context, id, token string, retryAt time.Time, reason string) error
    Kill(ctx context.Context, id, token string, reason string) error
    ListDead(ctx context.Context, queue string, limit int) ([]Job, error)  // ordered by died_at, id
    Requeue(ctx context.Context, id string) error                 // DLQ ops: human-driven, unfenced
    Purge(ctx context.Context, id string) error
    PurgeDeadBefore(ctx context.Context, cutoff time.Time) (int, error)
    Stats(ctx context.Context) (Stats, error)
}

type ClaimedJob struct {
    Job
    Token string // opaque fencing token, valid for this claim only
}

type TxPusher interface { PushTx(ctx context.Context, tx any, jobs ...Job) error }
type Maintainer interface { Maintain(ctx context.Context) error } // optional housekeeping
```

- New sentinel **`ErrLeaseLost`**: returned by the four fenced ops when the token no longer owns the job. Conformance requirement: after lease expiry and a successful re-claim, every fenced op with the stale token returns `ErrLeaseLost` and must not disturb the new claim's state.
- `QueueStats` gains `PendingCapped, DeadCapped bool`. Only backends that bound counts set them (pg); memory and redis report exact counts.
- `ListDead` ordering changes from `created_at` to **`died_at`** (kill time), ties by id — one redis ZSET serves both listing and retention, and recency-of-death is the more useful ops ordering.
- `Requeue`/`Purge`/`ListDead` stay unfenced: dead jobs have no lease.
- Batch `Push` is all-or-nothing on pg (single statement) and redis (`TxPipeline`); memory trivially atomic under its mutex.

## 3. Engine (Service, Client, Config)

Config additions (env-loadable):

| Knob | Default | Semantics |
|---|---|---|
| `HandlerTimeout` | 10m | Deadline for every handler without a per-kind setting. `WithHandlerTimeout(d)` overrides per kind; `WithHandlerTimeout(0)` disables for that kind (set-flag internally, so unset ≠ 0). Expiry takes the normal retry path. |
| `DeadRetention` | 720h (30d) | Sweep horizon for `PurgeDeadBefore`. `0` = keep forever. Negative = invalid. |

Run-loop changes:

- **Claim-error backoff**: when a poll round's claims all fail, the wait before the next poll doubles (from `PollInterval`, capped at `max(30s, PollInterval)`); first successful claim resets it. Protects a recovering broker from full-cadence fleet hammering and cuts error-log spam.
- **Idle sweep fix**: the leftover sweep runs only if the SWRR pass claimed > 0, and re-visits only queues that returned exactly the asked amount (a queue returning fewer is proven drained). Idle cost drops from 2× to 1× claims/poll.
- **Sweep ticker** (~5m, jittered): calls `Maintain` if the broker implements `Maintainer`; calls `PurgeDeadBefore(clk.Now() − DeadRetention)` when `DeadRetention > 0`, logging nonzero purge counts. Runs on every instance — both ops are idempotent and cheap; no leader election.

process() changes:

- Handler context is cancellable. When the heartbeat's `Extend` returns `ErrLeaseLost`, it cancels the handler context and stops heartbeating — the slot frees as soon as the handler observes cancellation instead of finishing doomed work.
- `finalize` distinguishes `ErrLeaseLost` (warn: "lease lost, job owned elsewhere" — expected, dropped) from other broker-op failures (error: "will redeliver after lease expiry").
- `retryAt` and the retention cutoff come from an injectable `clock.Clock` on the Service (`WithServiceClock`, mirrors `WithClientClock`). `core/clock` is `Now()`-only, so tickers stay stdlib; timing tests keep using short real intervals per the brokertest discipline.

Client changes:

- **`PushMany[T](ctx, c, k, payloads []T, opts ...PushOption) error`** — one scope resolution, one option parse, N envelopes (each its own UUIDv7), single variadic `broker.Push`. `Push` becomes a thin wrapper. Empty slice is a no-op.
- `buildJob` rejects empty queue names (`WithQueue("")`) with an error.
- `PushRaw` unchanged (single-job escape hatch).

Unchanged: SWRR priority + strict mode, verdicts (`Cancel`, `SkipRetry`), attempt precedence (per-job > per-kind > config), panic recovery, `WithoutCancel` drain semantics, supervisor integration.

## 4. Redis driver

- **Fencing**: claimed-ref map stores the per-claim token. Finalize ops check the token locally first (mismatch/absence → `ErrLeaseLost`; covers same-instance re-claims), then run a Lua script that atomically verifies PEL ownership (`XPENDING` for the message must name this consumer) before `XACK`/`XDEL` + outcome writes. Closes the "XACK succeeds regardless of owner" hole.
- **Dead storage = hash + ZSET**: envelopes in `:dead` hash; `:dead:idx` ZSET scored by kill-time ms. `ListDead` = `ZRANGE ... LIMIT` + `HMGET` (O(limit)). `PurgeDeadBefore` = `ZRANGEBYSCORE` + batched `HDEL`/`ZREM`/index cleanup.
- **Poison parking**: on decode failure in Claim, a Lua script atomically moves the raw entry to `<prefix><queue>:poison` (list) and acks/deletes it from the stream; Claim logs and continues. The queue never wedges on a bad entry (foreign XADD, future wire version). Poison key is documented for manual ops, not part of the Broker API.
- **`Maintain`**: per queue — `XINFO CONSUMERS`; `XGROUP DELCONSUMER` for consumers with 0 pending and idle > 1h (fixes unbounded consumer accumulation). Queues-set hygiene: `SREM` members whose stream, delayed ZSET, and dead hash are all empty (fixes Stats probing retired queues; a later push re-`SAdd`s).
- **`Requeue`/`Purge` become Lua scripts** — the read-check-move sequences turn atomic, eliminating the concurrent double-requeue duplicate.
- Unchanged: delayed-promotion script (128/claim cap), XAUTOCLAIM-first claim order, `XPendingExt` crash-redelivery attempt accounting, exact O(queues) Stats, per-instance consumer naming.

## 5. Memory broker, brokertest, testing, benchmarks

**MemoryBroker**: per-queue buckets (`map[queue]` → live map + dead map) mirroring the two-table shape; Claim scans one queue's live jobs only. Token field compare for fencing; `diedAt` recorded on Kill. Stays the readable reference implementation.

**brokertest additions**: fencing conformance (stale token → `ErrLeaseLost` on all four ops, new claim undisturbed, new token still acks); batch Push claims back in `run_at, id` order; `ListDead` ordered by kill time; `PurgeDeadBefore` cutoff behavior. Existing timing discipline kept (poll-based assertions, `dueNow()` past-bias). Redis package keeps its own two-instance cross-worker-theft test.

**Engine unit tests**: `ErrLeaseLost` finalize path; heartbeat cancels handler ctx on lease loss; claim-error backoff widen/reset; idle-sweep skip; `HandlerTimeout` default vs `WithHandlerTimeout(0)`; sweep ticker calls `PurgeDeadBefore`/`Maintain`; `PushMany` single scope resolution + per-job IDs; empty queue name rejection. Capped-Stats path: pg integration test pushes 10,001 jobs via `PushMany` and asserts `Pending == 10000, PendingCapped == true`.

**Tiers unchanged**: unit = memory/engine (Docker-free default), `//go:build integration` = pg/redis via testkit containers, per-process-unique redis prefix.

**Benchmarks** (before/after in PR, per repo rule): memory benches rerun on the new layout; `PushMany` at N ∈ {1, 100, 10k} on all three brokers; pg claim-cycle before/after schema rework; redis `ListDead` before/after ZSET index. Post-bench optimization pass, measured wins only (e.g. `CopyFrom` threshold decided by data).

**Docs**: doc.go delivery contract gains the fencing story ("duplicates only on true crash"), the 30d retention default (behavioral change), `PushMany` usage; redis package doc documents the poison key; `docs/packages.md` untouched (package already cataloged).

## Out of scope

- Metrics/observability seam beyond structured logs.
- Job priorities within a queue, cron/periodic scheduling, workflow chaining.
- Postgres partitioning (rejected: two-partition ceremony for no gain over two tables).
- Exact unbounded Stats or counter tables (rejected: hot-row contention).
- Automatic poison-entry replay tooling.

## Migration & compatibility notes

- The rewritten migration keeps the same filename/version (nothing has run it). `data/migration` applies it fresh.
- All `Broker` implementors (three in-repo) and brokertest update in lockstep; no external implementors exist.
- Producer/worker app code is source-compatible except: `QueueStats` field additions, `ListDead` ordering change, and new config defaults (10m handler timeout, 30d DLQ retention) — each called out in doc.go.
