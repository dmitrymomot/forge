package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func Migrate(ctx context.Context, pool *pgxpool.Pool, migrations embed.FS, migrationTable string, log *slog.Logger) error {
	// Bridge pgx connection pool to database/sql interface required by goose.
	// This creates a wrapper that shares the underlying connections but provides
	// the standard library interface that goose migration tool expects.
	// Note: We don't close db here because stdlib.OpenDBFromPool shares the underlying
	// pool connections, and closing would disrupt the shared pool.
	db := stdlib.OpenDBFromPool(pool)

	goose.SetBaseFS(migrations)
	goose.SetLogger(&gooseLoggerAdapter{log})
	goose.SetTableName(migrationTable)

	if err := goose.SetDialect("postgres"); err != nil {
		return errors.Join(ErrSetDialect, err)
	}

	if err := goose.UpContext(ctx, db, "."); err != nil {
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
