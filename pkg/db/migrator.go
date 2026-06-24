package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// Default migration settings.
const (
	defaultMigrationsDir   = "."
	defaultMigrationsTable = "schema_migrations"
)

// gooseMu serializes Migrate calls. goose v3 configures the migration run via
// process-global mutable state (SetBaseFS, SetTableName, SetDialect, SetLogger),
// which is not safe to mutate from multiple goroutines concurrently. Holding
// this mutex for the duration of a migration run makes Migrate safe to call
// concurrently at the cost of serializing concurrent migrations.
var gooseMu sync.Mutex

// Migrate runs database migrations using the embedded SQL files.
// Migrations are applied from the FS root (".") and tracked in the
// "schema_migrations" table. Pass nil for log to disable migration logging.
//
// Migrate is safe to call concurrently: calls are serialized internally because
// goose configures the run via process-global state.
func Migrate(ctx context.Context, pool *pgxpool.Pool, migrations embed.FS, log *slog.Logger) error {
	// Bridge the pgx connection pool to the database/sql interface required by
	// goose. stdlib.OpenDBFromPool returns a *sql.DB that draws connections
	// from the shared pool; closing this *sql.DB releases that wrapper without
	// closing the underlying pgxpool, so the caller's pool stays usable.
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	// Use a discard logger when none is provided.
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	gooseMu.Lock()
	defer gooseMu.Unlock()

	goose.SetBaseFS(migrations)
	goose.SetTableName(defaultMigrationsTable)
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
