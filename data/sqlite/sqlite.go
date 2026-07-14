package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"runtime"

	_ "modernc.org/sqlite" // registers the cgo-free "sqlite" driver
)

// DB wraps the writer and reader connection pools over one SQLite database. Obtain
// the native handles with Writer/Reader, or use the convenience routing methods.
type DB struct {
	writer *sql.DB
	reader *sql.DB
	logger *slog.Logger
}

// Open resolves options, validates, builds the writer (single pinned connection,
// BEGIN IMMEDIATE, WAL) and reader (N connections, query_only) pools, pings each,
// runs the migrator (if any) on the writer, and returns the live *DB. The caller
// owns it and should defer Close(db, logger).
func Open(ctx context.Context, opts ...Option) (*DB, error) {
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

	readConns := cfg.ReadPoolSize
	if readConns <= 0 {
		readConns = runtime.NumCPU()
	}

	memory := isMemory(cfg.Path)
	memName := ""
	if memory {
		memName = nextMemName()
	}

	writer, err := sql.Open("sqlite", buildDSN(cfg.Config, cfg.pragmas, memory, memName, true))
	if err != nil {
		return nil, fmt.Errorf("%w: open writer: %v", ErrConnect, err)
	}
	writer.SetMaxOpenConns(1)
	writer.SetMaxIdleConns(1)
	writer.SetConnMaxIdleTime(0)
	writer.SetConnMaxLifetime(0)

	reader, err := sql.Open("sqlite", buildDSN(cfg.Config, cfg.pragmas, memory, memName, false))
	if err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("%w: open reader: %v", ErrConnect, err)
	}
	reader.SetMaxOpenConns(readConns)
	reader.SetMaxIdleConns(readConns)
	reader.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	reader.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	// Ping the writer first: it creates the file and sets WAL before the query_only
	// reader (which cannot) connects; for memory it creates the shared-cache DB.
	if err := writer.PingContext(ctx); err != nil {
		_ = writer.Close()
		_ = reader.Close()
		return nil, fmt.Errorf("%w: ping writer: %v", ErrConnect, err)
	}
	if err := reader.PingContext(ctx); err != nil {
		_ = writer.Close()
		_ = reader.Close()
		return nil, fmt.Errorf("%w: ping reader: %v", ErrConnect, err)
	}

	db := &DB{writer: writer, reader: reader, logger: logger}

	if cfg.migrator != nil {
		if err := cfg.migrator.Up(ctx, writer); err != nil {
			_ = writer.Close()
			_ = reader.Close()
			return nil, fmt.Errorf("sqlite: migrate: %w", err)
		}
	}

	return db, nil
}

// Writer returns the single-connection write pool (BEGIN IMMEDIATE, WAL). Send every
// statement that writes here, plus any read that must observe uncommitted writes in a
// write transaction.
func (db *DB) Writer() *sql.DB { return db.writer }

// Reader returns the concurrent read pool (query_only). Send read-only queries here.
func (db *DB) Reader() *sql.DB { return db.reader }

// ExecContext routes to the writer.
func (db *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return db.writer.ExecContext(ctx, query, args...)
}

// QueryContext routes to the reader. For a write that returns rows (… RETURNING) use
// Writer().QueryContext instead — the reader is query_only and will reject it.
func (db *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return db.reader.QueryContext(ctx, query, args...)
}

// QueryRowContext routes to the reader (see QueryContext for the RETURNING caveat).
func (db *DB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return db.reader.QueryRowContext(ctx, query, args...)
}

// BeginTx routes to the writer and acquires the write lock immediately (_txlock).
func (db *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return db.writer.BeginTx(ctx, opts)
}
