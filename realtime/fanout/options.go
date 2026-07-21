package fanout

import (
	"context"
	"fmt"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

const (
	defaultBuffer    = 64
	defaultReplayTTL = 5 * time.Minute
)

type config struct {
	clk       clock.Clock
	bus       Bus
	scope     func(ctx context.Context) (string, error)
	errs      []error
	replayTTL time.Duration
	buffer    int
	replay    int
	policy    OverflowPolicy
}

// Option configures New.
type Option func(*config)

// WithDefaultBuffer sets the per-subscription buffer capacity used when a
// subscription does not override it with WithBuffer. Must be positive;
// defaults to 64.
func WithDefaultBuffer(n int) Option {
	return func(c *config) {
		if n <= 0 {
			c.errs = append(c.errs, fmt.Errorf("fanout: default buffer must be positive, got %d", n))
			return
		}
		c.buffer = n
	}
}

// WithDefaultPolicy sets the overflow policy used when a subscription does
// not override it with WithPolicy. Defaults to DropOldest.
func WithDefaultPolicy(p OverflowPolicy) Option {
	return func(c *config) {
		if p > CloseSlow {
			c.errs = append(c.errs, fmt.Errorf("fanout: unknown overflow policy %d", p))
			return
		}
		c.policy = p
	}
}

// WithReplay keeps a ring of the last n messages per topic so subscribers can
// resume with WithResumeAfter. n must be positive. Replay retains one ring
// per published topic (bounded by WithReplayTTL); message IDs stay
// per-instance — see the package comment.
func WithReplay(n int) Option {
	return func(c *config) {
		if n <= 0 {
			c.errs = append(c.errs, fmt.Errorf("fanout: replay size must be positive, got %d", n))
			return
		}
		c.replay = n
	}
}

// WithReplayTTL bounds how long the replay ring of a topic with no
// subscribers is retained before being swept. Must be positive; defaults to
// 5m. Only meaningful together with WithReplay.
func WithReplayTTL(d time.Duration) Option {
	return func(c *config) {
		if d <= 0 {
			c.errs = append(c.errs, fmt.Errorf("fanout: replay TTL must be positive, got %s", d))
			return
		}
		c.replayTTL = d
	}
}

// WithBus attaches a multi-instance backplane. Publish routes through the bus
// only; local delivery happens from the bus receive path (run the driver's
// supervisor.Service). See Bus for the contract.
func WithBus(b Bus) Option {
	return func(c *config) {
		if b == nil {
			c.errs = append(c.errs, fmt.Errorf("fanout: nil bus"))
			return
		}
		c.bus = b
	}
}

// WithScope installs the tenancy hook: its result namespaces every topic on
// Publish and Subscribe, isolating tenants from each other — including
// across a shared Bus. Fail-closed: once configured, a hook error or empty
// scope fails the call with ErrScopeMissing. Single-tenant apps simply do
// not configure it.
func WithScope(fn func(ctx context.Context) (string, error)) Option {
	return func(c *config) {
		if fn == nil {
			c.errs = append(c.errs, fmt.Errorf("fanout: nil scope hook"))
			return
		}
		c.scope = fn
	}
}

// WithClock injects the clock driving replay-ring retention (tests).
func WithClock(clk clock.Clock) Option {
	return func(c *config) {
		if clk == nil {
			c.errs = append(c.errs, fmt.Errorf("fanout: nil clock"))
			return
		}
		c.clk = clk
	}
}

type subConfig struct {
	errs        []error
	resumeAfter uint64
	buffer      int
	policy      OverflowPolicy
	resume      bool
}

// SubscribeOption configures a single subscription.
type SubscribeOption func(*subConfig)

// WithBuffer overrides the hub's default buffer capacity for this
// subscription. Must be positive.
func WithBuffer(n int) SubscribeOption {
	return func(c *subConfig) {
		if n <= 0 {
			c.errs = append(c.errs, fmt.Errorf("fanout: buffer must be positive, got %d", n))
			return
		}
		c.buffer = n
	}
}

// WithPolicy overrides the hub's default overflow policy for this
// subscription.
func WithPolicy(p OverflowPolicy) SubscribeOption {
	return func(c *subConfig) {
		if p > CloseSlow {
			c.errs = append(c.errs, fmt.Errorf("fanout: unknown overflow policy %d", p))
			return
		}
		c.policy = p
	}
}

// WithResumeAfter first delivers the buffered messages with ID greater than
// id from each subscribed topic's replay ring, in ID order, then goes live
// with no gap. id 0 replays everything buffered. Requires WithReplay on the
// hub (ErrReplayDisabled otherwise). If the replayed backlog exceeds the
// subscription buffer, only the newest messages that fit are delivered and
// the rest count as dropped.
func WithResumeAfter(id uint64) SubscribeOption {
	return func(c *subConfig) {
		c.resumeAfter = id
		c.resume = true
	}
}
