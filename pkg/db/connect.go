package db

import (
	"context"
	"embed"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Option configures database connection.
type Option func(*options)

type options struct {
	migrations        *embed.FS
	logger            *slog.Logger
	maxConns          int32
	minConns          int32
	healthCheckPeriod time.Duration
	maxConnIdleTime   time.Duration
	maxConnLifetime   time.Duration
	retryAttempts     int
	retryInterval     time.Duration
}

func defaultOptions() *options {
	return &options{
		maxConns:          10,
		minConns:          5,
		healthCheckPeriod: 1 * time.Minute,
		maxConnIdleTime:   10 * time.Minute,
		maxConnLifetime:   30 * time.Minute,
		retryAttempts:     3,
		retryInterval:     5 * time.Second,
	}
}

// WithMigrations enables automatic migrations using embedded SQL files.
func WithMigrations(fs embed.FS) Option {
	return func(o *options) {
		o.migrations = &fs
	}
}

// WithLogger sets the logger for migrations and connection events.
func WithLogger(log *slog.Logger) Option {
	return func(o *options) {
		o.logger = log
	}
}

// WithMaxConns sets maximum number of connections in the pool.
// Default: 10
func WithMaxConns(n int32) Option {
	return func(o *options) {
		o.maxConns = n
	}
}

// WithMinConns sets minimum number of connections kept open.
// Default: 5
func WithMinConns(n int32) Option {
	return func(o *options) {
		o.minConns = n
	}
}

// WithHealthCheckPeriod sets how often connections are checked.
// Default: 1 minute
func WithHealthCheckPeriod(d time.Duration) Option {
	return func(o *options) {
		o.healthCheckPeriod = d
	}
}

// WithMaxConnIdleTime sets maximum time a connection can be idle.
// Default: 10 minutes
func WithMaxConnIdleTime(d time.Duration) Option {
	return func(o *options) {
		o.maxConnIdleTime = d
	}
}

// WithMaxConnLifetime sets maximum lifetime of a connection.
// Default: 30 minutes
func WithMaxConnLifetime(d time.Duration) Option {
	return func(o *options) {
		o.maxConnLifetime = d
	}
}

// WithRetry configures connection retry behavior.
// Default: 3 attempts, 5 second interval with exponential backoff.
func WithRetry(attempts int, interval time.Duration) Option {
	return func(o *options) {
		o.retryAttempts = attempts
		o.retryInterval = interval
	}
}

// Open creates a PostgreSQL connection pool with sensible defaults.
// Supports optional migrations and configurable pool settings via functional options.
//
// Example:
//
//	//go:embed migrations/*.sql
//	var migrations embed.FS
//
//	pool, err := db.Open(ctx, "postgres://user:pass@host:5432/db",
//	    db.WithMigrations(migrations),
//	    db.WithLogger(log),
//	)
func Open(ctx context.Context, connString string, opts ...Option) (*pgxpool.Pool, error) {
	o := defaultOptions()
	for _, opt := range opts {
		opt(o)
	}

	connConfig, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, errors.Join(ErrFailedToParseDBConfig, err)
	}

	connConfig.MaxConns = o.maxConns
	connConfig.MinConns = o.minConns
	connConfig.HealthCheckPeriod = o.healthCheckPeriod
	connConfig.MaxConnIdleTime = o.maxConnIdleTime
	connConfig.MaxConnLifetime = o.maxConnLifetime

	pool, err := connect(ctx, connConfig, o.retryAttempts, o.retryInterval)
	if err != nil {
		return nil, err
	}

	if o.migrations != nil {
		if err := Migrate(ctx, pool, *o.migrations, o.logger); err != nil {
			pool.Close()
			return nil, err
		}
	}

	return pool, nil
}

// MustOpen creates a connection pool or exits on failure.
// Use for simple applications where startup failure is fatal.
//
// Example:
//
//	pool := db.MustOpen(ctx, os.Getenv("DATABASE_URL"),
//	    db.WithMigrations(migrations),
//	    db.WithLogger(log),
//	)
func MustOpen(ctx context.Context, connString string, opts ...Option) *pgxpool.Pool {
	pool, err := Open(ctx, connString, opts...)
	if err != nil {
		slog.Error("failed to open database connection", "error", err)
		os.Exit(1)
	}
	return pool
}

// connect establishes a connection with retry logic.
func connect(ctx context.Context, cfg *pgxpool.Config, attempts int, interval time.Duration) (*pgxpool.Pool, error) {
	attempts = max(attempts, 1)

	for i := range attempts {
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			if waitErr := wait(ctx, time.Duration(i+1)*interval); waitErr != nil {
				return nil, errors.Join(ErrFailedToOpenDBConnection, waitErr)
			}
			continue
		}

		if err := pool.Ping(ctx); err != nil {
			pool.Close()
			if waitErr := wait(ctx, time.Duration(i+1)*interval); waitErr != nil {
				return nil, errors.Join(ErrFailedToOpenDBConnection, waitErr)
			}
			continue
		}

		return pool, nil
	}

	return nil, ErrFailedToOpenDBConnection
}

func wait(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
