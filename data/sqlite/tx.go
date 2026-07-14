package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// maxRetryBackoff caps the wait between WithTxRetry attempts.
const maxRetryBackoff = 30 * time.Second

// retryConfig holds the resolved WithTxRetry knobs.
type retryConfig struct {
	interval time.Duration
	attempts int
}

func defaultRetryConfig() retryConfig {
	return retryConfig{attempts: 3, interval: 50 * time.Millisecond}
}

// RetryOption tunes WithTxRetry's busy-retry loop.
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
// capped at maxRetryBackoff). Values <= 0 are ignored. Default 50ms.
func WithRetryInterval(d time.Duration) RetryOption {
	return func(c *retryConfig) {
		if d > 0 {
			c.interval = d
		}
	}
}

// WithTx begins a transaction on the writer pool (BEGIN IMMEDIATE via the writer's
// _txlock, so the write lock is taken upfront), runs fn, and commits on success or
// rolls back on error. If fn panics, the transaction is rolled back and the panic is
// re-raised. The rollback's own error is ignored once fn has already failed.
func WithTx(ctx context.Context, db *DB, fn func(*sql.Tx) error) error {
	tx, err := db.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	panicked := true
	defer func() {
		if panicked {
			_ = tx.Rollback()
		}
	}()

	if err := fn(tx); err != nil {
		panicked = false
		_ = tx.Rollback()
		return err
	}

	panicked = false
	return tx.Commit()
}

// WithTxRetry is WithTx plus an automatic retry loop: when the transaction fails with
// a busy/locked condition (IsBusy) it backs off and retries up to the configured
// attempt budget. Any other error returns immediately. A panic propagates without
// retry (WithTx re-raises it).
func WithTxRetry(ctx context.Context, db *DB, fn func(*sql.Tx) error, opts ...RetryOption) error {
	rc := defaultRetryConfig()
	for _, opt := range opts {
		opt(&rc)
	}

	var lastErr error
	for attempt := range rc.attempts {
		err := WithTx(ctx, db, fn)
		if err == nil {
			return nil
		}
		if !IsBusy(err) {
			return err
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

// backoff returns interval · 2^attempt, capped at maxRetryBackoff.
func backoff(interval time.Duration, attempt int) time.Duration {
	if interval <= 0 {
		return 0
	}
	wait := interval << attempt
	if wait <= 0 || wait > maxRetryBackoff {
		return maxRetryBackoff
	}
	return wait
}
