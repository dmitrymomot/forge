package outbox

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/ops/logger"
	"github.com/dmitrymomot/forge/resilience/backoff"
)

// Relay is the forwarding half of the outbox: a supervisor.Service that
// claims committed rows from the Store in batches and pushes them into the
// broker, deleting each row once its push succeeds. Rows are never dropped —
// a failed push reschedules the row with capped exponential backoff, so a
// broker outage resolves by draining the backlog when the broker returns.
// Run any number of instances; claims are leased, and contention costs at
// worst a duplicate push.
type Relay struct {
	store  Store
	broker queue.Broker
	clk    clock.Clock
	log    *slog.Logger
	bo     backoff.Backoff
	name   string
	cfg    Config
}

// NewRelay builds a relay over store and broker. Returns ErrInvalidConfig on
// nil store, nil broker, or an invalid Config.
func NewRelay(store Store, broker queue.Broker, opts ...Option) (*Relay, error) {
	r := &Relay{
		store:  store,
		broker: broker,
		cfg:    DefaultConfig(),
		name:   "outbox",
		log:    logger.NewNope(),
		clk:    clock.System(),
		bo:     backoff.Exponential(5*time.Second, 5*time.Minute, backoff.WithJitter(0.2)),
	}
	for _, opt := range opts {
		opt(r)
	}
	if store == nil {
		return nil, fmt.Errorf("%w: nil store", ErrInvalidConfig)
	}
	if broker == nil {
		return nil, fmt.Errorf("%w: nil broker", ErrInvalidConfig)
	}
	if err := r.cfg.Validate(); err != nil {
		return nil, err
	}
	return r, nil
}

// Name implements supervisor.Service.
func (r *Relay) Name() string { return r.name }

// Run implements supervisor.Service: claim, forward, delete until ctx is
// cancelled. A cycle that claimed a full batch loops immediately (backlog
// draining); claim or full-cycle push failure widens the poll interval
// instead of hammering a down store or broker.
func (r *Relay) Run(ctx context.Context) error {
	opCtx := context.WithoutCancel(ctx) // post-claim ops must finish during drain
	r.log.InfoContext(ctx, "outbox relay started", slog.String("service", r.name), slog.Int("batch", r.cfg.BatchSize))

	maxWait := max(30*time.Second, r.cfg.PollInterval)
	wait := r.cfg.PollInterval
	for {
		claimed, err := r.relayOnce(ctx, opCtx)
		if ctx.Err() != nil {
			break
		}
		if err != nil {
			wait = min(wait*2, maxWait)
			r.log.ErrorContext(ctx, "outbox relay cycle failed", slog.String("service", r.name), slog.Any("error", err))
		} else {
			wait = r.cfg.PollInterval
			if claimed == r.cfg.BatchSize {
				continue // full batch: assume backlog, keep draining
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(wait):
			continue
		}
		break
	}
	r.log.InfoContext(opCtx, "outbox relay stopped", slog.String("service", r.name))
	return ctx.Err()
}

// relayOnce runs one claim-forward-delete cycle. It returns how many rows
// were claimed and an error only when the cycle made no progress at all —
// claim failed, or every claimed row failed to push — the signal Run uses to
// widen its poll.
func (r *Relay) relayOnce(ctx context.Context, opCtx context.Context) (int, error) {
	entries, err := r.store.Claim(ctx, r.cfg.BatchSize, r.cfg.Lease)
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, fmt.Errorf("outbox: claim: %w", err)
	}
	if len(entries) == 0 {
		return 0, nil
	}

	jobs := make([]queue.Job, len(entries))
	for i, e := range entries {
		jobs[i] = e.Job
	}
	if err := r.broker.Push(opCtx, jobs...); err == nil {
		r.deleteForwarded(opCtx, entries)
		return len(entries), nil
	}

	// Push is all-or-nothing, so one poison row fails the whole batch: retry
	// rows individually to let the healthy ones through and isolate failures.
	forwarded := make([]Entry, 0, len(entries))
	failed := 0
	for _, e := range entries {
		if err := r.broker.Push(opCtx, e.Job); err != nil {
			failed++
			retryAt := r.clk.Now().UTC().Add(r.bo.Next(e.Attempts))
			r.log.ErrorContext(opCtx, "outbox push failed",
				slog.String("service", r.name), slog.String("job_id", e.Job.ID), slog.String("kind", e.Job.Type),
				slog.Int("attempts", e.Attempts), slog.Time("retry_at", retryAt), slog.Any("error", err))
			if ferr := r.store.Fail(opCtx, e.Job.ID, retryAt, err.Error()); ferr != nil {
				// Lease expiry will make the row claimable again; only backoff is lost.
				r.log.ErrorContext(opCtx, "outbox fail-reschedule failed",
					slog.String("service", r.name), slog.String("job_id", e.Job.ID), slog.Any("error", ferr))
			}
			continue
		}
		forwarded = append(forwarded, e)
	}
	r.deleteForwarded(opCtx, forwarded)
	if failed == len(entries) {
		return len(entries), fmt.Errorf("outbox: push: all %d claimed rows failed", failed)
	}
	return len(entries), nil
}

// deleteForwarded removes successfully pushed rows. A failed delete is logged
// and dropped: the lease expires, the rows redeliver, and the duplicate push
// is absorbed by the at-least-once contract downstream.
func (r *Relay) deleteForwarded(ctx context.Context, entries []Entry) {
	if len(entries) == 0 {
		return
	}
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.Job.ID
	}
	if err := r.store.Delete(ctx, ids...); err != nil {
		r.log.ErrorContext(ctx, "outbox delete failed, rows will redeliver after lease expiry",
			slog.String("service", r.name), slog.Int("rows", len(ids)), slog.Any("error", err))
		return
	}
	r.log.DebugContext(ctx, "outbox forwarded", slog.String("service", r.name), slog.Int("rows", len(ids)))
}
