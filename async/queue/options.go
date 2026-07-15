package queue

import (
	"context"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
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
