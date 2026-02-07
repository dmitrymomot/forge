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

// Config holds Redis connection configuration.
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
		c.PoolSize = 10
	}
	if c.MinIdleConns == 0 {
		c.MinIdleConns = 5
	}
	if c.MaxIdleTime == 0 {
		c.MaxIdleTime = 10 * time.Minute
	}
	if c.MaxActiveTime == 0 {
		c.MaxActiveTime = 30 * time.Minute
	}
	if c.RetryAttempts == 0 {
		c.RetryAttempts = 3
	}
	if c.RetryInterval == 0 {
		c.RetryInterval = 5 * time.Second
	}
	if c.ReadTimeout == 0 {
		c.ReadTimeout = 3 * time.Second
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = 3 * time.Second
	}
	if c.DialTimeout == 0 {
		c.DialTimeout = 5 * time.Second
	}
}

// Open creates a Redis client with the given configuration.
// Supports both redis:// and rediss:// (TLS) URL schemes.
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
func connect(ctx context.Context, opts *redis.Options, attempts int, interval time.Duration) (redis.UniversalClient, error) {
	attempts = max(attempts, 1)

	for i := range attempts {
		client := redis.NewClient(opts)

		if err := client.Ping(ctx).Err(); err == nil {
			return client, nil
		}

		_ = client.Close()

		if waitErr := wait(ctx, time.Duration(i+1)*interval); waitErr != nil {
			return nil, errors.Join(ErrConnectionFailed, waitErr)
		}
	}

	return nil, ErrConnectionFailed
}

func wait(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
