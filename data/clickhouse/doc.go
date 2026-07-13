// Package clickhouse turns a Config into a live, health-checkable ClickHouse
// connection with production-sane defaults, bounded startup retry, and clean
// shutdown, then gets out of the way. It is a connection factory only: query
// building, batch ingestion, and schema stay consumer-side. It is the analytics-store
// analogue of data/postgres.
//
// The driver (github.com/ClickHouse/clickhouse-go/v2) is imported aliased as ch so
// this package can keep the natural name clickhouse; the public API returns the
// driver's own types — clickhouse.Conn, *sql.DB, clickhouse.Options,
// clickhouse.Exception — which render in godoc under the driver's package name.
// Consumers that import both this package and the driver must alias one of them.
//
// # Two constructors
//
// Open returns the native clickhouse.Conn — PrepareBatch, AsyncInsert, Select,
// QueryRow, Exec — the high-throughput columnar API that is the point of reaching for
// ClickHouse. OpenDB returns a database/sql *sql.DB for goose/sqlc/stdlib ergonomics.
// Both share one Config -> *clickhouse.Options build pipeline and the same bounded
// retry/backoff ping.
//
//	func main() {
//		ctx, stop := supervisor.NewContext()
//		defer stop()
//
//		cfg := clickhouse.DefaultConfig()
//		_ = env.Parse(&cfg) // CLICKHOUSE_DSN=clickhouse://user:pass@host:9000/db
//
//		conn, err := clickhouse.Open(ctx,
//			clickhouse.WithConfig(cfg),
//			clickhouse.WithLogger(logger),
//		)
//		if err != nil {
//			logger.Error("clickhouse open failed", "err", err)
//			os.Exit(1)
//		}
//		defer clickhouse.Close(conn, logger) // closes AFTER Run returns
//
//		// Native batch insert — the columnar ingestion path.
//		batch, _ := conn.PrepareBatch(ctx, "INSERT INTO events (id, ts) VALUES")
//		_ = batch.Append(uint64(1), time.Now())
//		_ = batch.Send()
//
//		err = supervisor.Run(ctx,
//			// routes wires clickhouse.Healthcheck(conn) — func(ctx) error — into /readyz
//			supervisor.WithService(httpserver.New(routes(conn))),
//		)
//		if err != nil {
//			logger.Error("shutdown", "err", err)
//			os.Exit(1)
//		}
//	}
//
// For migrations or database/sql code, open a *sql.DB instead and hand it to
// data/migration or sqlc; wire clickhouse.HealthcheckDB(db) into the readiness probe:
//
//	db, err := clickhouse.OpenDB(ctx, clickhouse.WithConfig(cfg))
//	if err != nil { /* ... */ }
//	defer clickhouse.Close(db, logger)
//
// # Configuration
//
// Config carries inert env struct tags; this package imports no config loader. Seed
// from DefaultConfig and parse the environment over it. DefaultConfig is the single
// source of truth for defaults (there are no envDefault tags); DSN has no default and
// must be supplied.
//
//	Env var (struct tag)          Field            Default   Notes
//	----------------------------  ---------------  --------  ---------------------------------
//	CLICKHOUSE_DSN                DSN              (none)    clickhouse://user:pass@host:9000/db; required
//	CLICKHOUSE_MAX_OPEN_CONNS     MaxOpenConns     10        pool ceiling
//	CLICKHOUSE_MAX_IDLE_CONNS     MaxIdleConns     5         idle pool size
//	CLICKHOUSE_CONN_MAX_LIFETIME  ConnMaxLifetime  30m
//	CLICKHOUSE_DIAL_TIMEOUT       DialTimeout      5s        per-attempt dial+handshake bound
//	CLICKHOUSE_RETRY_ATTEMPTS     RetryAttempts    3         bounded connect-retry in Open/OpenDB
//	CLICKHOUSE_RETRY_INTERVAL     RetryInterval    1s        base backoff (doubles per attempt, capped ~30s)
//
// LZ4 wire compression is enabled by default (a large insert-throughput win) unless
// the DSN sets compress explicitly (compress=zstd, compress=false, ...). WithOptions
// is the escape hatch for anything Config does not cover — TLS, the Settings map,
// block buffer size, a custom dialer, JWT auth; it runs last, on the fully-built
// *clickhouse.Options.
//
// # Errors and conveniences
//
// Failures wrap single-line sentinels matchable with errors.Is: ErrInvalidConfig (bad
// Config or option), ErrConnect (bad DSN or connect-retry exhausted), ErrHealthcheck
// (a failed probe ping). Over the driver's *clickhouse.Exception, Code extracts the
// server error code and IsTableNotFound / IsDatabaseNotFound / IsAlreadyExists /
// IsAuthFailed name the common ones.
package clickhouse
