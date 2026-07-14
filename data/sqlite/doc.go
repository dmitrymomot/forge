// Package sqlite turns a Config into a live, throughput-tuned, cgo-free SQLite
// database built on modernc.org/sqlite, with a reader/writer connection-pool split,
// WAL and pragma discipline, and clean shutdown.
//
// Open builds two database/sql pools over one file: a single pinned writer connection
// (BEGIN IMMEDIATE, WAL) so writes serialize inside Go and never race into
// SQLITE_BUSY, and an N-connection reader pool (query_only) for concurrent WAL reads.
// Send writes and read-your-writes queries to Writer(), read-only queries to Reader();
// the convenience ExecContext/QueryContext/QueryRowContext/BeginTx methods route by
// that same convention. Hand Healthcheck(db) to a readiness probe and defer
// Close(db, logger) in main.
//
//	func main() {
//		ctx, stop := supervisor.NewContext()
//		defer stop()
//
//		cfg := sqlite.DefaultConfig()
//		cfg.Path = "app.db"
//		_ = env.Parse(&cfg)
//
//		db, err := sqlite.Open(ctx,
//			sqlite.WithConfig(cfg),
//			sqlite.WithMigrator(migration.New(migrationsFS, migration.WithDialect(migration.SQLite))),
//		)
//		if err != nil {
//			slog.Error("db open failed", "err", err)
//			os.Exit(1)
//		}
//		defer sqlite.Close(db, slog.Default())
//
//		if err := supervisor.Run(ctx,
//			supervisor.WithService(httpserver.New(routes(db))),
//		); err != nil {
//			slog.Error("shutdown", "err", err)
//			os.Exit(1)
//		}
//	}
//
// Transactions: WithTx runs a function inside a writer transaction (commit on success,
// rollback on error, rollback-and-repanic on panic); WithTxRetry adds an automatic
// retry loop for busy/locked conditions (SQLITE_BUSY / SQLITE_LOCKED).
//
// Error classification: IsUniqueViolation, IsForeignKeyViolation, IsBusy, and
// IsNotFound match the underlying *sqlite.Error result code (or sql.ErrNoRows) so call
// sites branch without importing the driver.
//
// Sentinel errors — ErrInvalidConfig, ErrConnect, ErrHealthcheck — are single-line and
// matchable with errors.Is. Configuration is supplied through Config, whose env struct
// tags are inert; DefaultConfig is the single source of truth for defaults:
//
//	Field            Env var                    Default
//	Path             SQLITE_PATH                "" (required)
//	JournalMode      SQLITE_JOURNAL_MODE        WAL
//	Synchronous      SQLITE_SYNCHRONOUS         NORMAL
//	BusyTimeout      SQLITE_BUSY_TIMEOUT        5s
//	ForeignKeys      SQLITE_FOREIGN_KEYS        true
//	CacheSize        SQLITE_CACHE_SIZE          -16000 (~16 MiB)
//	MmapSize         SQLITE_MMAP_SIZE           268435456 (256 MiB)
//	ReadPoolSize     SQLITE_READ_POOL_SIZE      0 (=> runtime.NumCPU())
//	ConnMaxIdleTime  SQLITE_CONN_MAX_IDLE_TIME  0 (keep warm)
//	ConnMaxLifetime  SQLITE_CONN_MAX_LIFETIME   0 (unbounded)
//
// A Path of ":memory:" is rewritten to a unique shared-cache in-memory database so all
// reader connections see the same data and each Open stays isolated; WAL is skipped
// there (memory databases do not support it).
package sqlite
