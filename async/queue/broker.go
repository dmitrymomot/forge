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
