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

// Option configures a Redis connection.
type Option func(*options)

type options struct {
	poolSize      int
	minIdleConns  int
	maxIdleTime   time.Duration
	maxActiveTime time.Duration
	retryAttempts int
	retryInterval time.Duration
	readTimeout   time.Duration
	writeTimeout  time.Duration
	dialTimeout   time.Duration
}

func defaultOptions() *options {
	return &options{
		poolSize:      10,
		minIdleConns:  5,
		maxIdleTime:   10 * time.Minute,
		maxActiveTime: 30 * time.Minute,
		retryAttempts: 3,
		retryInterval: 5 * time.Second,
		readTimeout:   3 * time.Second,
		writeTimeout:  3 * time.Second,
		dialTimeout:   5 * time.Second,
	}
}

// WithPoolSize sets the maximum number of connections in the pool.
// Default: 10
func WithPoolSize(n int) Option {
	return func(o *options) {
		o.poolSize = n
	}
}

// WithMinIdleConns sets the minimum number of idle connections kept open.
// Default: 5
func WithMinIdleConns(n int) Option {
	return func(o *options) {
		o.minIdleConns = n
	}
}

// WithMaxIdleTime sets the maximum time a connection can be idle before being closed.
// Default: 10 minutes
func WithMaxIdleTime(d time.Duration) Option {
	return func(o *options) {
		o.maxIdleTime = d
	}
}

// WithMaxActiveTime sets the maximum lifetime of a connection.
// Default: 30 minutes
func WithMaxActiveTime(d time.Duration) Option {
	return func(o *options) {
		o.maxActiveTime = d
	}
}

// WithRetry configures connection retry behavior.
// Default: 3 attempts, 5 second base interval with exponential backoff.
func WithRetry(attempts int, interval time.Duration) Option {
	return func(o *options) {
		o.retryAttempts = attempts
		o.retryInterval = interval
	}
}

// WithReadTimeout sets the timeout for read operations.
// Default: 3 seconds
func WithReadTimeout(d time.Duration) Option {
	return func(o *options) {
		o.readTimeout = d
	}
}

// WithWriteTimeout sets the timeout for write operations.
// Default: 3 seconds
func WithWriteTimeout(d time.Duration) Option {
	return func(o *options) {
		o.writeTimeout = d
	}
}

// WithDialTimeout sets the timeout for establishing new connections.
// Default: 5 seconds
func WithDialTimeout(d time.Duration) Option {
	return func(o *options) {
		o.dialTimeout = d
	}
}

// Open creates a Redis client with sensible defaults.
// Supports both redis:// and rediss:// (TLS) URL schemes.
//
// Example:
//
//	client, err := redis.Open(ctx, "redis://localhost:6379/0",
//	    redis.WithPoolSize(20),
//	    redis.WithRetry(5, 3*time.Second),
//	)
func Open(ctx context.Context, url string, opts ...Option) (redis.UniversalClient, error) {
	if url == "" {
		return nil, ErrEmptyConnectionURL
	}

	if !strings.HasPrefix(url, "redis://") && !strings.HasPrefix(url, "rediss://") {
		return nil, ErrFailedToParseURL
	}

	o := defaultOptions()
	for _, opt := range opts {
		opt(o)
	}

	redisOpts, err := redis.ParseURL(url)
	if err != nil {
		return nil, errors.Join(ErrFailedToParseURL, err)
	}

	redisOpts.PoolSize = o.poolSize
	redisOpts.MinIdleConns = o.minIdleConns
	redisOpts.ConnMaxIdleTime = o.maxIdleTime
	redisOpts.ConnMaxLifetime = o.maxActiveTime
	redisOpts.ReadTimeout = o.readTimeout
	redisOpts.WriteTimeout = o.writeTimeout
	redisOpts.DialTimeout = o.dialTimeout

	return connect(ctx, redisOpts, o.retryAttempts, o.retryInterval)
}

// MustOpen creates a Redis client or exits on failure.
// Use for simple applications where startup failure is fatal.
//
// Example:
//
//	client := redis.MustOpen(ctx, os.Getenv("REDIS_URL"),
//	    redis.WithPoolSize(20),
//	)
func MustOpen(ctx context.Context, url string, opts ...Option) redis.UniversalClient {
	client, err := Open(ctx, url, opts...)
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
