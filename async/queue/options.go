package queue

import (
	"context"
	"log/slog"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/backoff"
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
	runAt       time.Time
	queue       string
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

// WithHandlerTimeout bounds each invocation of this kind; expiry counts as a
// failure and takes the retry path. Overrides Config.HandlerTimeout;
// WithHandlerTimeout(0) disables the timeout entirely for kinds that
// legitimately run long.
func WithHandlerTimeout(d time.Duration) HandlerOption {
	return func(h *handler) { h.timeout, h.timeoutSet = d, true }
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
