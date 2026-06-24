package redis

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Default Config values. These are the single source of truth: the envDefault
// struct tags reference the same values so env-loaded and programmatically
// constructed configs share identical defaults.
const (
	defaultPoolSize      = 10
	defaultMinIdleConns  = 5
	defaultMaxIdleTime   = 10 * time.Minute
	defaultMaxActiveTime = 30 * time.Minute
	defaultRetryAttempts = 3
	defaultRetryInterval = 5 * time.Second
	defaultReadTimeout   = 3 * time.Second
	defaultWriteTimeout  = 3 * time.Second
	defaultDialTimeout   = 5 * time.Second
)

// Config holds Redis connection configuration.
//
// envDefault tags only apply when the config is populated from the environment.
// For programmatically constructed configs, applyDefaults fills zero-value
// fields with the same defaults so both paths behave identically.
type Config struct {
	URL           string        `env:"URL,required"`
	PoolSize      int           `env:"POOL_SIZE"       envDefault:"10"`
	MinIdleConns  int           `env:"MIN_IDLE_CONNS"  envDefault:"5"`
	MaxIdleTime   time.Duration `env:"MAX_IDLE_TIME"   envDefault:"10m"`
	MaxActiveTime time.Duration `env:"MAX_ACTIVE_TIME" envDefault:"30m"`
	RetryAttempts int           `env:"RETRY_ATTEMPTS"  envDefault:"3"`
	RetryInterval time.Duration `env:"RETRY_INTERVAL"  envDefault:"5s"`
	ReadTimeout   time.Duration `env:"READ_TIMEOUT"    envDefault:"3s"`
	WriteTimeout  time.Duration `env:"WRITE_TIMEOUT"   envDefault:"3s"`
	DialTimeout   time.Duration `env:"DIAL_TIMEOUT"    envDefault:"5s"`
}

// applyDefaults fills zero-value fields with sensible defaults.
// The envDefault tags handle env parsing; this covers programmatic construction.
func (c *Config) applyDefaults() {
	if c.PoolSize == 0 {
		c.PoolSize = defaultPoolSize
	}
	if c.MinIdleConns == 0 {
		c.MinIdleConns = defaultMinIdleConns
	}
	if c.MaxIdleTime == 0 {
		c.MaxIdleTime = defaultMaxIdleTime
	}
	if c.MaxActiveTime == 0 {
		c.MaxActiveTime = defaultMaxActiveTime
	}
	if c.RetryAttempts == 0 {
		c.RetryAttempts = defaultRetryAttempts
	}
	if c.RetryInterval == 0 {
		c.RetryInterval = defaultRetryInterval
	}
	if c.ReadTimeout == 0 {
		c.ReadTimeout = defaultReadTimeout
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = defaultWriteTimeout
	}
	if c.DialTimeout == 0 {
		c.DialTimeout = defaultDialTimeout
	}
}

// Open creates a Redis client with the given configuration.
// Supports both redis:// and rediss:// (TLS) URL schemes.
//
// The client is a single-node client ([redis.NewClient]) returned through the
// [redis.UniversalClient] interface so callers can program against the interface
// regardless of the underlying topology. Cluster and sentinel topologies are not
// configured here.
func Open(ctx context.Context, cfg Config) (redis.UniversalClient, error) {
	if cfg.URL == "" {
		return nil, ErrEmptyConnectionURL
	}

	if !strings.HasPrefix(cfg.URL, "redis://") && !strings.HasPrefix(cfg.URL, "rediss://") {
		return nil, ErrFailedToParseURL
	}

	cfg.applyDefaults()

	redisOpts, err := redis.ParseURL(cfg.URL)
	if err != nil {
		return nil, errors.Join(ErrFailedToParseURL, err)
	}

	redisOpts.PoolSize = cfg.PoolSize
	redisOpts.MinIdleConns = cfg.MinIdleConns
	redisOpts.ConnMaxIdleTime = cfg.MaxIdleTime
	redisOpts.ConnMaxLifetime = cfg.MaxActiveTime
	redisOpts.ReadTimeout = cfg.ReadTimeout
	redisOpts.WriteTimeout = cfg.WriteTimeout
	redisOpts.DialTimeout = cfg.DialTimeout

	return connect(ctx, redisOpts, cfg.RetryAttempts, cfg.RetryInterval)
}

// MustOpen creates a Redis client or exits on failure.
// Use for simple applications where startup failure is fatal.
func MustOpen(ctx context.Context, cfg Config) redis.UniversalClient {
	client, err := Open(ctx, cfg)
	if err != nil {
		slog.Error("failed to open redis connection", "error", err)
		os.Exit(1)
	}
	return client
}

// connect establishes a connection with retry logic and exponential backoff.
// On final failure it joins the underlying ping error so callers can inspect the
// cause via errors.Is/errors.Unwrap, as documented.
func connect(ctx context.Context, opts *redis.Options, attempts int, interval time.Duration) (redis.UniversalClient, error) {
	attempts = max(attempts, 1)

	var lastErr error
	for i := range attempts {
		client := redis.NewClient(opts)

		lastErr = client.Ping(ctx).Err()
		if lastErr == nil {
			return client, nil
		}

		_ = client.Close()

		// Don't sleep after the final attempt — there is no further retry, so
		// waiting only delays returning the failure.
		if i == attempts-1 {
			break
		}

		if waitErr := wait(ctx, time.Duration(i+1)*interval); waitErr != nil {
			return nil, errors.Join(ErrConnectionFailed, lastErr, waitErr)
		}
	}

	return nil, errors.Join(ErrConnectionFailed, lastErr)
}

func wait(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
