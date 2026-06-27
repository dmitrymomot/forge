# Design: database connectivity package group (`postgres`, `mongo`, `redis`, `opensearch`, `migration`)

- **Date:** 2026-06-27
- **Status:** Draft for review
- **Scope:** Five new flat top-level packages that cut the **per-app boilerplate** of the
  framework's data backends — `postgres` (pgx/v5 + pgxpool), `mongo` (mongo-driver/v2),
  `redis` (go-redis/v9, Valkey-compatible, standalone/cluster/sentinel), `opensearch`
  (opensearch-go/v4), and `migration` (goose/v3 over the stdlib `*sql.DB` seam). Each
  connectivity package applies one shared convention — `Config` + `DefaultConfig` +
  `Validate`, code-only functional options, the three lifecycle helpers `Open` (with
  connect-retry) / `Close(client, logger)` / `Healthcheck` — and **returns the native
  driver client**, then layers the recurring chores every app re-hand-rolls: transactions,
  **boot-time schema setup** (migrations, index/mapping/shard provisioning), **error
  classification** (`Is…` predicates over driver errors), and a few **typed conveniences**.
  It draws the line short of a general ORM/query-builder/row-scanner — broad typed
  data-access stays with the future `kv`/`data` layer. No `supervisor.Service` adapter. Each
  driver dependency is confined to its own package.

## Overview

These packages do one thing: turn a `Config` (typically loaded from the environment)
into a live, pooled, health-checkable driver client with production-sane defaults,
bounded startup retry, and clean shutdown — then get out of the way. They are the
data-layer analogue of `httpserver`: a well-tested helper layer over a hardened
third-party client — house conventions (`Config`/`Validate`, options, sentinel errors) and
the recurring setup/error/transaction chores — without ever hiding the client underneath.

A consumer calls `Open`, uses the returned `*pgxpool.Pool` / `*mongo.Client` /
`redis.UniversalClient` / `*opensearch.Client` directly with the full driver API, hands
`Healthcheck(client)` to its readiness probe, `defer`s `Close(client, logger)` for ordered
shutdown, and reaches for the per-package setup/error/typed helpers to delete boilerplate.

```go
func main() {
	ctx := supervisor.NewContext() // SIGINT/SIGTERM

	cfg := postgres.DefaultConfig()
	_ = env.ParseWithOptions(&cfg, env.Options{Prefix: "DATABASE_"})

	pool, err := postgres.Open(ctx,
		postgres.WithConfig(cfg),
		postgres.WithLogger(logger),
		postgres.WithMigrator(migration.New(migrationsFS)), // up-migrate on app start
	)
	if err != nil {
		logger.Error("db open failed", "err", err)
		os.Exit(1)
	}
	defer postgres.Close(pool, logger) // logs + closes AFTER Run returns

	err = supervisor.Run(ctx,
		// routes wires postgres.Healthcheck(pool) — func(ctx) error — into /readyz
		supervisor.WithService(httpserver.New(routes(pool))),
	)
	if err != nil {
		logger.Error("shutdown", "err", err)
		os.Exit(1)
	}
}
```

**Why packages and not just "call the driver"?** Each driver makes you re-solve the same
boot-time chores at every app: parse a URL/URI into a pool config, set non-footgun pool
limits and timeouts (drivers ship permissive zero-values), retry the first connection
with backoff so a container that races its database doesn't crash-loop, ping to verify
liveness, and wire close into shutdown without racing in-flight work. forge solves each
once per backend, in forge's idiom, and pins/audits the one driver dependency behind a
single import boundary.

## Architecture

Five flat packages at the repo root, beside `supervisor/`, `httpserver/`, `render/`:

```
postgres/      pgx/v5 + pgxpool                  → *pgxpool.Pool
mongo/         go.mongodb.org/mongo-driver/v2    → *mongo.Client
redis/         github.com/redis/go-redis/v9      → redis.UniversalClient (standalone/cluster/sentinel; Valkey too)
opensearch/    opensearch-project/opensearch-go/v4 → *opensearch.Client
migration/     github.com/pressly/goose/v3       → *migration.Migrator (operates on *sql.DB)
```

Three structural rules, all inherited from existing forge packages:

1. **Each driver dependency is confined to exactly one package.** Importing
   `forge/postgres` is the deliberate act of taking the pgx dependency; nothing else in
   the framework pulls it in. This is the `logger/sentry` isolation pattern applied at
   the top level (the package *is* the boundary). The rest of forge stays driver-free.
2. **No package imports `supervisor`.** The connectivity packages open and close *resources*,
   not long-running services. A pool is opened in `main` and closed with `defer Close(...)`;
   it is not wrapped as a `supervisor.Service`. So these packages have no relationship to
   `supervisor` at all — they are plain constructors plus three lifecycle helpers.
3. **No shared base package (yet).** The convention below is *copied* into each
   connectivity package rather than factored into a `dataconn` helper. This follows the
   framework's standing decision to defer a shared base until 2–3 real implementations
   exist and duplication actually hurts. The connect-retry loop (~15 lines) is the only
   real candidate for later extraction; the spec flags it but does not build it.

`migration` is independent of all four connectivity packages and of pgx — it operates on
the stdlib `*sql.DB` interface. It meets `postgres` only at a one-method interface seam
(below). It is forge's goose-isolation boundary.

## The shared convention (identical shape in `postgres`, `mongo`, `redis`, `opensearch`)

Every connectivity package exposes the same surface, differing only in the native client
type and the contents of `Config`.

### `Config` + `DefaultConfig` + `Validate`

`Config` holds serializable settings with **inert `env:"..."` tags** — the package
imports no config loader. Defaults live solely in `DefaultConfig()` (there are **no
`envDefault` tags** to drift from it — this modernizes the old `pkg/db` wrapper, which
used `envDefault`). The consumer seeds from `DefaultConfig()` and parses the environment
over it with whatever env loader they use.

```go
// DefaultConfig is the single source of truth for defaults.
func DefaultConfig() Config { ... }

// Validate reports unusable values as an ErrInvalidConfig-wrapped, single-line
// errors.Join. Open calls it defensively; callers may call it after env-loading.
func (c Config) Validate() error { ... }
```

### Options (config + code values)

Every package ships **`WithConfig(cfg Config)`** — the serializable block — plus
`WithLogger(*slog.Logger)` and a single **native-config escape hatch**:
`WithPoolConfig(func(*pgxpool.Config))` (postgres), `WithOptions(func(*redis.Options))`
(redis), `WithClientOptions(func(*options.ClientOptions))` (mongo),
`WithClientConfig(func(*opensearch.Config))` (opensearch). `postgres` adds `WithMigrator`.

There is **no positional `cfg`** on `Open` — config arrives through `WithConfig`, exactly
as `httpserver.New(handler, opts...)` and `supervisor.Run(ctx, opts...)` take their config.
This keeps every forge constructor the same shape: `Verb(required, opts...)`. Options apply
in order: `WithConfig` sets the whole block, the escape hatch runs **last** (after the
`Config` overlay) so anything the serializable fields don't cover (TLS, tracers, connect
hooks, custom dialers) stays reachable. Nil function/pointer arguments are rejected into an
accumulated error surfaced by `Open` (the `httpserver` option-error pattern).

### `Open`

```go
func Open(ctx context.Context, opts ...Option) (*Client, error)
```

Flow: start from `DefaultConfig()` → apply options (`WithConfig` replaces the block; code
options set loggers/hooks/migrator) → `Validate()` (return `ErrInvalidConfig` on failure,
e.g. a missing `URL`) → parse the URL/URI into the driver's config → overlay pool limits +
timeouts from `Config` → run the escape hatch → **connect with bounded retry/backoff**
(below) → **ping** to confirm a live server → (postgres only) run the migrator if one was
supplied → return the native client. Any failure returns a sentinel-wrapped, single-line
error and leaks nothing (a partially opened client is closed before returning). Omitting
`WithConfig` runs on pure `DefaultConfig()` — which fails `Validate` for backends whose URL
is required.

`MustOpen` is intentionally **omitted** (the old wrapper's `os.Exit` convenience);
applications handle the returned error.

### Connect-with-retry

A small in-package loop (no dependency on the not-yet-built `backoff` primitive):
attempt `Open`+`Ping`; on failure wait `RetryInterval · 2^attempt` capped at a small
ceiling (mild improvement over the old wrapper's linear `(i+1)·interval`), honoring
`ctx` cancellation between attempts; give up after `RetryAttempts`, returning
`ErrConnect` joined with the last driver error. `RetryAttempts <= 1` means a single
attempt with no wait. This is the one piece of genuinely testable logic that needs no
real server (point it at an unreachable address with short timeouts).

### `Healthcheck` — ping closure for readiness

```go
func Healthcheck(client *Client) func(ctx context.Context) error
```

Returns a closure that pings the backend (wrapping failure in `ErrHealthcheck`) — the
exact `func(context.Context) error` shape a readiness/liveness probe wants. Hand it to the
app's `/readyz` handler; it is stateless and safe to call on every probe.

### `Close` — log-and-close lifecycle helper

```go
func Close(client *Client, log *slog.Logger)
```

Logs a single "closing …" line and closes the client/pool. Used as
`defer Close(client, logger)` in `main`, so it runs *after* `supervisor.Run` returns —
i.e., after the HTTP server and every worker have drained, which is the only point at
which closing is guaranteed not to race in-flight work (see next section). A nil logger is
tolerated (close still happens; the log line is skipped). It takes no `ctx` because the
underlying client closes are synchronous and unconditional.

That is the **entire lifecycle surface** — `Open`, `Close`, `Healthcheck`. There is no
`supervisor.Service` adapter: a connection pool is a resource managed by `main`, not a
supervised unit of work.

### Errors, docs, tests

`errors.go` holds sentinels (`ErrInvalidConfig`, `ErrConnect`, `ErrHealthcheck`, plus
per-package ones), all `errors.Is`-matchable, single-line, no embedded blobs. `doc.go`
carries the package doc with the env-var table and runnable examples. Tests are
black-box (`package postgres_test`).

## Lifecycle & shutdown ordering

`supervisor.Run`, on shutdown, cancels **all** services' contexts at once and drains them
**in parallel** — there is no ordering or phasing. So the pool must outlive the drain: if
anything closed it mid-shutdown, a still-draining `httpserver` could lose **in-flight
queries** mid-request. The clean guarantee is structural — `defer Close(pool, logger)` in
`main` runs only *after* `supervisor.Run` returns, i.e. after the HTTP server and every
worker have finished draining. Closing the pool last therefore needs no machinery: it
falls out of ordinary `defer` ordering, which is exactly why the connectivity packages
ship **no `supervisor.Service` adapter** and model the client as a `main`-owned resource.

(An earlier draft modeled each pool as a supervised liveness probe; it was dropped — the
probe is generic, not per-DB, and the bulk of its value, "restart on a dead backend," is a
~5-line `supervisor.WithServiceFunc` over `Healthcheck(pool)` the consumer writes if and
when they want it.)

## Package: `postgres`

Targets PostgreSQL 18; pgx is server-version-agnostic, so nothing is 18-specific — the
package simply does not get in the way of any PG18 feature.

```go
type Config struct {
	URL               string        `env:"URL"`                 // postgres://… (required)
	MinConns          int32         `env:"MIN_CONNS"`
	MaxConns          int32         `env:"MAX_CONNS"`
	MaxConnLifetime   time.Duration `env:"MAX_CONN_LIFETIME"`
	MaxConnIdleTime   time.Duration `env:"MAX_CONN_IDLE_TIME"`
	HealthCheckPeriod time.Duration `env:"HEALTH_CHECK_PERIOD"` // pgxpool's own idle-conn check
	ConnectTimeout    time.Duration `env:"CONNECT_TIMEOUT"`
	RetryAttempts     int           `env:"RETRY_ATTEMPTS"`
	RetryInterval     time.Duration `env:"RETRY_INTERVAL"`
}

func DefaultConfig() Config // MaxConns 10, MinConns 2, MaxConnLifetime 30m,
                            // MaxConnIdleTime 10m, HealthCheckPeriod 1m,
                            // ConnectTimeout 5s, RetryAttempts 3, RetryInterval 1s
func Open(ctx context.Context, opts ...Option) (*pgxpool.Pool, error)
func Close(pool *pgxpool.Pool, log *slog.Logger)                 // log + pool.Close()
func Healthcheck(pool *pgxpool.Pool) func(context.Context) error // pool.Ping closure for /readyz

// Migrator is the one-method seam to migration (structural; postgres does not import it).
// *migration.Migrator satisfies it, so WithMigrator(migration.New(fsys)) just works.
type Migrator interface {
	Up(ctx context.Context, db *sql.DB) error
}

// Options
func WithConfig(cfg Config) Option                   // the serializable block; defaults to DefaultConfig()
func WithLogger(l *slog.Logger) Option
func WithPoolConfig(fn func(*pgxpool.Config)) Option // escape hatch: tracers/hooks/anything not in Config
func WithMigrator(m Migrator) Option                 // runs m.Up after connect; nil is rejected

// Transaction helpers
func WithTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error
func WithTxRetry(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error, opts ...RetryOption) error

// Error classification (over *pgconn.PgError SQLSTATE; all take a wrapped error)
func IsUniqueViolation(err error) bool     // 23505
func IsForeignKeyViolation(err error) bool // 23503
func IsNotFound(err error) bool            // pgx.ErrNoRows
func IsSerializationFailure(err error) bool // 40001 / 40P01 (deadlock)
```

`WithPoolConfig` runs last in `Open` (after the `Config` overlay), so it is the final
escape hatch for anything the `Config` fields don't cover — a query tracer, `AfterConnect`
hooks, a custom TLS config. `WithTx` begins a transaction, runs `fn`, commits on success,
rolls back on error, and rolls back then re-panics on panic (the old wrapper's semantics,
preserved). `WithTxRetry` is `WithTx` plus an automatic retry loop when the transaction
fails with a serialization failure or deadlock (SQLSTATE `40001`/`40P01`) — bounded by
`RetryOption`s (max attempts, backoff), removing the hand-rolled retry every
`SERIALIZABLE`/`REPEATABLE READ` workload otherwise grows. The **error-classification
predicates** wrap `errors.As(err, &*pgconn.PgError)` + SQLSTATE comparison (and
`pgx.ErrNoRows` for `IsNotFound`), so app code writes `if postgres.IsUniqueViolation(err)`
instead of importing `pgconn` and string-matching codes at every call site.

## Package: `migration`

Goose-backed, operating purely on `*sql.DB` and `io/fs`. Knows nothing about pgx.
Deliberately **up-only** — it applies all pending migrations and nothing else. There is no
`Down`/`Version`/`Status`/`reset`/`redo`/step API; rollbacks and inspection are done with
the `goose` CLI against the same table, out of band. This is the whole package surface:

```go
func New(fsys fs.FS, opts ...Option) *Migrator

func (m *Migrator) Up(ctx context.Context, db *sql.DB) error // apply all pending migrations

// Options (minimal)
func WithTable(name string) Option    // goose version table; default "schema_migrations"
func WithLogger(l *slog.Logger) Option
```

Dialect is fixed to Postgres (the framework's declared database), so there is no dialect
option. Migrations live at the root of `fsys`; embed a subdirectory with `fs.Sub` if
needed (no `WithDir` knob).

**Improvement over the old `pkg/db.Migrate`:** it used goose's *global* mutable state
(`goose.SetBaseFS`, `goose.SetTableName`, `goose.SetDialect`) — unsafe for a library and
non-reentrant. This package uses goose's instance-based **`Provider` API**
(`goose.NewProvider(goose.DialectPostgres, db, fsys, goose.WithTableName(...))` then
`provider.Up(ctx)`), so each `Migrator` is self-contained and two of them never clobber
each other.

`migration` deliberately exposes no pgx and no pool. The pgx → `*sql.DB` bridge is the
*caller's* concern, performed by `postgres.WithMigrator`. `*Migrator` satisfies the
one-method `postgres.Migrator` interface, so it is passed straight in.

## The migration seam (`postgres` ↔ `migration`)

The two packages meet only at the one-method interface `postgres.Migrator`
(`Up(ctx, *sql.DB) error`), which `*migration.Migrator` satisfies structurally — so the
`*Migrator` value is passed straight into `WithMigrator`, no `.Up`, no closure, and
`postgres` never imports `migration`.

`postgres.WithMigrator` stores the `Migrator`; inside `Open`, after the pool is live and
pinged, it bridges with `stdlib.OpenDBFromPool(pool)` (from `github.com/jackc/pgx/v5/stdlib`)
and calls `m.Up` with the resulting `*sql.DB`. **It must not `Close()` that `*sql.DB`** —
`OpenDBFromPool` shares the pool's connections, and closing it would tear down the live
pool (the old wrapper's warning, preserved). Migration failure closes the pool and returns
the error, so a failed migration is a failed `Open`.

Result: `postgres` imports `pgx/v5/stdlib` (part of the already-sanctioned pgx
dependency) but **not goose**; `migration` imports goose but **not pgx**. A consumer who
wants no migrations pays for neither.

```go
pool, err := postgres.Open(ctx,
	postgres.WithConfig(cfg),
	postgres.WithMigrator(migration.New(migrationsFS)))                       // defaults
// or, overriding the version table:
pool, err = postgres.Open(ctx,
	postgres.WithConfig(cfg),
	postgres.WithMigrator(migration.New(migrationsFS, migration.WithTable("schema_migrations"))))
```

## Package: `redis` (Redis + Valkey; standalone / cluster / sentinel)

go-redis/v9 talks to Redis and Valkey identically (same RESP protocol); the package is
documented and smoke-tested against both. All three topologies are covered through
go-redis's **`UniversalClient`**: `Open` builds via `redis.NewUniversalClient`, which
selects standalone, cluster, or sentinel from the options — so `Open` returns the
`redis.UniversalClient` **interface** (the price of one topology-agnostic constructor;
`*redis.Client`/`*redis.ClusterClient`/`*redis.FailoverClient` all satisfy it).

```go
type Config struct {
	Addresses       []string      `env:"ADDRESSES"`     // 1 = standalone, many = cluster
	MasterName      string        `env:"MASTER_NAME"`   // set → sentinel/failover mode
	Username        string        `env:"USERNAME"`
	Password        string        `env:"PASSWORD"`
	DB              int           `env:"DB"`            // standalone/sentinel only (cluster ignores)
	PoolSize        int           `env:"POOL_SIZE"`
	MinIdleConns    int           `env:"MIN_IDLE_CONNS"`
	DialTimeout     time.Duration `env:"DIAL_TIMEOUT"`
	ReadTimeout     time.Duration `env:"READ_TIMEOUT"`
	WriteTimeout    time.Duration `env:"WRITE_TIMEOUT"`
	ConnMaxIdleTime time.Duration `env:"CONN_MAX_IDLE_TIME"`
	RetryAttempts   int           `env:"RETRY_ATTEMPTS"`
	RetryInterval   time.Duration `env:"RETRY_INTERVAL"`
}

func Open(ctx context.Context, opts ...Option) (redis.UniversalClient, error)
func Close(c redis.UniversalClient, log *slog.Logger)                 // log + c.Close()
func Healthcheck(c redis.UniversalClient) func(context.Context) error // PING closure

// Options (same shape as postgres)
func WithConfig(cfg Config) Option
func WithLogger(l *slog.Logger) Option
func WithUniversalOptions(fn func(*redis.UniversalOptions)) Option // escape hatch: TLS, hooks, dialer

// Error classification + typed JSON convenience (reduce app boilerplate)
func IsNil(err error) bool // redis.Nil — a cache miss, not a failure
func GetJSON[T any](ctx context.Context, c redis.Cmdable, key string) (T, error)            // json.Unmarshal; IsNil on miss
func SetJSON(ctx context.Context, c redis.Cmdable, key string, v any, ttl time.Duration) error // json.Marshal + Set
```

Topology is chosen from `Config`: a single `Addresses` entry → standalone, multiple →
cluster, a non-empty `MasterName` → sentinel. `WithUniversalOptions` runs last (after the
`Config` overlay) for `TLSConfig`/`OnConnect`/custom dialer. `IsNil` names the most-checked
redis error (`redis.Nil` = key absent). `GetJSON[T]`/`SetJSON` collapse the ubiquitous
`json.Marshal` → `Set` / `Get` → `json.Unmarshal` dance into one call over any
`redis.Cmdable`; they are point conveniences, not a cache abstraction — the broader cached
`Store` seam remains the future `kv` layer's job. No SQL-style transaction helper (callers
use go-redis `TxPipeline`/`Watch`).

## Package: `mongo`

Official driver v2, latest MongoDB server (8.x).

```go
type Config struct {
	URI                    string        `env:"URI"`                       // mongodb://… (required)
	Database               string        `env:"DATABASE"`                  // optional default db name
	MaxPoolSize            uint64        `env:"MAX_POOL_SIZE"`
	MinPoolSize            uint64        `env:"MIN_POOL_SIZE"`
	ConnectTimeout         time.Duration `env:"CONNECT_TIMEOUT"`
	ServerSelectionTimeout time.Duration `env:"SERVER_SELECTION_TIMEOUT"`
	MaxConnIdleTime        time.Duration `env:"MAX_CONN_IDLE_TIME"`
	ReadPreference         string        `env:"READ_PREFERENCE"`           // primary, primaryPreferred, secondary, …
	ReadConcern            string        `env:"READ_CONCERN"`              // local, majority, snapshot, …
	WriteConcern           string        `env:"WRITE_CONCERN"`             // majority, or a w-number; journaled
	RetryAttempts          int           `env:"RETRY_ATTEMPTS"`
	RetryInterval          time.Duration `env:"RETRY_INTERVAL"`
}

func Open(ctx context.Context, opts ...Option) (*mongo.Client, error)
func Close(c *mongo.Client, log *slog.Logger)                 // log + c.Disconnect(ctx)
func Healthcheck(c *mongo.Client) func(context.Context) error // Ping(readpref.Primary) closure

// Options (same shape as postgres)
func WithConfig(cfg Config) Option
func WithLogger(l *slog.Logger) Option
func WithClientOptions(fn func(*options.ClientOptions)) Option // escape hatch: driver options

// Transaction helper (requires a replica set / mongos — documented)
func WithTransaction(ctx context.Context, c *mongo.Client, fn func(ctx context.Context) error) error

// Boot-time schema setup (idempotent)
func EnsureIndexes(ctx context.Context, db *mongo.Database, specs map[string][]mongo.IndexModel) error
func EnableSharding(ctx context.Context, c *mongo.Client, db string) error
func ShardCollection(ctx context.Context, c *mongo.Client, namespace string, key bson.D) error

// Error classification
func IsDuplicateKey(err error) bool // E11000 (incl. inside BulkWriteException)
func IsNotFound(err error) bool     // mongo.ErrNoDocuments
```

*Connection (concerns):* `ReadPreference`/`ReadConcern`/`WriteConcern` are parsed into the
driver's typed values and applied to the client — the verbose-to-hand-build knobs that
matter most on replica sets and sharded clusters. `mongo.Client.Disconnect` takes a `ctx`,
but `Close` keeps the uniform no-`ctx` signature by disconnecting under a short internal
bounded context.

*Setup (run at boot, after `Open`):* `EnsureIndexes` creates the declared indexes per
collection idempotently (`CreateMany` is itself idempotent by index spec) — the
ever-present "make sure my indexes exist" boilerplate, in one declarative call.
`EnableSharding`/`ShardCollection` wrap the `admin` commands so a sharded deployment can be
provisioned from app setup; on a non-sharded server they return the driver error verbatim
rather than pretending. (Targeted vs scatter-gather reads need no helper — include the
shard key in the filter; forge documents this rather than wrapping queries, staying clear
of data-access.)

*Errors:* `IsDuplicateKey`/`IsNotFound` name the two most-checked Mongo conditions so app
code stops unwrapping `mongo.WriteException`/`mongo.ErrNoDocuments` by hand.

`WithTransaction` runs `fn` inside a session via the driver's `WithTransaction` (commit on
success, abort on error); documented as requiring a replica set/mongos, returning the
driver's error verbatim on a standalone server.

## Package: `opensearch`

opensearch-go/v4. Returns the base `*opensearch.Client`; callers use `opensearchapi` for
typed requests.

```go
type Config struct {
	Addresses          []string      `env:"ADDRESSES"`            // comma-separated in env
	Username           string        `env:"USERNAME"`
	Password           string        `env:"PASSWORD"`
	InsecureSkipVerify bool          `env:"INSECURE_SKIP_VERIFY"` // dev/self-signed only
	MaxRetries         int           `env:"MAX_RETRIES"`
	RequestTimeout     time.Duration `env:"REQUEST_TIMEOUT"`
	RetryAttempts      int           `env:"RETRY_ATTEMPTS"`
	RetryInterval      time.Duration `env:"RETRY_INTERVAL"`
}

func Open(ctx context.Context, opts ...Option) (*opensearch.Client, error)
func Close(c *opensearch.Client, log *slog.Logger)                 // idles transport; mostly a no-op
func Healthcheck(c *opensearch.Client) func(context.Context) error // cluster health / info closure

// Options (same shape as postgres)
func WithConfig(cfg Config) Option
func WithLogger(l *slog.Logger) Option
func WithClientConfig(fn func(*opensearch.Config)) Option // escape hatch: custom transport/TLS

// Declarative index/mapping setup — the search analogue of migration (run at boot)
func NewSetup(fsys fs.FS, opts ...SetupOption) *Setup
func (s *Setup) Apply(ctx context.Context, c *opensearch.Client) error
func WithUpdateMappings(enabled bool) SetupOption // PUT additive mappings onto existing indices; default false

// Error classification
func IsNotFound(err error) bool // 404 (index/document absent)
```

Same three-helper lifecycle as `postgres`; `WithClientConfig` is the `WithPoolConfig`
analogue. `opensearch.Client` is HTTP-based with no long-lived sockets, so `Close` just
idles transport connections (and logs) — kept so every backend reads `Open` / `Close` /
`Healthcheck`.

**`Setup` is OpenSearch's `migration`.** You embed index and template definitions as JSON
in an `fs.FS` (`<name>.index.json`, `<name>.template.json`); `Apply` creates each absent
index/template idempotently (templates `PUT`-upsert; indices create-if-absent). With
`WithUpdateMappings(true)` it also `PUT`s mappings onto already-existing indices for
additive field changes (OpenSearch forbids non-additive mapping changes — those remain a
reindex the consumer drives, the same boundary `migration` draws at destructive SQL). It is
deliberately **forward-only**, matching `migration`'s up-only stance. `IsNotFound` names the
404 every "does this index/doc exist" check reduces to.

## Errors convention

Each package defines `errors.New("postgres: ...")`-style sentinels:
`ErrInvalidConfig`, `ErrConnect`, `ErrHealthcheck`, plus package-specific ones (e.g.
`migration.ErrMigrate`). Failures wrap a sentinel with the underlying driver error via
`fmt.Errorf("%w: %v", ErrConnect, err)` or `errors.Join`, kept single-line per the
framework's structured-logging rule (no embedded stacks/multi-line blobs). Callers branch
with `errors.Is`.

Distinct from forge's own sentinels are the **`Is…` classification predicates** over the
*driver's* errors — `postgres.IsUniqueViolation`/`IsForeignKeyViolation`/`IsNotFound`/`IsSerializationFailure`
(SQLSTATE on `*pgconn.PgError`), `mongo.IsDuplicateKey`/`IsNotFound`, `redis.IsNil`,
`opensearch.IsNotFound`. Each unwraps with `errors.As`/`errors.Is` against the driver's
typed error, so app code asks "was this a duplicate key?" without importing `pgconn`,
matching SQLSTATE strings, or unwrapping `mongo.WriteException` at every call site. They are
the single highest-leverage boilerplate cut in this group.

## Testing strategy

The *connection* layer is thin enough that real-server tests mostly exercise the driver,
but the added helpers (error classification, schema setup, `WithTxRetry`, typed JSON, the
`opensearch` `Setup` runner) carry genuine forge logic that does deserve coverage — split
across two tiers:

- **Pure black-box unit tests** (always run under `just test`, no server, no Docker):
  `DefaultConfig` values; `Validate` accept/reject matrix; option wiring (`WithConfig` sets
  the block, the escape hatch runs last, nil args rejected); error sentinel mapping; **the
  connect-retry loop** pointed at an unreachable address with tiny timeouts (attempt count,
  `ErrConnect` wrapping, `ctx`-cancellation mid-backoff); **redis topology selection** from
  `Config` (addresses/MasterName → which `UniversalOptions`); **error-classification
  predicates** fed synthetic `*pgconn.PgError`/`mongo.WriteException`/`redis.Nil` values to
  assert each `Is…` matches the right code without a live server. Env-tag presence verified
  by reflection (the `httpserver` config-tag test pattern).
- **Env-gated integration tests** — now higher-ROI than the connection layer alone:
  `WithTxRetry` actually retrying a forced `40001`; `EnsureIndexes` idempotency on re-run;
  `GetJSON`/`SetJSON` round-trip + `IsNil` miss; the `opensearch` `Setup` runner being a
  no-op on second `Apply`. `t.Skip`-ped when `FORGE_TEST_POSTGRES_DSN` / `FORGE_TEST_REDIS_URL`
  / `FORGE_TEST_MONGO_URI` / `FORGE_TEST_OPENSEARCH_ADDR` is unset; CI supplies them via
  GitHub Actions **service containers**, never required for a green local `just test`.
- **No `testcontainers` and no new test-only dependency** — consistent with "minimal
  deps" and "black-box only."

## Dependencies

Five new production dependencies, each isolated to its single package, pinned to current
latest at design time:

| Package | Module | Version |
|---|---|---|
| `postgres` | `github.com/jackc/pgx/v5` (+ `pgxpool`, `stdlib`) | `v5.10.0` |
| `mongo` | `go.mongodb.org/mongo-driver/v2` | `v2.7.0` |
| `redis` | `github.com/redis/go-redis/v9` | `v9.21.0` |
| `opensearch` | `github.com/opensearch-project/opensearch-go/v4` | `v4.6.0` |
| `migration` | `github.com/pressly/goose/v3` | `v3.27.1` |

`pgx` was already the framework's one sanctioned DB dependency; the other four extend the
"buy the wire, build the ergonomics" rule to the remaining backends, each behind its own
import boundary. No package imports `supervisor` — the pools are `main`-owned resources,
not services. `testify` remains test-only.

## Build order

1. **`postgres` + `migration`** together — the reference implementation of the convention
   plus the migrator seam they share. Establishes the copied-convention shape the rest
   follow; postgres also lands `WithTx`/`WithTxRetry` and the SQLSTATE `Is…` predicates.
2. **`redis`** — `UniversalClient` topology selection, `IsNil`, typed `GetJSON`/`SetJSON`.
3. **`mongo`** — concern config, `WithTransaction`, `EnsureIndexes`/sharding setup, error helpers.
4. **`opensearch`** — HTTP client + the declarative `Setup` runner, `IsNotFound`.

Within each package the lifecycle trio comes first; the boilerplate-reducers (setup,
typed/error helpers) layer on top and can land as follow-up commits.

## Non-goals (anti-scope)

- **No general ORM, query builder, or row scanning** — these packages return the raw
  client for all real data access. The included typed helpers are deliberately narrow point
  conveniences (`redis.GetJSON`/`SetJSON`), not an entity/repository layer; anything that
  would grow into struct-tag mapping, query construction, or a `Store` abstraction belongs
  to the future `kv`/`data` layer, not here.
- **No cross-backend abstraction / unified `Store` interface** — each package exposes its
  native client; unifying them is the downstream `kv`/caching layer's job.
- **No `MustOpen`.**
- **No migration down/reset/test-fixture helpers** — `migration` is strictly up-only.
  Resetting a test database (TRUNCATE, schema drop, or an ephemeral/template DB) is the
  consumer's concern. This was considered and deliberately rejected: goose down/reset is
  only as reliable as hand-written `-- +goose Down` sections, so forge does not pretend to
  own clean-slate teardown, and "drop my data" verbs stay out of the package called on
  every boot.
- **No shared base package** until duplication across the four proves painful.

## Open questions / future

- **Retry-loop extraction** — if the copied connect-retry loop turns out byte-identical
  across all four packages, extract a tiny internal helper *after* the fourth lands, not
  before.
- **Generic liveness probe** — if "restart the app when a backend dies" is wanted as more
  than a hand-written `supervisor.WithServiceFunc`, a single DB-agnostic probe (wrapping
  any `Healthcheck`-style `func(ctx) error`) belongs in a future shared package, not in
  each connectivity package.
- **OpenSearch non-additive mappings** — `Setup` is forward/additive-only; a
  reindex-on-breaking-change helper is deferred until there's demand (a real reindex needs
  app-specific source/target/transform decisions forge shouldn't guess).
- **Typed conveniences vs the `kv` layer** — `redis.GetJSON`/`SetJSON` are point helpers
  here; if a cross-backend cached `Store` materializes in the `kv` layer, these stay as
  thin shortcuts and do not grow into it.
- **Naming collisions** — `redis`, `mongo`, `opensearch` share their package names with
  the underlying drivers; a consumer importing both forge's package and the driver's
  aliases the driver (`goredis "github.com/redis/go-redis/v9"`), the idiomatic Go
  resolution. Package names stay natural.
