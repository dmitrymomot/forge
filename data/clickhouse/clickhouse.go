package clickhouse

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

// maxConnectBackoff caps the exponential backoff between connect attempts so a large
// RetryInterval or RetryAttempts cannot produce an unbounded wait.
const maxConnectBackoff = 30 * time.Second

// Open builds a native ClickHouse connection from options and returns it only once a
// Ping has confirmed a live server. It starts from DefaultConfig, applies options in
// order, surfaces accumulated option errors and a failed Validate as an
// ErrInvalidConfig-wrapped error (before any network I/O), builds the driver options
// (DSN parse + Config overlay + LZ4 default), runs the WithOptions escape hatch LAST,
// constructs the conn, then pings with bounded retry/backoff. On failure it closes the
// partial conn and returns an ErrConnect-wrapped, single-line error, leaking nothing.
//
// The returned clickhouse.Conn exposes the native API — PrepareBatch, AsyncInsert,
// Select, QueryRow, Exec. The caller owns it and should defer Close(conn, logger).
func Open(ctx context.Context, opts ...Option) (ch.Conn, error) {
	chOpts, cfg, logger, err := resolve(opts)
	if err != nil {
		return nil, err
	}
	conn, err := ch.Open(chOpts)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConnect, err)
	}
	if err := pingWithRetry(ctx, conn.Ping, cfg, logger); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// OpenDB is the database/sql counterpart to Open, returning a *sql.DB for consumers
// that want goose/sqlc/stdlib ergonomics. It shares Open's resolve+build pipeline; the
// driver applies MaxOpenConns/MaxIdleConns/ConnMaxLifetime from the options onto the
// *sql.DB. It pings with the same bounded retry/backoff and, on failure, closes the
// handle and returns an ErrConnect-wrapped error.
func OpenDB(ctx context.Context, opts ...Option) (*sql.DB, error) {
	chOpts, cfg, logger, err := resolve(opts)
	if err != nil {
		return nil, err
	}
	db := ch.OpenDB(chOpts)
	if err := pingWithRetry(ctx, db.PingContext, cfg, logger); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// resolve runs the shared, I/O-free front half of Open/OpenDB: apply options, surface
// option errors, Validate, build the driver options, run the escape hatch, and pick
// the logger (slog.Default when unset). It returns the built options, the resolved
// Config (for the retry budget), and the logger.
func resolve(opts []Option) (*ch.Options, Config, *slog.Logger, error) {
	c := config{Config: DefaultConfig()}
	for _, opt := range opts {
		opt(&c)
	}
	if len(c.errs) > 0 {
		return nil, c.Config, nil, errors.Join(c.errs...)
	}
	if err := c.Validate(); err != nil {
		return nil, c.Config, nil, err
	}
	chOpts, err := buildOptions(c.Config)
	if err != nil {
		return nil, c.Config, nil, err
	}
	if c.withOptions != nil {
		c.withOptions(chOpts) // escape hatch runs LAST, on the fully-built options
	}
	logger := c.logger
	if logger == nil {
		logger = slog.Default()
	}
	return chOpts, c.Config, logger, nil
}

// pingWithRetry pings via ping up to RetryAttempts times, waiting
// RetryInterval·2^attempt (capped at maxConnectBackoff) between tries and honoring ctx
// cancellation during the wait. RetryAttempts <= 1 means a single attempt with no
// wait. After exhausting attempts it returns ErrConnect joined with the last error.
func pingWithRetry(ctx context.Context, ping func(context.Context) error, cfg Config, logger *slog.Logger) error {
	attempts := max(cfg.RetryAttempts, 1)

	var lastErr error
	for attempt := range attempts {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%w: %w", ErrConnect, err)
		}
		if err := ping(ctx); err != nil {
			lastErr = err
		} else {
			return nil
		}

		if attempt == attempts-1 {
			break // no wait after the final attempt
		}
		wait := backoff(cfg.RetryInterval, attempt)
		logger.Warn("clickhouse connect attempt failed; retrying",
			slog.Int("attempt", attempt+1),
			slog.Int("attempts", attempts),
			slog.Duration("wait", wait),
			slog.String("err", lastErr.Error()),
		)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("%w: %w", ErrConnect, ctx.Err())
		case <-timer.C:
		}
	}
	return fmt.Errorf("%w: %v", ErrConnect, lastErr)
}

// backoff returns base·2^attempt, capped at maxConnectBackoff. A non-positive base
// yields no wait.
func backoff(base time.Duration, attempt int) time.Duration {
	if base <= 0 {
		return 0
	}
	d := base << attempt                 // base · 2^attempt
	if d <= 0 || d > maxConnectBackoff { // overflow or over the cap
		return maxConnectBackoff
	}
	return d
}
