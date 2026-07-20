package eventbus

import (
	"context"
	"time"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/backoff"
)

// Option configures New and NewSync.
type Option func(*Bus)

// WithScope installs the tenancy hook: it is called on every publish and its
// result is captured into each fanned-out job's scope. Fail-closed: once
// configured, a hook error or empty scope makes the publish fail with
// ErrScopeMissing — on the sync bus too, so dev catches a missing tenant
// context exactly like production. Single-tenant apps simply do not configure
// it. Restore the scope on the worker side by passing queue.WithScopeContext
// to NewService.
func WithScope(fn func(ctx context.Context) (string, error)) Option {
	return func(b *Bus) { b.scope = fn }
}

// WithClock injects a clock stamping Delivery.OccurredAt (tests).
func WithClock(clk clock.Clock) Option {
	return func(b *Bus) { b.clk = clk }
}

// SubscribeOption configures a single subscription. All of them tune the
// durable worker's dispatch and are ignored by the sync bus, where the
// publisher's call is the only attempt.
type SubscribeOption func(*subscription)

// WithMaxAttempts sets this subscription's delivery attempt budget
// (overrides the worker's Config.MaxAttempts); once spent, the event
// dead-letters on this subscription only.
func WithMaxAttempts(n int) SubscribeOption {
	qo := queue.WithHandlerMaxAttempts(n)
	return func(s *subscription) { s.hopts = append(s.hopts, qo) }
}

// WithRetryBackoff overrides the worker's default retry backoff for this
// subscription.
func WithRetryBackoff(b backoff.Backoff) SubscribeOption {
	qo := queue.WithHandlerBackoff(b)
	return func(s *subscription) { s.hopts = append(s.hopts, qo) }
}

// WithHandlerTimeout bounds each durable invocation of this subscription's
// handler; expiry counts as a failure and takes the retry path. 0 disables
// the worker's default timeout; panics on a negative duration.
func WithHandlerTimeout(d time.Duration) SubscribeOption {
	qo := queue.WithHandlerTimeout(d)
	return func(s *subscription) { s.hopts = append(s.hopts, qo) }
}
