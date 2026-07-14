package sqlite

import (
	"context"
	"database/sql"
)

// Migrator is the one-method seam between this package and migration. It is
// structural: sqlite does not import migration, and *migration.Migrator satisfies it.
// Up applies pending schema changes against the writer pool's *sql.DB.
type Migrator interface {
	Up(ctx context.Context, db *sql.DB) error
}
