// Package sqlitedb provides SQLite database utilities for zero-ops deployments.
//
// It mirrors the API of pkg/db (PostgreSQL) but uses modernc.org/sqlite,
// a pure-Go SQLite driver that requires no CGO and cross-compiles cleanly.
//
// # Features
//
//   - Connection management with sensible SQLite defaults (WAL mode, foreign keys)
//   - Goose-based migrations with embedded SQL files
//   - Transaction helper with automatic rollback on error or panic
//   - Health check and shutdown hooks compatible with the Forge framework
//
// # Configuration
//
// SQLite-specific PRAGMAs are applied automatically in the correct order:
//
//   - journal_mode=wal — concurrent readers with single writer
//   - synchronous=normal — safe with WAL, good performance
//   - busy_timeout=5000 — retry internally instead of returning SQLITE_BUSY
//   - foreign_keys=on — enforced by default (SQLite has them off)
//   - cache_size=-20000 — 20MB page cache
//
// # Usage
//
//	//go:embed migrations/*.sql
//	var migrations embed.FS
//
//	db, err := sqlitedb.Open(ctx, sqlitedb.Config{Path: "./app.db"},
//	    sqlitedb.WithMigrations(migrations),
//	    sqlitedb.WithLogger(slog.Default()),
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Use with Forge
//	app := forge.New(cfg,
//	    forge.WithHealthChecks(
//	        forge.HealthCheck("db", sqlitedb.Healthcheck(db)),
//	    ),
//	)
//
//	forge.Run(forge.RunConfig{Address: ":8080"},
//	    forge.WithFallback(app),
//	    forge.WithShutdownHook(sqlitedb.Shutdown(db)),
//	)
//
// # In-Memory Databases
//
// Use ":memory:" as the path for testing or ephemeral data:
//
//	db, err := sqlitedb.Open(ctx, sqlitedb.Config{Path: ":memory:"})
//
// Note: MaxIdleConns must be >= 1 for in-memory databases, otherwise
// closing the last connection loses all data. The default of 1 handles this.
package sqlitedb
