package redis

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// maxConnectBackoff caps the exponential backoff between connect attempts so a large
// RetryInterval or RetryAttempts cannot produce an unbounded wait.
const maxConnectBackoff = 30 * time.Second

// Open builds a Redis (or Valkey) client from options and returns it only once a PING
// has confirmed a live server. It starts from DefaultConfig, applies the options in
// order, surfaces any accumulated option errors and a failed Validate as an
// ErrInvalidConfig-wrapped error (before any network I/O), maps the Config onto
// *goredis.UniversalOptions, runs the WithUniversalOptions escape hatch LAST,
// constructs a topology-appropriate client via goredis.NewUniversalClient, then pings
// with bounded retry/backoff. On failure it closes the partially-opened client and
// returns an ErrConnect-wrapped, single-line error — leaking nothing.
//
// The returned value is the goredis.UniversalClient interface; *goredis.Client,
// *goredis.ClusterClient, and *goredis.FailoverClient all satisfy it.
func Open(ctx context.Context, opts ...Option) (goredis.UniversalClient, error) {
	c := &config{Config: DefaultConfig()}
	for _, opt := range opts {
		opt(c)
	}
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	logger := c.logger
	if logger == nil {
		logger = slog.Default()
	}

	uopts := buildOptions(c.Config)
	if c.universalOptions != nil {
		c.universalOptions(uopts) // escape hatch runs LAST, on the fully-built options
	}

	client := goredis.NewUniversalClient(uopts)
	if err := pingWithRetry(ctx, client, c.Config, logger); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

// pingWithRetry pings the server up to RetryAttempts times, waiting
// RetryInterval·2^attempt (capped at maxConnectBackoff) between tries and honoring
// ctx cancellation during the wait. RetryAttempts <= 1 means a single attempt with no
// wait. After exhausting attempts it returns ErrConnect joined with the last error.
func pingWithRetry(ctx context.Context, client goredis.UniversalClient, cfg Config, logger *slog.Logger) error {
	attempts := max(cfg.RetryAttempts, 1)

	var lastErr error
	for attempt := range attempts {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%w: %v", ErrConnect, err)
		}
		if err := client.Ping(ctx).Err(); err != nil {
			lastErr = err
		} else {
			return nil
		}

		// No wait after the final attempt.
		if attempt == attempts-1 {
			break
		}
		wait := backoff(cfg.RetryInterval, attempt)
		logger.Warn("redis connect attempt failed; retrying",
			slog.Int("attempt", attempt+1),
			slog.Int("attempts", attempts),
			slog.Duration("wait", wait),
			slog.String("err", lastErr.Error()),
		)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("%w: %v", ErrConnect, ctx.Err())
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
	d := base << attempt // base · 2^attempt
	// Guard the shift overflowing into a negative duration on large attempt counts.
	if d <= 0 || d > maxConnectBackoff {
		return maxConnectBackoff
	}
	return d
}
