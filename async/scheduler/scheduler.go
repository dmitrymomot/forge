package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/ops/logger"
)

// Scheduler is a supervisor.Service that enqueues a typed queue job whenever
// one of its schedules comes due. It runs on every instance; the Store claim
// makes each tick fire once per fleet. Add every job before Run.
type Scheduler struct {
	store   Store
	clk     clock.Clock
	client  *queue.Client
	log     *slog.Logger
	pushCtx func(ctx context.Context) context.Context
	names   map[string]struct{}
	name    string
	entries []*entry
	cfg     Config
	mu      sync.Mutex
	started bool
}

// entry is one scheduled job: its identity, tick source, and enqueue closure.
type entry struct {
	next  time.Time
	sched Schedule
	fire  func(ctx context.Context, scheduledFor time.Time) error
	name  string
}

// New builds a Scheduler pushing through client. The default claim store is
// in-memory — correct for a single instance; fleets pass
// WithStore(pgscheduler) so ticks dedupe across instances.
func New(client *queue.Client, opts ...Option) (*Scheduler, error) {
	if client == nil {
		return nil, fmt.Errorf("%w: nil queue client", ErrInvalidConfig)
	}
	s := &Scheduler{
		client: client,
		store:  NewMemoryStore(),
		clk:    clock.System(),
		log:    logger.NewNope(),
		names:  make(map[string]struct{}),
		name:   "scheduler",
		cfg:    DefaultConfig(),
	}
	for _, opt := range opts {
		opt(s)
	}
	if err := s.cfg.Validate(); err != nil {
		return nil, err
	}
	if s.store == nil {
		return nil, fmt.Errorf("%w: nil store", ErrInvalidConfig)
	}
	if s.clk == nil {
		return nil, fmt.Errorf("%w: nil clock", ErrInvalidConfig)
	}
	if s.log == nil {
		return nil, fmt.Errorf("%w: nil logger", ErrInvalidConfig)
	}
	if s.name == "" {
		return nil, fmt.Errorf("%w: empty service name", ErrInvalidConfig)
	}
	return s, nil
}

// Add registers a job with a fixed payload: whenever sched fires, payload is
// enqueued as kind k. The name is the schedule's identity — it keys the fleet
// claim and must be unique within this scheduler (convention:
// "domain.action", independent of the kind name so one kind can run on two
// schedules). Panics on wiring errors (empty or duplicate name, nil schedule)
// and after Run has started: jobs are package-level wiring, not runtime data.
func Add[T any](s *Scheduler, name string, sched Schedule, k queue.Kind[T], payload T, opts ...JobOption) {
	AddFunc(s, name, sched, k, func(time.Time) (T, error) { return payload, nil }, opts...)
}

// AddFunc registers a job whose payload is built at fire time: build receives
// the tick being fired (not the enqueue time — a delayed instance passes the
// tick it is catching up on), so period-keyed payloads like "report for the
// hour ending at scheduledFor" stay correct. A build error is logged and the
// tick is retried. Same wiring rules and panics as Add.
func AddFunc[T any](s *Scheduler, name string, sched Schedule, k queue.Kind[T], build func(scheduledFor time.Time) (T, error), opts ...JobOption) {
	if name == "" {
		panic("scheduler: AddFunc requires a non-empty job name")
	}
	if sched == nil {
		panic(fmt.Sprintf("scheduler: AddFunc(%q) with nil schedule", name))
	}
	if build == nil {
		panic(fmt.Sprintf("scheduler: AddFunc(%q) with nil build func", name))
	}
	var jc jobConfig
	for _, opt := range opts {
		opt(&jc)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		panic(fmt.Sprintf("scheduler: AddFunc(%q) after Run started", name))
	}
	if _, dup := s.names[name]; dup {
		panic(fmt.Sprintf("scheduler: duplicate job name %q", name))
	}
	s.names[name] = struct{}{}
	s.entries = append(s.entries, &entry{
		name:  name,
		sched: sched,
		fire: func(ctx context.Context, scheduledFor time.Time) error {
			payload, err := build(scheduledFor)
			if err != nil {
				return fmt.Errorf("scheduler: build payload for %q: %w", name, err)
			}
			return queue.Push(ctx, s.client, k, payload, jc.push...)
		},
	})
}

// Name implements supervisor.Service.
func (s *Scheduler) Name() string { return s.name }

// Run implements supervisor.Service: wait until the earliest schedule is due,
// claim-and-enqueue each due tick, repeat until ctx is cancelled. Ticks
// missed while due (instance down, timer late) are not replayed: only the
// latest due tick fires, on the assumption that a punctual instance already
// claimed the older ones. A tick whose claim or enqueue fails is retried
// every Config.RetryInterval until it succeeds, another instance claims it,
// or its next tick supersedes it.
func (s *Scheduler) Run(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return ErrAlreadyRunning
	}
	s.started = true
	active := slices.Clone(s.entries)
	s.mu.Unlock()
	if len(active) == 0 {
		return ErrNoJobs
	}

	// Claims and pushes must commit during drain; the select below is the
	// cancellation point.
	opCtx := context.WithoutCancel(ctx)

	now := s.clk.Now()
	kept := active[:0]
	for _, e := range active {
		e.next = e.sched.Next(now)
		if e.next.IsZero() {
			s.log.WarnContext(ctx, "schedule never fires, job parked", slog.String("service", s.name), slog.String("job", e.name))
			continue
		}
		kept = append(kept, e)
	}
	active = kept

	var sweepC <-chan time.Time
	if s.cfg.SweepInterval > 0 {
		ticker := time.NewTicker(s.cfg.SweepInterval)
		defer ticker.Stop()
		sweepC = ticker.C
	}

	s.log.InfoContext(ctx, "scheduler started", slog.String("service", s.name), slog.Int("jobs", len(active)))

	for {
		now = s.clk.Now()
		retry := false
		kept := active[:0]
		for _, e := range active {
			if e.next.After(now) {
				kept = append(kept, e)
				continue
			}
			// Skip past missed ticks: fire only the latest due one. The walk
			// leaves next as the first tick strictly after now (or zero).
			tick, next := e.next, e.sched.Next(e.next)
			for !next.IsZero() && !next.After(now) {
				tick, next = next, e.sched.Next(next)
			}
			if s.fireTick(ctx, opCtx, e, tick) {
				if next.IsZero() {
					s.log.WarnContext(ctx, "schedule exhausted, job parked", slog.String("service", s.name), slog.String("job", e.name))
					continue
				}
				e.next = next
			} else {
				e.next = tick
				retry = true
			}
			kept = append(kept, e)
		}
		active = kept

		if len(active) == 0 && sweepC == nil {
			<-ctx.Done()
			return ctx.Err()
		}
		timer := time.NewTimer(s.waitFor(active, now, retry))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		case <-sweepC:
			timer.Stop()
			s.sweep(ctx, opCtx)
		}
	}
}

// fireTick claims and enqueues one tick. It reports whether the entry may
// advance to its next tick: true on success, on a tick already claimed
// elsewhere, and on a claim stuck after a failed release (retrying would only
// hit ErrAlreadyClaimed); false asks the run loop to retry the same tick.
func (s *Scheduler) fireTick(ctx, opCtx context.Context, e *entry, tick time.Time) bool {
	pctx := opCtx
	if s.pushCtx != nil {
		pctx = s.pushCtx(opCtx)
	}
	// One deadline over claim+enqueue+release: opCtx survives shutdown, so
	// this is the only bound keeping a wedged backend from blocking drain.
	pctx, cancel := context.WithTimeout(pctx, s.cfg.OpTimeout)
	defer cancel()
	tick = tick.UTC()
	attrs := []slog.Attr{slog.String("service", s.name), slog.String("job", e.name), slog.Time("scheduled_for", tick)}
	switch err := s.store.Claim(pctx, e.name, tick); {
	case errors.Is(err, ErrAlreadyClaimed):
		s.log.LogAttrs(ctx, slog.LevelDebug, "tick claimed elsewhere", attrs...)
		return true
	case err != nil:
		s.log.LogAttrs(ctx, slog.LevelError, "tick claim failed", append(attrs, slog.Any("error", err))...)
		return false
	}
	if err := e.fire(pctx, tick); err != nil {
		s.log.LogAttrs(ctx, slog.LevelError, "scheduled enqueue failed", append(attrs, slog.Any("error", err))...)
		if rerr := s.store.Release(pctx, e.name, tick); rerr != nil {
			s.log.LogAttrs(ctx, slog.LevelError, "claim release failed, tick lost", append(attrs, slog.Any("error", rerr))...)
			return true
		}
		return false
	}
	s.log.LogAttrs(ctx, slog.LevelDebug, "scheduled job enqueued", attrs...)
	return true
}

// waitFor picks the sleep until the next actionable moment: the earliest
// upcoming tick, RetryInterval when a failed tick is pending, and a bounded
// park when every remaining entry is parked.
func (s *Scheduler) waitFor(active []*entry, now time.Time, retry bool) time.Duration {
	wait := time.Hour
	for _, e := range active {
		if d := e.next.Sub(now); d > 0 {
			wait = min(wait, d)
		}
	}
	if retry {
		wait = min(wait, s.cfg.RetryInterval)
	}
	return wait
}

// sweep deletes claims older than the retention window.
func (s *Scheduler) sweep(ctx, opCtx context.Context) {
	opCtx, cancel := context.WithTimeout(opCtx, s.cfg.OpTimeout)
	defer cancel()
	cutoff := s.clk.Now().UTC().Add(-s.cfg.Retention)
	n, err := s.store.PurgeBefore(opCtx, cutoff)
	if err != nil {
		s.log.ErrorContext(ctx, "claim sweep failed", slog.String("service", s.name), slog.Any("error", err))
		return
	}
	if n > 0 {
		s.log.DebugContext(ctx, "claims swept", slog.String("service", s.name), slog.Int("purged", n))
	}
}
