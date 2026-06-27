// Package opensearch turns a Config into a live, health-checkable OpenSearch
// client (opensearch-go/v4) with production-sane defaults, bounded startup retry,
// and a declarative boot-time index/template setup runner — then gets out of the
// way. Open returns the native *opensearch.Client; callers use the opensearchapi
// subpackage for typed requests. Hand Healthcheck(client) to a readiness probe and
// defer Close(client, logger) in main.
//
// # Configuration
//
// Config carries serializable settings with inert env struct tags (no envDefault;
// DefaultConfig is the single source of truth). Seed from DefaultConfig and parse
// the environment over it with any env loader. Addresses is a []string; a
// comma-separated value parses into it under ADDRESSES.
//
//	Field               Env var                 Default   Notes
//	Addresses           ADDRESSES               (none)    node URLs; required (comma-separated in env)
//	Username            USERNAME                ""        HTTP basic auth user
//	Password            PASSWORD                ""        HTTP basic auth password
//	InsecureSkipVerify  INSECURE_SKIP_VERIFY    false     skip TLS verify (dev/self-signed only)
//	MaxRetries          MAX_RETRIES             3         driver retry on retriable status codes
//	RequestTimeout      REQUEST_TIMEOUT         10s       per-request timeout (transport + ctx)
//	RetryAttempts       RETRY_ATTEMPTS          3         Open's bounded connect-retry attempts
//	RetryInterval       RETRY_INTERVAL          1s        base connect backoff (interval * 2^attempt, capped 30s)
//
// # Lifecycle
//
//	func main() {
//		ctx, stop := supervisor.NewContext()
//		defer stop()
//
//		cfg := opensearch.DefaultConfig()
//		_ = env.ParseWithOptions(&cfg, env.Options{Prefix: "OPENSEARCH_"})
//
//		client, err := opensearch.Open(ctx, opensearch.WithConfig(cfg), opensearch.WithLogger(logger))
//		if err != nil {
//			logger.Error("opensearch open failed", "err", err)
//			os.Exit(1)
//		}
//		defer opensearch.Close(client, logger) // runs after supervisor.Run returns
//
//		// Provision indices/templates embedded in a fs.FS (forward-only).
//		if err := opensearch.NewSetup(setupFS).Apply(ctx, client); err != nil {
//			logger.Error("opensearch setup failed", "err", err)
//			os.Exit(1)
//		}
//
//		err = supervisor.Run(ctx,
//			// routes wires opensearch.Healthcheck(client) — func(ctx) error — into /readyz
//			supervisor.WithService(httpserver.New(routes(client))),
//		)
//		if err != nil {
//			logger.Error("shutdown", "err", err)
//			os.Exit(1)
//		}
//	}
//
// # Setup
//
// NewSetup reads <name>.index.json and <name>.template.json from the root of an
// fs.FS. Apply creates absent indices, PUT-upserts templates, and — with
// WithUpdateMappings(true) — PUTs additive mappings onto existing indices. It is
// forward-only and idempotent: a second Apply with no FS changes makes no mutating
// index create. Non-additive mapping changes remain a consumer-driven reindex.
//
// # Errors
//
// Failures wrap single-line sentinels matchable with errors.Is: ErrInvalidConfig,
// ErrConnect, ErrHealthcheck, ErrSetup. IsNotFound classifies a driver 404 (absent
// index or document) over opensearch-go's typed *StructError/*StringError.
package opensearch
