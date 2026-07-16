package queue

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/ops/logger"
	"github.com/dmitrymomot/forge/resilience/backoff"
)

// Service is the worker: a supervisor.Service that claims jobs from its
// configured queues and dispatches them to registered handlers on a bounded
// pool. Register all kinds before Run; Register is not safe to call
// concurrently with Run.
type Service struct {
	broker         Broker
	defaultBackoff backoff.Backoff
	clk            clock.Clock
	queueWeights   map[string]int
	log            *slog.Logger
	scopeCtx       func(ctx context.Context, scope string) context.Context
	handlers       map[string]*handler
	name           string
	queues         []weightedQueue // weight desc, name asc; built by NewService
	cfg            Config
	sweepEvery     time.Duration
	strict         bool
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
	timeoutSet  bool
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
		clk:            clock.System(),
		sweepEvery:     5 * time.Minute,
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

	s.log.InfoContext(ctx, "queue service started", slog.String("service", s.name), slog.Int("concurrency", s.cfg.Concurrency))

	maintainer, hasMaintain := s.broker.(Maintainer)
	if hasMaintain || s.cfg.DeadRetention > 0 {
		wg.Go(func() { s.sweepLoop(ctx, maintainer) })
	}

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
	s.log.InfoContext(opCtx, "queue service draining", slog.String("service", s.name))
	wg.Wait()
	s.log.InfoContext(opCtx, "queue service stopped", slog.String("service", s.name))
	return ctx.Err()
}

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
	if free > 0 && errored == 0 { // leftover sweep only with capacity to spare and a healthy broker this round
		for _, q := range s.queues {
			if !drained[q.name] {
				claim(q.name, free)
			}
		}
	}
	return total, attempted > 0 && errored == attempted
}

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

// process runs one claimed job to a terminal broker state. opCtx is never
// cancelled by shutdown: in-flight completions must still commit.
func (s *Service) process(opCtx context.Context, cj ClaimedJob) {
	job := cj.Job
	logAttrs := []any{
		slog.String("service", s.name), slog.String("job_id", job.ID),
		slog.String("kind", job.Type), slog.String("queue", job.Queue), slog.Int("attempt", job.Attempt),
	}
	h, ok := s.handlers[job.Type]
	if !ok {
		s.finalize(opCtx, "dead", logAttrs, func() error {
			return s.broker.Kill(opCtx, job.ID, cj.Token, ErrNoHandler.Error()+": "+job.Type)
		})
		return
	}
	if s.scopeCtx != nil && job.Scope == "" {
		s.finalize(opCtx, "dead", logAttrs, func() error {
			return s.broker.Kill(opCtx, job.ID, cj.Token, ErrScopeMissing.Error())
		})
		return
	}

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

	timeout := s.cfg.HandlerTimeout
	if h.timeoutSet {
		timeout = h.timeout
	}
	var cancel context.CancelFunc = func() {}
	if timeout > 0 {
		hctx, cancel = context.WithTimeout(hctx, timeout)
	}
	start := time.Now()
	err := s.invoke(hctx, h, job)
	cancel()
	cancelHandler()
	stopHB()
	hbWG.Wait()
	logAttrs = append(logAttrs, slog.Duration("duration", time.Since(start)))

	switch {
	case err == nil:
		s.finalize(opCtx, "done", logAttrs, func() error { return s.broker.Ack(opCtx, job.ID, cj.Token) })
	case errors.Is(err, Cancel):
		s.finalize(opCtx, "cancelled", logAttrs, func() error { return s.broker.Ack(opCtx, job.ID, cj.Token) })
	case IsSkipRetry(err):
		logAttrs = append(logAttrs, slog.Any("error", err))
		s.finalize(opCtx, "dead", logAttrs, func() error { return s.broker.Kill(opCtx, job.ID, cj.Token, err.Error()) })
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
			s.finalize(opCtx, "dead", logAttrs, func() error { return s.broker.Kill(opCtx, job.ID, cj.Token, err.Error()) })
			return
		}
		bo := s.defaultBackoff
		if h.backoff != nil {
			bo = h.backoff
		}
		retryAt := s.clk.Now().UTC().Add(bo.Next(job.Attempt))
		logAttrs = append(logAttrs, slog.Time("retry_at", retryAt))
		s.finalize(opCtx, "retry", logAttrs, func() error { return s.broker.Nack(opCtx, job.ID, cj.Token, retryAt, err.Error()) })
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
