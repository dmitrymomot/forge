package sqlitedb

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"os"

	_ "modernc.org/sqlite" // SQLite driver registration
)

// Config holds SQLite connection configuration.
// Fields are tagged for environment variable parsing via caarlos0/env.
type Config struct {
	Path          string `env:"PATH,required"`
	JournalMode   string `env:"JOURNAL_MODE"    envDefault:"wal"`
	Synchronous   string `env:"SYNCHRONOUS"     envDefault:"normal"`
	MaxOpenConns  int    `env:"MAX_OPEN_CONNS"  envDefault:"1"`
	MaxIdleConns  int    `env:"MAX_IDLE_CONNS"  envDefault:"1"`
	BusyTimeoutMS int    `env:"BUSY_TIMEOUT_MS" envDefault:"5000"`
	CacheSize     int    `env:"CACHE_SIZE"      envDefault:"-20000"`
	ForeignKeys   bool   `env:"FOREIGN_KEYS"    envDefault:"true"`
}

// Option configures the SQLite database connection.
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

// Open creates a SQLite database connection with sensible defaults.
// PRAGMAs are applied in the correct order for optimal performance.
//
// Example:
//
//	//go:embed migrations/*.sql
//	var migrations embed.FS
//
//	db, err := sqlitedb.Open(ctx, sqlitedb.Config{Path: "./app.db"},
//	    sqlitedb.WithMigrations(migrations),
//	    sqlitedb.WithLogger(log),
//	)
func Open(ctx context.Context, cfg Config, opts ...Option) (*sql.DB, error) {
	if cfg.Path == "" {
		return nil, ErrInvalidConfig
	}

	applyDefaults(&cfg)

	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	db, err := sql.Open("sqlite", cfg.Path)
	if err != nil {
		return nil, errors.Join(ErrOpenFailed, err)
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)

	if err := applyPragmas(ctx, db, &cfg); err != nil {
		db.Close()
		return nil, errors.Join(ErrOpenFailed, err)
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, errors.Join(ErrOpenFailed, err)
	}

	if o.migrations != nil {
		if err := Migrate(ctx, db, *o.migrations, o.logger); err != nil {
			db.Close()
			return nil, err
		}
	}

	return db, nil
}

// MustOpen creates a SQLite database connection or exits on failure.
// Use for simple applications where startup failure is fatal.
func MustOpen(ctx context.Context, cfg Config, opts ...Option) *sql.DB {
	db, err := Open(ctx, cfg, opts...)
	if err != nil {
		slog.Error("failed to open sqlite database", "error", err)
		os.Exit(1)
	}
	return db
}

// applyDefaults sets zero-value fields to their defaults.
func applyDefaults(cfg *Config) {
	if cfg.MaxOpenConns == 0 {
		cfg.MaxOpenConns = 1
	}
	if cfg.MaxIdleConns == 0 {
		cfg.MaxIdleConns = 1
	}
	if cfg.BusyTimeoutMS == 0 {
		cfg.BusyTimeoutMS = 5000
	}
	if cfg.JournalMode == "" {
		cfg.JournalMode = "wal"
	}
	if cfg.Synchronous == "" {
		cfg.Synchronous = "normal"
	}
	if cfg.CacheSize == 0 {
		cfg.CacheSize = -20000
	}
}

// applyPragmas executes PRAGMAs in the correct order.
// journal_mode must be set first, before other PRAGMAs.
func applyPragmas(ctx context.Context, db *sql.DB, cfg *Config) error {
	fk := 0
	if cfg.ForeignKeys {
		fk = 1
	}

	pragmas := []string{
		fmt.Sprintf("PRAGMA journal_mode=%s", cfg.JournalMode),
		fmt.Sprintf("PRAGMA synchronous=%s", cfg.Synchronous),
		fmt.Sprintf("PRAGMA cache_size=%d", cfg.CacheSize),
		fmt.Sprintf("PRAGMA busy_timeout=%d", cfg.BusyTimeoutMS),
		fmt.Sprintf("PRAGMA foreign_keys=%d", fk),
	}

	for _, p := range pragmas {
		if _, err := db.ExecContext(ctx, p); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
	}

	return nil
}
