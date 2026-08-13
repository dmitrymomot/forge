// Package postgres turns a Config into a live, pooled, health-checkable PostgreSQL
// client built on pgx/v5 and pgxpool, with production-sane defaults, bounded
// startup retry, and clean shutdown — then gets out of the way by returning the
// native *pgxpool.Pool for all data access.
//
// Open seeds from DefaultConfig, applies options, validates, parses the URL,
// overlays the pool limits/timeouts, runs the WithPoolConfig escape hatch last,
// then connects with bounded exponential backoff and a liveness ping. Hand
// Healthcheck(pool) to a readiness probe and defer Close(pool, logger) in main so
// the pool outlives every supervised service's drain.
//
//	func main() {
//		ctx, stop := supervisor.NewContext()
//		defer stop()
//
//		cfg := postgres.DefaultConfig()
//		_ = env.Parse(&cfg)
//
//		pool, err := postgres.Open(ctx,
//			postgres.WithConfig(cfg),
//			postgres.WithLogger(slog.Default()),
//			postgres.WithMigrator(migration.New(migrationsFS)),
//		)
//		if err != nil {
//			slog.Error("db open failed", "err", err)
//			os.Exit(1)
//		}
//		defer postgres.Close(pool, slog.Default())
//
//		err = supervisor.Run(ctx,
//			supervisor.WithService(httpserver.New(routes(pool))),
//		)
//		if err != nil {
//			slog.Error("shutdown", "err", err)
//			os.Exit(1)
//		}
//	}
//
// Transactions: WithTx runs a function inside a transaction (commit on success,
// rollback on error, rollback-and-repanic on panic); WithTxRetry adds an automatic
// retry loop for serialization failures and deadlocks (SQLSTATE 40001 / 40P01).
//
// Error classification: IsUniqueViolation, IsForeignKeyViolation, IsCheckViolation,
// IsNotFound, and IsSerializationFailure match the underlying *pgconn.PgError (or pgx.ErrNoRows) so
// call sites branch without importing pgconn or matching SQLSTATE strings.
//
// Identifiers: Open installs RegisterIDTypes on every pooled connection, so an
// id.UUID binds as its raw 16 bytes instead of being formatted to the canonical
// string and parsed back on every query. Call it yourself only for a *pgx.Conn you
// built outside this package.
//
// Sentinel errors returned by this package — ErrInvalidConfig, ErrConnect,
// ErrHealthcheck — are single-line and matchable with errors.Is.
//
// Configuration is supplied through Config, whose env struct tags are inert (no
// loader is imported). DefaultConfig is the single source of truth for defaults:
//
//	Field              Env var                  Default
//	URL                DB_URL                   "" (required)
//	MaxConns           DB_MAX_CONNS             10
//	MinConns           DB_MIN_CONNS             2
//	MaxConnLifetime    DB_MAX_CONN_LIFETIME     30m
//	MaxConnIdleTime    DB_MAX_CONN_IDLE_TIME    10m
//	HealthCheckPeriod  DB_HEALTH_CHECK_PERIOD   1m
//	ConnectTimeout     DB_CONNECT_TIMEOUT       5s
//	RetryAttempts      DB_RETRY_ATTEMPTS        3
//	RetryInterval      DB_RETRY_INTERVAL        1s
package postgres
