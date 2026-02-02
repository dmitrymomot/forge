package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// Default migration settings.
const (
	defaultMigrationsDir   = "migrations"
	defaultMigrationsTable = "schema_migrations"
)

// Migrate runs database migrations using the embedded SQL files.
// Uses hardcoded defaults: "migrations" directory and "schema_migrations" table.
// Pass nil for log to disable migration logging.
func Migrate(ctx context.Context, pool *pgxpool.Pool, migrations embed.FS, log *slog.Logger) error {
	// Bridge pgx connection pool to database/sql interface required by goose.
	// This creates a wrapper that shares the underlying connections but provides
	// the standard library interface that goose migration tool expects.
	// Note: We don't close db here because stdlib.OpenDBFromPool shares the underlying
	// pool connections, and closing would disrupt the shared pool.
	db := stdlib.OpenDBFromPool(pool)

	goose.SetBaseFS(migrations)
	goose.SetTableName(defaultMigrationsTable)

	// Use discard logger if nil
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	goose.SetLogger(&gooseLoggerAdapter{log})

	if err := goose.SetDialect("postgres"); err != nil {
		return errors.Join(ErrSetDialect, err)
	}

	if err := goose.UpContext(ctx, db, defaultMigrationsDir); err != nil {
		return errors.Join(ErrApplyMigrations, err)
	}

	return nil
}

type gooseLoggerAdapter struct {
	log *slog.Logger
}

func (g *gooseLoggerAdapter) Printf(format string, args ...any) {
	g.log.Info(fmt.Sprintf(format, args...))
}

func (g *gooseLoggerAdapter) Fatalf(format string, args ...any) {
	// Log at error level only - goose will return an error that propagates up.
	// We avoid os.Exit(1) to allow proper shutdown and cleanup.
	g.log.Error(fmt.Sprintf(format, args...))
}
