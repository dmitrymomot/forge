package retry

import (
	"context"
	"errors"
	"time"

	"github.com/dmitrymomot/forge/backoff"
)

type config struct {
	backoff     backoff.Backoff
	retryIf     func(error) bool
	maxAttempts int
}

// Option configures a Retrier.
type Option func(*config)

// WithMaxAttempts caps total attempts (default 3). Values ≤ 0 are ignored.
func WithMaxAttempts(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.maxAttempts = n
		}
	}
}

// WithBackoff sets the delay strategy (default Exponential(100ms,10s,jitter 0.5)).
func WithBackoff(b backoff.Backoff) Option {
	return func(c *config) {
		if b != nil {
			c.backoff = b
		}
	}
}

// WithRetryIf decides, per error, whether to retry (default: retry all non-Permanent).
func WithRetryIf(fn func(error) bool) Option {
	return func(c *config) {
		if fn != nil {
			c.retryIf = fn
		}
	}
}

// Retrier executes functions with a fixed retry policy and is reusable and
// safe for concurrent use.
type Retrier struct{ cfg config }

// New builds a Retrier from options.
func New(opts ...Option) *Retrier {
	c := config{
		maxAttempts: 3,
		backoff:     backoff.Exponential(100*time.Millisecond, 10*time.Second, backoff.WithJitter(0.5)),
		retryIf:     func(error) bool { return true },
	}
	for _, o := range opts {
		o(&c)
	}
	return &Retrier{cfg: c}
}

type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

// Permanent wraps err so retry stops immediately and returns the wrapped error.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

// Do runs fn, retrying per the policy. It returns nil on success, the wrapped
// error for a Permanent, ctx.Err() on cancellation, or the last error on
// exhaustion.
func (r *Retrier) Do(ctx context.Context, fn func(context.Context) error) error {
	var lastErr error
	for attempt := 1; attempt <= r.cfg.maxAttempts; attempt++ {
		err := fn(ctx)
		if err == nil {
			return nil
		}
		if perm, ok := errors.AsType[*permanentError](err); ok {
			return perm.err
		}
		lastErr = err
		if !r.cfg.retryIf(err) {
			return err
		}
		if attempt == r.cfg.maxAttempts {
			break
		}
		timer := time.NewTimer(r.cfg.backoff.Next(attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}

// Do is a one-shot convenience equivalent to New(opts...).Do(ctx, fn).
func Do(ctx context.Context, fn func(context.Context) error, opts ...Option) error {
	return New(opts...).Do(ctx, fn)
}
