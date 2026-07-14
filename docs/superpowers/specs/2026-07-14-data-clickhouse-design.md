# data/clickhouse — Design

## Summary

A ClickHouse connection factory in the `data/postgres` mold. Connection only: query building, batching, and schema stay consumer-side. It resolves a serializable `Config` (DSN + pool/timeout overlays) into `*clickhouse.Options`, connects with a bounded retry/backoff ping, and returns the driver's real connection type. Two constructors share one build pipeline: `Open` returns the native `clickhouse.Conn` (batch/columnar API), `OpenDB` returns a `database/sql` `*sql.DB` (goose/sqlc/stdlib). Ships lifecycle helpers (`Close`, `Healthcheck`, `HealthcheckDB`) and a small `classify.go` of `*clickhouse.Exception` predicates.

Roadmap entry (`docs/packages.md`): "ClickHouse connection factory in the `data/postgres` mold: DSN config with Validate, pooling, health ping. Connection only — query building and schema stay consumer-side. Deps: none forge-internal (driver external)."

## Driver

`github.com/ClickHouse/clickhouse-go/v2` — a new direct dependency, isolated in this package per the framework's "isolate every real dep in a driver subpackage" rule. The `github.com/ClickHouse/clickhouse-go-linter` is already an indirect dep in `go.mod`; the repo's lint will enforce correct clickhouse-go usage (context passing, closing batches/rows). The driver exposes both interfaces over one `*clickhouse.Options`: `clickhouse.Open(opts) (clickhouse.Conn, error)` (native) and `clickhouse.OpenDB(opts) *sql.DB` (database/sql; construction returns no error, errors surface on first use/ping). `clickhouse.ParseDSN(dsn) (*clickhouse.Options, error)` parses a `clickhouse://` DSN — including multiple hosts and most tuning knobs as query params — into `*clickhouse.Options`.

## Public API

```go
// Two constructors over one shared Config → *clickhouse.Options builder.
func Open(ctx context.Context, opts ...Option) (clickhouse.Conn, error)   // native: PrepareBatch, AsyncInsert, Select, QueryRow, Exec
func OpenDB(ctx context.Context, opts ...Option) (*sql.DB, error)          // database/sql: goose, sqlc, stdlib

// Lifecycle. Both clickhouse.Conn and *sql.DB satisfy io.Closer, so one Close covers both.
func Close(c io.Closer, log *slog.Logger)
func Healthcheck(conn clickhouse.Conn) func(context.Context) error         // conn.Ping
func HealthcheckDB(db *sql.DB) func(context.Context) error                 // db.PingContext

// classify.go — predicates over *clickhouse.Exception (carries an integer Code).
func Code(err error) (int32, bool)
func IsCode(err error, code int32) bool
func IsTableNotFound(err error) bool     // 60  UNKNOWN_TABLE
func IsDatabaseNotFound(err error) bool  // 81  UNKNOWN_DATABASE
func IsAlreadyExists(err error) bool     // 57  TABLE_ALREADY_EXISTS
func IsAuthFailed(err error) bool        // 516 AUTHENTICATION_FAILED
```

`Close` takes `io.Closer` because both returned types implement `Close() error`; it logs intent (when logger non-nil), closes, and logs a close error. `Healthcheck`/`HealthcheckDB` are split because the ping method names differ (`clickhouse.Conn.Ping(ctx)` vs `*sql.DB.PingContext(ctx)`); each returns a stateless `func(context.Context) error` closure shaped for a readiness/liveness probe, wrapping failures in `ErrHealthcheck`. Exact ClickHouse error-code integers are pinned against the driver's error-code table during implementation.

## Config (minimal, postgres-mirrored)

```go
type Config struct {
    DSN             string        `env:"CLICKHOUSE_DSN"`               // clickhouse://user:pass@host:9000/db?param=value (required)
    ConnMaxLifetime time.Duration `env:"CLICKHOUSE_CONN_MAX_LIFETIME"` // close a conn this long after creation
    DialTimeout     time.Duration `env:"CLICKHOUSE_DIAL_TIMEOUT"`      // per-attempt dial+handshake bound
    RetryInterval   time.Duration `env:"CLICKHOUSE_RETRY_INTERVAL"`    // base backoff between connect attempts
    MaxOpenConns    int           `env:"CLICKHOUSE_MAX_OPEN_CONNS"`    // pool ceiling
    MaxIdleConns    int           `env:"CLICKHOUSE_MAX_IDLE_CONNS"`    // idle pool size
    RetryAttempts   int           `env:"CLICKHOUSE_RETRY_ATTEMPTS"`    // total connect attempts; <=1 means one, no wait
}
```

`Config` is deliberately minimal, mirroring `data/postgres`. The env prefix is the full word `CLICKHOUSE_` per the naming rule (no ad-hoc abbreviations). Env struct tags are inert strings — this package imports no config loader; a consumer populates `Config` with any env loader by seeding from `DefaultConfig`. Field order is subject to betteralign. Everything ClickHouse-specific not listed (TLS, the `Settings` map, block buffer size, custom auth, HTTP vs native protocol) rides the DSN query params or the `WithOptions` escape hatch.

`DefaultConfig()` returns production-sane pool/timeout defaults and is the single source of truth for them (no `envDefault` tags to drift from). `DSN` is left empty and must be supplied, so `DefaultConfig` alone fails `Validate`. Indicative defaults (final values set in implementation): `MaxOpenConns` ~10, `MaxIdleConns` ~5, `ConnMaxLifetime` ~30m, `DialTimeout` ~5s, `RetryAttempts` 3, `RetryInterval` 1s.

`Validate()` reports whether the field values are usable, returning an `ErrInvalidConfig`-wrapped, single-line joined error otherwise. Checks: `DSN` non-empty; `MaxOpenConns >= 0`, `MaxIdleConns >= 0`; when `MaxOpenConns > 0`, `MaxIdleConns <= MaxOpenConns`; `RetryAttempts >= 0`; every duration `>= 0`. Callers may call it after loading from env (zero-trust); `Open`/`OpenDB` also call it defensively before any network I/O.

## Options

```go
type config struct {
    logger      *slog.Logger
    withOptions func(*clickhouse.Options)   // escape hatch
    errs        []error
    Config
}
type Option func(*config)

func WithConfig(cfg Config) Option                 // set the whole serializable block; layer code options after it
func WithLogger(l *slog.Logger) Option             // Close/lifecycle logging; nil rejected (ErrInvalidConfig); default slog.Default()
func WithOptions(fn func(*clickhouse.Options)) Option  // single extensibility seam; runs LAST over fully-built options
```

`WithOptions(func(*clickhouse.Options))` is the sole extensibility seam — the analog of `data/postgres`'s `WithPoolConfig`. It runs last, after DSN parse + Config overlay + the LZ4 default, so it can override anything (TLS config, `Settings`, block buffer, protocol, custom dialer). Invalid option values accumulate in `errs` and are returned by the constructor before any I/O.

## Open pipeline (shared by Open and OpenDB)

1. `cfg := config{Config: DefaultConfig()}`; apply options in order; if `len(cfg.errs) > 0` return `errors.Join(cfg.errs...)`; then `cfg.Validate()`. All before any network I/O. Resolve logger (`slog.Default()` when unset).
2. `buildOptions(cfg.Config) (*clickhouse.Options, error)`: `clickhouse.ParseDSN(cfg.DSN)` (parse failure → `ErrConnect`-wrapped), then overlay the Config pool/timeout fields onto the returned `*clickhouse.Options` (`MaxOpenConns`, `MaxIdleConns`, `ConnMaxLifetime`, `DialTimeout`) only when non-zero.
3. LZ4-on-by-default: if the DSN did not set compression (`opts.Compression == nil`), default it to LZ4. A DSN `?compress=…` therefore wins over the default; the escape hatch can override further. Compression needs no `Config` field because the builder owns the default — this keeps `Config` minimal.
4. Run the `WithOptions` escape hatch last, over the fully-built `*clickhouse.Options`.
5. Construct the handle: `Open` calls `clickhouse.Open(opts)` (surfaces the driver's construction error, `ErrConnect`-wrapped); `OpenDB` calls `clickhouse.OpenDB(opts)` (no construction error). Then ping with bounded retry/backoff. On any failure, close the partial handle and return an `ErrConnect`-wrapped, single-line error — leaking nothing.

`pingWithRetry(ctx, ping func(context.Context) error, cfg Config, logger *slog.Logger) error` is shared: `Open` passes `conn.Ping`, `OpenDB` passes `db.PingContext`. It pings up to `RetryAttempts` times (`>= 1`), waiting `backoff(RetryInterval, attempt) = RetryInterval · 2^attempt` capped at ~30s between tries, honoring `ctx.Done()` during the wait, logging a warn line per failed attempt, and returning `ErrConnect` joined with the last error on exhaustion. `RetryAttempts <= 1` means one attempt with no wait. `backoff` guards shift overflow (non-positive or over-cap result → the ~30s cap). This mirrors the retry helpers in `data/postgres`, `data/redis`, and `data/opensearch`.

## errors.go

```go
var (
    ErrInvalidConfig = errors.New("clickhouse: invalid config")   // joined by Validate and the constructors on a bad option/field
    ErrConnect       = errors.New("clickhouse: connect failed")   // pool build / server unreachable within the retry budget
    ErrHealthcheck   = errors.New("clickhouse: healthcheck failed") // wraps a failed liveness ping from a Healthcheck closure
)
```

`errors.Is`-matchable, single-line, distinct from the `classify` predicates (which match the underlying `*clickhouse.Exception`, not these sentinels).

## Files

- `doc.go` — package comment + runnable example showing both paths: `Open` + `PrepareBatch` (native batch insert) and `OpenDB` wired to `data/migration`/goose, plus `defer Close(...)` and a `Healthcheck` handed to a readiness probe.
- `config.go` — `Config`, `DefaultConfig`, `Validate`.
- `options.go` — `config` struct, `Option`, `WithConfig`, `WithLogger`, `WithOptions`.
- `clickhouse.go` — `Open`, `OpenDB`, `buildOptions`, `pingWithRetry`, `backoff`.
- `lifecycle.go` — `Close`, `Healthcheck`, `HealthcheckDB`.
- `classify.go` — `Code`, `IsCode`, `IsTableNotFound`, `IsDatabaseNotFound`, `IsAlreadyExists`, `IsAuthFailed`.
- `errors.go` — the three sentinels.
- `_test.go` per source file — black-box (`package clickhouse_test`) throughout, except the single `buildoptions_test.go` white-box file that asserts the unexported `buildOptions` overlay/LZ4 default.

## Deliberate scope calls

- No migrator seam. Schema stays consumer-side (roadmap). `database/sql` users wire `data/migration`/goose over the returned `*sql.DB` themselves; the `doc.go` example shows this.
- No tenancy `WithScope` hook. A connection factory is stateless infra with no keyed storage to scope — identical to every sibling `data/*` package. The framework multi-tenant rule targets packages that compose storage keys; a connection factory is exempt, and single-tenant use pays zero ceremony either way.
- No `bench_test.go`. Follows the established `data/*` precedent: none of postgres/redis/mongo/opensearch ship one, because a connection factory has no per-request hot path (`Open` runs once at startup). This precedent overrides the repo-wide "benchmarks required" rule for this package class.

## Testing

Black-box tests (`package clickhouse_test`), no live server required for the core suite:

- `config_test.go` — `DefaultConfig` values; `Validate` accept/reject matrix (empty DSN, negatives, `MaxIdleConns > MaxOpenConns`); `ErrInvalidConfig` matching.
- `options_test.go` — option ordering, `WithLogger(nil)` rejection, error accumulation in `errs`.
- `open_test.go` — constructor error paths without a live server: option errors short-circuit before I/O, `Validate` failure returns before I/O, malformed DSN → `ErrConnect`, unreachable host exhausts the retry budget → `ErrConnect` (small `RetryAttempts`/`RetryInterval`), `ctx` cancellation honored during backoff.
- `buildoptions_test.go` — the one warranted white-box test (`package clickhouse`), per the black-box rule's explicit exception for asserting unexported state: calls `buildOptions` directly to assert the Config→Options overlay, the LZ4-on default, and that a DSN `?compress=…` wins over the default. Everything else stays black-box (`package clickhouse_test`).
- `classify_test.go` — construct `*clickhouse.Exception` values directly (exported struct/fields, no live server) to drive `Code`/`IsCode` and each named predicate, including nil and non-exception errors returning false.
- `lifecycle_test.go` — `Close` tolerates nil handle and nil logger; `Healthcheck`/`HealthcheckDB` closures wrap ping failure in `ErrHealthcheck`.
- Optional live integration test guarded by a `CLICKHOUSE_DSN` env var (skipped when unset): `Open` + `PrepareBatch` round-trip and `OpenDB` + `PingContext` against a real ClickHouse (ephemeral container in CI, as the sibling packages run live pg16/redis7).
