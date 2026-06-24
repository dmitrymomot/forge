package sqlitedb

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

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

	db, err := sql.Open("sqlite", buildDSN(&cfg))
	if err != nil {
		return nil, errors.Join(ErrOpenFailed, err)
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)

	// PingContext forces the pool to open a connection, which makes the driver
	// apply the _pragma DSN parameters and surfaces any invalid PRAGMA value
	// eagerly instead of on first query.
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

// buildDSN constructs the SQLite connection string with all per-connection
// PRAGMAs encoded as _pragma query parameters.
//
// The modernc.org/sqlite driver applies _pragma parameters on every connection
// it opens (see applyQueryParams in the driver), so the settings hold for the
// whole pool. Running PRAGMA statements against the *sql.DB pool instead would
// only configure a single arbitrary connection, leaving any additional
// connections opened under MaxOpenConns > 1 on driver defaults (foreign_keys
// off, busy_timeout 0, etc.). journal_mode is included here too: for WAL it is
// idempotent (and also persists in the file header), and for per-connection
// journal modes it keeps every pooled connection configured identically.
func buildDSN(cfg *Config) string {
	fk := 0
	if cfg.ForeignKeys {
		fk = 1
	}

	pragmas := []string{
		fmt.Sprintf("_pragma=journal_mode(%s)", cfg.JournalMode),
		fmt.Sprintf("_pragma=busy_timeout(%d)", cfg.BusyTimeoutMS),
		fmt.Sprintf("_pragma=synchronous(%s)", cfg.Synchronous),
		fmt.Sprintf("_pragma=cache_size(%d)", cfg.CacheSize),
		fmt.Sprintf("_pragma=foreign_keys(%d)", fk),
	}

	// Append to any query string the caller already supplied (e.g. a file: URI
	// DSN). The path itself is passed to SQLite verbatim, so ":memory:" and
	// plain file paths both work without escaping.
	sep := "?"
	if strings.ContainsRune(cfg.Path, '?') {
		sep = "&"
	}
	return cfg.Path + sep + strings.Join(pragmas, "&")
}
