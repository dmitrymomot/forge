package circuitbreaker

import (
	"context"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

// State is the breaker's current mode.
type State int

const (
	// StateClosed passes calls through and counts failures.
	StateClosed State = iota
	// StateOpen rejects calls with ErrOpen until the open timeout elapses.
	StateOpen
	// StateHalfOpen admits a limited number of probe calls.
	StateHalfOpen
)

// String returns "closed", "open", or "half-open".
func (s State) String() string {
	switch s {
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "closed"
	}
}

type config struct {
	onStateChange func(from, to State)
	clk           clock.Clock
	threshold     int
	openTimeout   time.Duration
	halfOpenMax   int
}

// Option configures a Breaker.
type Option func(*config)

// WithFailureThreshold sets consecutive failures that open the circuit (default 5).
func WithFailureThreshold(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.threshold = n
		}
	}
}

// WithOpenTimeout sets how long the circuit stays open before a probe (default 30s).
func WithOpenTimeout(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.openTimeout = d
		}
	}
}

// WithHalfOpenMax caps concurrent probe calls in half-open (default 1).
func WithHalfOpenMax(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.halfOpenMax = n
		}
	}
}

// WithOnStateChange registers a transition callback. It runs while the breaker
// lock is held, so it must not call back into the same Breaker.
func WithOnStateChange(fn func(from, to State)) Option {
	return func(c *config) { c.onStateChange = fn }
}

// WithClock injects the time source (default clock.System()).
func WithClock(clk clock.Clock) Option {
	return func(c *config) {
		if clk != nil {
			c.clk = clk
		}
	}
}

// Breaker guards calls to a dependency. Construct it with New; safe for
// concurrent use.
type Breaker struct {
	openedAt   time.Time
	cfg        config
	state      State
	failures   int
	halfOpenIn int
	mu         sync.Mutex
}

// newConfig applies opts over the defaults. Shared by New and Group.
func newConfig(opts ...Option) config {
	c := config{threshold: 5, openTimeout: 30 * time.Second, halfOpenMax: 1, clk: clock.System()}
	for _, o := range opts {
		o(&c)
	}
	return c
}

// New builds a Breaker from options.
func New(opts ...Option) *Breaker {
	return &Breaker{cfg: newConfig(opts...), state: StateClosed}
}

// State reports the current state.
func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// RetryAfter reports how long until the breaker would admit a probe call, or 0
// when it is not open.
func (b *Breaker) RetryAfter() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state != StateOpen {
		return 0
	}
	if remaining := b.cfg.openTimeout - b.cfg.clk.Now().Sub(b.openedAt); remaining > 0 {
		return remaining
	}
	return 0
}

// Do runs fn unless the circuit rejects it, recording the outcome. It returns
// ErrOpen without calling fn when the circuit is open.
func (b *Breaker) Do(ctx context.Context, fn func(context.Context) error) error {
	if err := b.before(); err != nil {
		return err
	}
	err := fn(ctx)
	b.after(err)
	return err
}

func (b *Breaker) before() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case StateOpen:
		elapsed := b.cfg.clk.Now().Sub(b.openedAt)
		if elapsed < b.cfg.openTimeout {
			return &openError{retryAfter: b.cfg.openTimeout - elapsed}
		}
		b.transition(StateHalfOpen)
		b.halfOpenIn = 1
		return nil
	case StateHalfOpen:
		if b.halfOpenIn >= b.cfg.halfOpenMax {
			return &openError{retryAfter: 0} // a probe is in flight; retry shortly
		}
		b.halfOpenIn++
		return nil
	default:
		return nil
	}
}

func (b *Breaker) after(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case StateHalfOpen:
		b.halfOpenIn--
		if err != nil {
			b.openedAt = b.cfg.clk.Now()
			b.transition(StateOpen)
			return
		}
		b.failures = 0
		b.transition(StateClosed)
	case StateClosed:
		if err == nil {
			b.failures = 0
			return
		}
		b.failures++
		if b.failures >= b.cfg.threshold {
			b.openedAt = b.cfg.clk.Now()
			b.transition(StateOpen)
		}
	}
}

// transition sets a new state and fires the callback. Caller holds b.mu.
func (b *Breaker) transition(to State) {
	if b.state == to {
		return
	}
	from := b.state
	b.state = to
	if b.cfg.onStateChange != nil {
		b.cfg.onStateChange(from, to)
	}
}
