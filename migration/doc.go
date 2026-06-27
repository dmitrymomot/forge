// Package migration applies pending up-migrations from an embedded fs.FS against a
// *sql.DB, using goose's instance-based Provider API so two Migrators never clobber
// each other's global state. It is deliberately up-only — it applies all pending
// migrations and nothing else. There is no Down/Version/Status/reset/redo; rollbacks
// and inspection are done with the goose CLI against the same version table, out of
// band.
//
// New stores configuration only; the goose Provider is built per Up call, which
// takes the *sql.DB the caller already owns. The dialect is fixed to PostgreSQL,
// the framework's declared database. Migrations live at the root of fsys; embed a
// subdirectory with fs.Sub if needed.
//
//	//go:embed migrations/*.sql
//	var migrationsFS embed.FS
//
//	func main() {
//		ctx, stop := supervisor.NewContext()
//		defer stop()
//
//		sub, _ := fs.Sub(migrationsFS, "migrations")
//
//		pool, err := postgres.Open(ctx,
//			postgres.WithConfig(cfg),
//			postgres.WithMigrator(migration.New(sub)), // up-migrate on boot
//		)
//		if err != nil {
//			slog.Error("db open failed", "err", err)
//			os.Exit(1)
//		}
//		defer postgres.Close(pool, slog.Default())
//	}
//
// The migration seam: *Migrator structurally satisfies the one-method
// postgres.Migrator interface (Up(ctx, *sql.DB) error), so the value is passed
// straight to postgres.WithMigrator with no adapter. postgres bridges its pool to a
// *sql.DB with stdlib.OpenDBFromPool and calls Up; this package never imports pgx,
// and postgres never imports goose. A failed migration fails postgres.Open.
//
// An fs.FS containing no migration files is a successful no-op, so embedding an empty
// migrations directory still boots cleanly.
//
// Options: WithTable sets the goose version table (default "schema_migrations");
// WithLogger routes goose progress through an *slog.Logger. Errors wrap the
// single-line sentinels ErrInvalidConfig and ErrMigrate and are matchable with
// errors.Is.
package migration
