package sqlitedb

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"path"
	"strings"

	"github.com/pressly/goose/v3"
)

// Migrate runs database migrations using the embedded SQL files.
// SQL files are located automatically within the embed.FS — the caller
// can embed at any depth (e.g., //go:embed migrations/*.sql).
// Pass nil for log to disable migration logging.
func Migrate(ctx context.Context, db *sql.DB, migrations embed.FS, log *slog.Logger) error {
	migrationFS, err := resolveMigrationsFS(migrations)
	if err != nil {
		return errors.Join(ErrApplyMigrations, err)
	}

	opts := []goose.ProviderOption{
		goose.WithTableName("schema_migrations"),
	}
	if log != nil {
		opts = append(opts, goose.WithSlog(log))
	}

	provider, err := goose.NewProvider(goose.DialectSQLite3, db, migrationFS, opts...)
	if err != nil {
		return errors.Join(ErrApplyMigrations, err)
	}

	if _, err := provider.Up(ctx); err != nil {
		return errors.Join(ErrApplyMigrations, err)
	}

	return nil
}

// resolveMigrationsFS walks the embed.FS to find the directory containing
// SQL migration files and returns a sub-filesystem rooted there.
// This allows callers to embed at any depth (e.g., //go:embed migrations/*.sql
// or //go:embed testdata/migrations/*.sql) without worrying about directory structure.
func resolveMigrationsFS(fsys embed.FS) (fs.FS, error) {
	var sqlDir string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".sql") {
			sqlDir = path.Dir(p)
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if sqlDir == "" || sqlDir == "." {
		return fsys, nil
	}

	return fs.Sub(fsys, sqlDir)
}
