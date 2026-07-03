package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// retryConfig holds the resolved WithTxRetry knobs.
type retryConfig struct {
	interval time.Duration
	attempts int
}

func defaultRetryConfig() retryConfig {
	return retryConfig{attempts: 3, interval: 50 * time.Millisecond}
}

// RetryOption tunes WithTxRetry's serialization-failure retry loop.
type RetryOption func(*retryConfig)

// WithRetryAttempts sets the total number of attempts (including the first). Values
// below 1 are clamped to 1. Default 3.
func WithRetryAttempts(n int) RetryOption {
	return func(c *retryConfig) {
		if n < 1 {
			n = 1
		}
		c.attempts = n
	}
}

// WithRetryInterval sets the base backoff between retries (interval · 2^attempt,
// capped at maxRetryBackoff). Default 50ms.
func WithRetryInterval(d time.Duration) RetryOption {
	return func(c *retryConfig) {
		if d > 0 {
			c.interval = d
		}
	}
}

// WithTx begins a transaction, runs fn, and commits on success or rolls back on
// error. If fn panics, the transaction is rolled back and the panic is re-raised.
// The rollback's own error is ignored once fn has already failed (the original
// error is the meaningful one).
func WithTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}

	panicked := true
	defer func() {
		if panicked {
			_ = tx.Rollback(ctx) // fn panicked; undo and let the panic propagate
		}
	}()

	if err := fn(tx); err != nil {
		panicked = false
		_ = tx.Rollback(ctx)
		return err
	}

	panicked = false
	return tx.Commit(ctx)
}

// WithTxRetry is WithTx plus an automatic retry loop: when the transaction fails
// with a serialization failure or deadlock (SQLSTATE 40001 / 40P01), it backs off
// and retries up to the configured attempt budget. Any other error returns
// immediately. A panic propagates without retry (WithTx re-raises it).
func WithTxRetry(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error, opts ...RetryOption) error {
	rc := defaultRetryConfig()
	for _, opt := range opts {
		opt(&rc)
	}

	var lastErr error
	for attempt := range rc.attempts {
		err := WithTx(ctx, pool, fn)
		if err == nil {
			return nil
		}
		if !isSerializationFailure(err) {
			return err // non-retryable
		}
		lastErr = err

		if attempt == rc.attempts-1 {
			break
		}
		timer := time.NewTimer(backoff(rc.interval, attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(lastErr, ctx.Err())
		case <-timer.C:
		}
	}
	return lastErr
}

// isSerializationFailure delegates to the public predicate so the retry loop and
// classification helpers share one SQLSTATE definition.
func isSerializationFailure(err error) bool {
	return IsSerializationFailure(err)
}
