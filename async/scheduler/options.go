package scheduler

import (
	"context"
	"log/slog"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/core/clock"
)

// Option configures New.
type Option func(*Scheduler)

// WithStore replaces the claim store (default NewMemoryStore). A multi-instance
// fleet must share a durable store — async/scheduler/postgres — or every
// instance fires every tick.
func WithStore(st Store) Option {
	return func(s *Scheduler) { s.store = st }
}

// WithConfig replaces the Config (validated by New).
func WithConfig(cfg Config) Option {
	return func(s *Scheduler) { s.cfg = cfg }
}

// WithName overrides the supervisor service name (default "scheduler");
// required when running multiple Scheduler instances under one supervisor.
func WithName(name string) Option {
	return func(s *Scheduler) { s.name = name }
}

// WithLogger sets the logger (default logger.NewNope()).
func WithLogger(l *slog.Logger) Option {
	return func(s *Scheduler) { s.log = l }
}

// WithClock injects a clock used for tick computation and sweep cutoffs
// (tests). Timers stay real time.
func WithClock(clk clock.Clock) Option {
	return func(s *Scheduler) { s.clk = clk }
}

// WithPushContext installs a hook that prepares the context every claim and
// enqueue runs under — the tenancy seam for a scope-configured queue.Client:
// inject the system tenant here so the client's scope hook resolves instead
// of failing closed on the scheduler's background context. Single-tenant apps
// do not configure it.
func WithPushContext(fn func(ctx context.Context) context.Context) Option {
	return func(s *Scheduler) { s.pushCtx = fn }
}

// JobOption configures a single Add or AddFunc call.
type JobOption func(*jobConfig)

type jobConfig struct {
	push []queue.PushOption
}

// WithPushOptions forwards queue push options (queue.WithQueue,
// queue.WithMaxAttempts) to every job this schedule enqueues. Scheduling
// options (queue.WithDelay, queue.WithRunAt) also apply verbatim — the
// scheduler already decides *when*, so combining them is rarely meaningful.
func WithPushOptions(opts ...queue.PushOption) JobOption {
	return func(c *jobConfig) { c.push = append(c.push, opts...) }
}
