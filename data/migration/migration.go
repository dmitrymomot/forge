package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

// Migrator applies pending up-migrations from an fs.FS against a *sql.DB using
// goose's instance-based Provider API (no global mutable state). It is up-only:
// there is no Down/Version/Status. The zero value is not usable; build one with New.
//
// *Migrator structurally satisfies the postgres.Migrator interface, so it can be
// passed straight to postgres.WithMigrator.
type Migrator struct {
	fsys fs.FS
	cfg  config
}

// New returns a Migrator that applies the migrations rooted at fsys. Migrations live
// at the root of fsys; embed a subdirectory with fs.Sub if needed. The dialect is
// fixed to PostgreSQL. New never returns an error and never contacts a database — it
// only stores configuration; the goose provider is built per call to Up.
func New(fsys fs.FS, opts ...Option) *Migrator {
	cfg := config{table: DefaultTable}
	for _, opt := range opts {
		opt(&cfg)
	}

	return &Migrator{fsys: fsys, cfg: cfg}
}

// Up applies all pending migrations against db. It builds a fresh goose Provider
// (dialect Postgres, the configured version table) and runs provider.Up. An fsys
// with no migration files is treated as a successful no-op. The db is owned by the
// caller and is never closed here. Errors wrap ErrMigrate and are single-line.
func (m *Migrator) Up(ctx context.Context, db *sql.DB) error {
	opts := []goose.ProviderOption{goose.WithTableName(m.cfg.table)}
	if m.cfg.logger != nil {
		opts = append(opts, goose.WithSlog(m.cfg.logger))
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, db, m.fsys, opts...)
	if err != nil {
		// An empty fs.FS is a no-op, not a failure: an app that embeds an empty
		// migrations directory should still boot cleanly.
		if errors.Is(err, goose.ErrNoMigrations) {
			return nil
		}

		return fmt.Errorf("%w: new provider: %v", ErrMigrate, err)
	}

	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("%w: %v", ErrMigrate, err)
	}

	return nil
}
