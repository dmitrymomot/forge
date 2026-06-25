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

// Config holds database connection configuration.
type Config struct {
	URL               string        `env:"URL,required"`
	MaxConns          int32         `env:"MAX_CONNS"           envDefault:"10"`
	MinConns          int32         `env:"MIN_CONNS"           envDefault:"5"`
	HealthCheckPeriod time.Duration `env:"HEALTH_CHECK_PERIOD" envDefault:"1m"`
	MaxConnIdleTime   time.Duration `env:"MAX_CONN_IDLE_TIME"  envDefault:"10m"`
	MaxConnLifetime   time.Duration `env:"MAX_CONN_LIFETIME"   envDefault:"30m"`
	RetryAttempts     int           `env:"RETRY_ATTEMPTS"      envDefault:"3"`
	RetryInterval     time.Duration `env:"RETRY_INTERVAL"      envDefault:"5s"`
}

// Option configures database connection.
type Option func(*options)

type options struct {
	migrations *embed.FS
	logger     *slog.Logger
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

// Open creates a PostgreSQL connection pool with sensible defaults.
// Supports optional migrations and configurable pool settings via Config.
//
// Example:
//
//	//go:embed *.sql
//	var migrations embed.FS
//
//	pool, err := db.Open(ctx, db.Config{URL: "postgres://user:pass@host:5432/db"},
//	    db.WithMigrations(migrations),
//	    db.WithLogger(log),
//	)
func Open(ctx context.Context, cfg Config, opts ...Option) (*pgxpool.Pool, error) {
	if cfg.URL == "" {
		return nil, ErrFailedToParseDBConfig
	}

	applyDefaults(&cfg)

	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	connConfig, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, errors.Join(ErrFailedToParseDBConfig, err)
	}

	connConfig.MaxConns = cfg.MaxConns
	connConfig.MinConns = cfg.MinConns
	connConfig.HealthCheckPeriod = cfg.HealthCheckPeriod
	connConfig.MaxConnIdleTime = cfg.MaxConnIdleTime
	connConfig.MaxConnLifetime = cfg.MaxConnLifetime

	pool, err := connect(ctx, connConfig, cfg.RetryAttempts, cfg.RetryInterval)
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

// applyDefaults fills zero-valued pool and retry settings with sensible
// defaults. It mutates cfg in place and only touches fields left at their zero
// value, so explicitly-set values are preserved.
func applyDefaults(cfg *Config) {
	if cfg.MaxConns == 0 {
		cfg.MaxConns = 10
	}
	if cfg.MinConns == 0 {
		cfg.MinConns = 5
	}
	if cfg.HealthCheckPeriod == 0 {
		cfg.HealthCheckPeriod = time.Minute
	}
	if cfg.MaxConnIdleTime == 0 {
		cfg.MaxConnIdleTime = 10 * time.Minute
	}
	if cfg.MaxConnLifetime == 0 {
		cfg.MaxConnLifetime = 30 * time.Minute
	}
	if cfg.RetryAttempts == 0 {
		cfg.RetryAttempts = 3
	}
	if cfg.RetryInterval == 0 {
		cfg.RetryInterval = 5 * time.Second
	}
}

// MustOpen creates a connection pool or exits on failure.
// Use for simple applications where startup failure is fatal.
//
// Example:
//
//	pool := db.MustOpen(ctx, db.Config{URL: os.Getenv("DATABASE_URL")},
//	    db.WithMigrations(migrations),
//	    db.WithLogger(log),
//	)
func MustOpen(ctx context.Context, cfg Config, opts ...Option) *pgxpool.Pool {
	pool, err := Open(ctx, cfg, opts...)
	if err != nil {
		slog.Error("failed to open database connection", "error", err)
		os.Exit(1)
	}
	return pool
}

// connect establishes a connection with retry logic.
// Backoff between attempts is linear: the wait before attempt i is i*interval.
// The underlying error from the final attempt is preserved in the returned error.
func connect(ctx context.Context, cfg *pgxpool.Config, attempts int, interval time.Duration) (*pgxpool.Pool, error) {
	attempts = max(attempts, 1)

	var lastErr error
	for i := range attempts {
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			lastErr = err
		} else if pingErr := pool.Ping(ctx); pingErr != nil {
			pool.Close()
			lastErr = pingErr
		} else {
			return pool, nil
		}

		// Skip the backoff wait after the final attempt; there is nothing
		// left to retry, so sleeping would only delay the returned error.
		if i == attempts-1 {
			break
		}
		if waitErr := wait(ctx, time.Duration(i+1)*interval); waitErr != nil {
			return nil, errors.Join(ErrFailedToOpenDBConnection, lastErr, waitErr)
		}
	}

	return nil, errors.Join(ErrFailedToOpenDBConnection, lastErr)
}

func wait(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
