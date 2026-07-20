package workflow

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/backoff"
)

// Option configures NewEngine.
type Option func(*Engine)

// WithScope installs the tenancy hook: it is called on every Start and its
// result is captured into the run and its driving job. Fail-closed: once
// configured, a hook error or empty scope makes Start fail with
// ErrScopeMissing. Single-tenant apps simply do not configure it. Restore the
// scope on the worker side by passing queue.WithScopeContext to NewService.
func WithScope(fn func(ctx context.Context) (string, error)) Option {
	return func(e *Engine) { e.scope = fn }
}

// WithClock injects the clock stamping run timestamps (tests).
func WithClock(clk clock.Clock) Option {
	return func(e *Engine) { e.clk = clk }
}

// WithLogger sets the logger for run outcomes (completed, failed,
// compensation exhausted). Defaults to a no-op logger.
func WithLogger(l *slog.Logger) Option {
	return func(e *Engine) {
		if l != nil {
			e.log = l
		}
	}
}

// WithStepAttempts sets the default per-step attempt budget applied when a
// Step leaves MaxAttempts at 0 (default 5): after n failed attempts a step's
// failure turns permanent and compensation begins. Panics on n <= 0 — engine
// options are startup wiring.
func WithStepAttempts(n int) Option {
	if n <= 0 {
		panic(fmt.Sprintf("workflow: WithStepAttempts requires n > 0, got %d", n))
	}
	return func(e *Engine) { e.stepAttempts = n }
}

// RegisterOption configures a single workflow registration. All of them tune
// the worker's dispatch of that workflow's driving jobs.
type RegisterOption func(*registration)

// WithRetryBackoff overrides the worker's default backoff for this workflow's
// transient retries.
func WithRetryBackoff(b backoff.Backoff) RegisterOption {
	qo := queue.WithHandlerBackoff(b)
	return func(r *registration) { r.hopts = append(r.hopts, qo) }
}

// WithHandlerTimeout bounds each driving-job invocation — which may span many
// steps — overriding the worker's Config.HandlerTimeout (default 10m). Expiry
// is a transient failure: the run resumes from its checkpoint on retry. 0
// disables the worker default for long workflows; panics on a negative
// duration.
func WithHandlerTimeout(d time.Duration) RegisterOption {
	qo := queue.WithHandlerTimeout(d)
	return func(r *registration) { r.hopts = append(r.hopts, qo) }
}

// StartOption configures a single Start call.
type StartOption func(*startConfig)

type startConfig struct {
	runID string
}

// WithRunID sets the run id instead of generating one. Use a business key
// ("onboard:"+userID) to make Start idempotent: a second Start with the same
// id fails with ErrRunAlreadyExists. An empty id falls back to a generated
// one.
func WithRunID(id string) StartOption {
	return func(c *startConfig) { c.runID = id }
}
