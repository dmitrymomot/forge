package postgres

import (
	"context"
	"database/sql"
)

// Migrator is the one-method seam between this package and migration. It is
// structural: postgres does not import migration, and *migration.Migrator satisfies
// this interface, so postgres.WithMigrator(migration.New(fsys)) just works.
//
// Up applies pending schema changes against the supplied *sql.DB, which Open
// bridges from the live pool with stdlib.OpenDBFromPool. The *sql.DB shares the
// pool's connections and must not be closed by the implementation.
type Migrator interface {
	Up(ctx context.Context, db *sql.DB) error
}
