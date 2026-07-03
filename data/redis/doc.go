// Package redis turns a Config into a live, pooled, health-checkable go-redis
// client (Redis or Valkey — same RESP protocol) with production-sane defaults,
// bounded startup retry, and clean shutdown, then gets out of the way. It is the
// data-layer analogue of httpserver: a thin, well-tested helper over a hardened
// third-party client that never hides the client beneath it.
//
// The driver is imported aliased as goredis so this package can keep the natural
// name redis; the public API returns the driver's own types — UniversalClient,
// Cmdable — which render in godoc under the driver's package name (redis.*).
//
// # Topology
//
// All three topologies are reached through go-redis's UniversalClient. Open builds
// the client with NewUniversalClient, which selects the topology from Config:
//
//	one Addresses entry, empty MasterName  -> standalone
//	multiple Addresses entries             -> cluster
//	non-empty MasterName                   -> sentinel / failover (Addresses lists the sentinels)
//
// Open returns the UniversalClient interface; *redis.Client, *redis.ClusterClient,
// and *redis.FailoverClient all satisfy it.
//
// # Lifecycle
//
// The entire lifecycle surface is Open, Close, and Healthcheck. A client is a
// resource owned by main, not a supervisor.Service: open it in main, hand
// Healthcheck(client) to the readiness probe, and defer Close(client, logger) so it
// runs after supervisor.Run returns — i.e. after the HTTP server and workers have
// drained, the only point at which closing cannot race in-flight work.
//
//	func main() {
//		ctx, stop := supervisor.NewContext()
//		defer stop()
//
//		cfg := redis.DefaultConfig()
//		_ = env.ParseWithOptions(&cfg, env.Options{Prefix: "REDIS_"})
//
//		client, err := redis.Open(ctx,
//			redis.WithConfig(cfg),
//			redis.WithLogger(logger),
//		)
//		if err != nil {
//			logger.Error("redis open failed", "err", err)
//			os.Exit(1)
//		}
//		defer redis.Close(client, logger) // closes AFTER Run returns
//
//		err = supervisor.Run(ctx,
//			// routes wires redis.Healthcheck(client) — func(ctx) error — into /readyz
//			supervisor.WithService(httpserver.New(routes(client))),
//		)
//		if err != nil {
//			logger.Error("shutdown", "err", err)
//			os.Exit(1)
//		}
//	}
//
// # Configuration
//
// Config carries inert env struct tags; this package imports no config loader. Seed
// from DefaultConfig and parse the environment over it. DefaultConfig is the single
// source of truth for defaults (there are no envDefault tags); Addresses has no
// default and must be supplied.
//
//	Env var (struct tag)  Field            Default   Notes
//	--------------------  ---------------  --------  ---------------------------------
//	ADDRESSES             Addresses        (none)    1 = standalone, many = cluster; required
//	MASTER_NAME           MasterName       ""        set -> sentinel/failover
//	USERNAME              Username         ""        ACL username (Redis 6+)
//	PASSWORD              Password         ""
//	DB                    DB               0         standalone/sentinel only (cluster ignores)
//	POOL_SIZE             PoolSize         10        max connections per node
//	MIN_IDLE_CONNS        MinIdleConns     0
//	DIAL_TIMEOUT          DialTimeout      5s
//	READ_TIMEOUT          ReadTimeout      3s
//	WRITE_TIMEOUT         WriteTimeout     3s
//	CONN_MAX_IDLE_TIME    ConnMaxIdleTime  0         0 = driver default
//	RETRY_ATTEMPTS        RetryAttempts    3         bounded connect-retry in Open
//	RETRY_INTERVAL        RetryInterval    1s        base backoff (doubles per attempt, capped ~30s)
//
// WithUniversalOptions is the escape hatch for anything Config does not cover —
// TLSConfig, OnConnect, a custom Dialer; it runs last in Open, after the Config
// overlay, on the fully-built *redis.UniversalOptions.
//
// # Errors and conveniences
//
// Failures wrap single-line sentinels matchable with errors.Is: ErrInvalidConfig
// (bad Config or option), ErrConnect (connect-retry exhausted), ErrHealthcheck (a
// failed probe ping). Over the driver's own errors, IsNil reports a goredis.Nil
// cache miss. GetJSON[T] and SetJSON collapse the json.Marshal->Set and
// Get->json.Unmarshal dance into one call over any redis.Cmdable; they are point
// conveniences, not a cache abstraction.
package redis
