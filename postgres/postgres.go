package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// maxRetryBackoff caps the exponential wait between connect attempts so a large
// RetryInterval or attempt count cannot push a single wait past ~30s.
const maxRetryBackoff = 30 * time.Second

// Open builds a connection pool from the resolved options, connects with bounded
// retry/backoff, and verifies liveness with a ping before returning the live
// *pgxpool.Pool. The caller owns the pool and should defer Close(pool, logger).
//
// Flow: start from DefaultConfig, apply options, surface any option errors,
// Validate, parse the URL, overlay the Config limits/timeouts onto the parsed
// *pgxpool.Config, run WithPoolConfig last, then connect-with-retry + ping. Any
// failure closes the partial pool and returns a sentinel-wrapped, single-line error.
func Open(ctx context.Context, opts ...Option) (*pgxpool.Pool, error) {
	cfg := config{Config: DefaultConfig()}
	for _, opt := range opts {
		opt(&cfg)
	}
	if len(cfg.errs) > 0 {
		return nil, errors.Join(cfg.errs...)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	logger := cfg.logger
	if logger == nil {
		logger = slog.Default()
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("%w: parse url: %v", ErrConnect, err)
	}

	// Overlay the serializable Config onto the parsed pool config.
	if cfg.MaxConns > 0 {
		poolCfg.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		poolCfg.MinConns = cfg.MinConns
	}
	if cfg.MaxConnLifetime > 0 {
		poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	}
	if cfg.MaxConnIdleTime > 0 {
		poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	}
	if cfg.HealthCheckPeriod > 0 {
		poolCfg.HealthCheckPeriod = cfg.HealthCheckPeriod
	}
	if cfg.ConnectTimeout > 0 {
		poolCfg.ConnConfig.ConnectTimeout = cfg.ConnectTimeout
	}

	// Escape hatch runs last, after the Config overlay.
	if cfg.poolConfig != nil {
		cfg.poolConfig(poolCfg)
	}

	pool, err := connectWithRetry(ctx, poolCfg, cfg.RetryAttempts, cfg.RetryInterval, logger)
	if err != nil {
		return nil, err
	}

	// Migrator wiring is added in PG-6.

	return pool, nil
}

// connectWithRetry builds the pool and pings it, retrying on failure with
// exponential backoff (RetryInterval · 2^attempt, capped at maxRetryBackoff) and
// honoring ctx cancellation between attempts. attempts <= 1 means a single attempt
// with no wait. On exhaustion it returns ErrConnect joined with the last error.
func connectWithRetry(ctx context.Context, poolCfg *pgxpool.Config, attempts int, interval time.Duration, logger *slog.Logger) (*pgxpool.Pool, error) {
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := range attempts {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrConnect, err)
		}

		pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
		if err == nil {
			if err = pool.Ping(ctx); err == nil {
				return pool, nil
			}
			pool.Close() // ping failed: drop the partial pool before retrying
		}
		lastErr = err

		if attempt == attempts-1 {
			break // do not wait after the final attempt
		}

		wait := backoff(interval, attempt)
		logger.Warn("postgres connect attempt failed; retrying",
			slog.Int("attempt", attempt+1),
			slog.Int("attempts", attempts),
			slog.Duration("wait", wait),
			slog.String("err", err.Error()),
		)
		select {
		case <-ctx.Done():
			return nil, errors.Join(ErrConnect, ctx.Err())
		case <-time.After(wait):
		}
	}

	return nil, errors.Join(ErrConnect, lastErr)
}

// backoff returns interval · 2^attempt, capped at maxRetryBackoff.
func backoff(interval time.Duration, attempt int) time.Duration {
	wait := interval << attempt // interval * 2^attempt
	if wait <= 0 || wait > maxRetryBackoff {
		return maxRetryBackoff
	}
	return wait
}
