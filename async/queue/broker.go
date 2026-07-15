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
