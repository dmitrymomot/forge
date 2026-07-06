// Package mongo turns a Config (typically env-loaded) into a live, pooled,
// health-checkable MongoDB database handle and layers the recurring boot-time
// chores — transactions, index/shard provisioning, and error classification —
// over the official driver (go.mongodb.org/mongo-driver/v2). Open returns the
// native *mongo.Database scoped to Config.Database; use db.Client() to reach
// client-level ops. Full driver access stays available.
//
// The forge package is itself named mongo, so callers that also import the driver
// alias the driver (the idiomatic Go resolution):
//
//	import (
//		forgemongo "github.com/dmitrymomot/forge/data/mongo"
//		mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
//	)
//
// # Lifecycle
//
// Open applies functional options over DefaultConfig, validates, builds the driver
// client options (URI, pool limits, timeouts, read/write concerns), connects, pings
// with bounded exponential-backoff retry so a container that races its database
// does not crash-loop, and returns client.Database(cfg.Database). Hand
// Healthcheck(db) to a readiness probe and defer Close(db, logger) in main so it
// runs after supervisor.Run returns — after every service has drained, the only
// point at which disconnecting cannot race in-flight work. Use db.Client() to reach
// client-level ops such as StartSession or the sharding helpers.
//
//	func main() {
//		ctx, stop := supervisor.NewContext()
//		defer stop()
//
//		cfg := mongo.DefaultConfig()
//		_ = env.Parse(&cfg)
//
//		db, err := mongo.Open(ctx, mongo.WithConfig(cfg), mongo.WithLogger(logger))
//		if err != nil {
//			logger.Error("mongo open failed", "err", err)
//			os.Exit(1)
//		}
//		defer mongo.Close(db, logger)
//
//		err = supervisor.Run(ctx,
//			supervisor.WithService(httpserver.New(routes(db))),
//		)
//		if err != nil {
//			logger.Error("shutdown", "err", err)
//			os.Exit(1)
//		}
//	}
//
// # Boot-time schema setup
//
// EnsureIndexes creates declared indexes per collection idempotently; run it once
// after Open. EnableSharding and ShardCollection wrap the admin commands for
// sharded deployments and take *mongo.Client (pass db.Client()); they return the
// driver's error verbatim on a non-sharded server.
//
//	specs := map[string][]mongodriver.IndexModel{
//		"users": {{Keys: bson.D{{Key: "email", Value: 1}}, Options: options.Index().SetUnique(true)}},
//	}
//	if err := mongo.EnsureIndexes(ctx, db, specs); err != nil {
//		logger.Error("ensure indexes", "err", err)
//		os.Exit(1)
//	}
//
// # Transactions
//
// WithTransaction runs a callback inside a session-backed transaction (commit on
// nil, abort on error). It requires a replica set or mongos; on a standalone server
// the driver returns its own error verbatim. The callback must use the context it
// is given — that context carries the session.
//
// # Error classification
//
// IsDuplicateKey (E11000, including inside WriteException/BulkWriteException) and
// IsNotFound (mongo.ErrNoDocuments) name the two most-checked Mongo conditions so
// app code stops unwrapping driver error types by hand. The package's own sentinels
// ErrInvalidConfig, ErrConnect, and ErrHealthcheck are matched with errors.Is.
//
// # Configuration
//
// Config carries inert env struct tags; this package imports no config loader. Seed
// from DefaultConfig and parse the environment over it. Defaults live solely in
// DefaultConfig (there are no envDefault tags to drift from it).
//
//	Env var                            Field                   Default
//	-----------------------------------------------------------------------------
//	MONGO_URI                          URI                     "" (required)
//	MONGO_DATABASE                     Database                "" (required; the database Open opens and returns)
//	MONGO_READ_PREFERENCE              ReadPreference          "" (driver default)
//	MONGO_READ_CONCERN                 ReadConcern             "" (driver default)
//	MONGO_WRITE_CONCERN                WriteConcern            "" (driver default)
//	MONGO_MAX_POOL_SIZE                MaxPoolSize             100
//	MONGO_MIN_POOL_SIZE                MinPoolSize             0
//	MONGO_CONNECT_TIMEOUT              ConnectTimeout          10s
//	MONGO_SERVER_SELECTION_TIMEOUT     ServerSelectionTimeout  10s
//	MONGO_MAX_CONN_IDLE_TIME           MaxConnIdleTime         0 (no cap)
//	MONGO_RETRY_ATTEMPTS               RetryAttempts           3
//	MONGO_RETRY_INTERVAL               RetryInterval           1s
//
// MONGO_READ_PREFERENCE accepts: primary, primaryPreferred, secondary,
// secondaryPreferred, nearest. MONGO_READ_CONCERN accepts: local, majority, available,
// linearizable, snapshot. MONGO_WRITE_CONCERN accepts: majority, journaled,
// unacknowledged, or a w-number ("0", "1", "2", …). An empty concern leaves the
// driver default in place; an unknown value fails Validate.
//
// The WithClientOptions escape hatch runs last in Open, after the Config-derived
// options, for anything Config does not cover (TLS, monitors, custom dialer, auth).
//
// # Testing
//
// Unit tests run with no server under `just test`. Integration tests are env-gated
// and skip when their variable is unset: FORGE_TEST_MONGO_URI (lifecycle, indexes),
// FORGE_TEST_MONGO_RS_URI (WithTransaction — replica set), and
// FORGE_TEST_MONGO_SHARDED_URI (EnableSharding/ShardCollection — mongos).
package mongo
