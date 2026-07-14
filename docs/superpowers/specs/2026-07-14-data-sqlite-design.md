# data/sqlite — Design Spec

Status: approved for planning · Date: 2026-07-14 · Branch: dm/sqlite-package-brainstorm-559219

## Purpose

A cgo-free SQLite connection factory in the `data/postgres` mold, tuned by default for maximum read+write throughput on a single node. The package owns SQLite's pragma discipline (WAL, `busy_timeout`, `synchronous`, `foreign_keys`, cache/mmap) and its single-writer concurrency model behind a reader/writer pool split, then gets out of the way by exposing native `*sql.DB` handles. `modernc.org/sqlite` (the pure-Go, cgo-free driver) is isolated to this package. Primary consumer is `async/jobqueue/sqlite` (zero-infra single-node and dev/test); it is also the standard dev/test database story.

This mirrors `data/postgres`'s surface — `Open`/`Close`/`Healthcheck`, `Config`/`DefaultConfig`/`Validate`, `WithTx`/`WithTxRetry`, `Is…` classification predicates, a structural `Migrator` seam — adapted to SQLite's realities.

## Package shape & files

Standard forge package anatomy (`docs/design.md` §Anatomy):

- `doc.go` — package doc + runnable example.
- `config.go` — `Config`, `DefaultConfig`, `Validate`.
- `options.go` — `type Option func(*config)` (never builders): `WithConfig`, `WithLogger`, `WithMigrator`, `WithPragma`.
- `errors.go` — `errors.Is`-matchable single-line sentinels.
- `sqlite.go` — `Open`, the exported `DB` wrapper, pool wiring, `DB` routing/accessor methods.
- `dsn.go` — writer/reader DSN builders (pragma assembly, `file:` URI escaping, in-memory rewrite).
- `tx.go` — `WithTx`, `WithTxRetry`, `RetryOption`.
- `classify.go` — `IsUniqueViolation`, `IsForeignKeyViolation`, `IsBusy`, `IsNotFound`.
- `lifecycle.go` — `Close`, `Healthcheck`.
- `migrator.go` — the structural `Migrator` seam.
- `sqlite_test.go`, `dsn_test.go`, `tx_test.go`, `classify_test.go`, `config_test.go`, `options_test.go`, `lifecycle_test.go`, `bench_test.go` — black-box (`package sqlite_test`) except where asserting unexported state.

Target ~250–850 LOC of implementation (design.md single-responsibility rule).

## Concurrency model (the core decision)

SQLite permits one writer at a time; in WAL mode readers never block the writer and the writer never blocks readers. To get concurrent reads *and* write throughput without the app ever inflicting `SQLITE_BUSY` on itself, `Open` builds two `database/sql` pools over the same file:

### Writer pool
- `SetMaxOpenConns(1)` — exactly one writer connection ever. Writes serialize inside Go's `database/sql` pool (goroutines queue for the single conn) rather than racing at the SQLite lock, so the app never produces `SQLITE_BUSY` against itself.
- `SetMaxIdleConns(1)`, `SetConnMaxIdleTime(0)`, `SetConnMaxLifetime(0)` — the single writer conn is pinned warm and never idle-closed. This avoids re-running pragmas on reconnect and, critically for `:memory:`, prevents the in-memory database from being destroyed when the last conn closes.
- DSN carries `_txlock=immediate` — every `Begin`/`BeginTx` on the writer acquires the write lock upfront (`BEGIN IMMEDIATE` semantics via the driver), eliminating the deferred→write upgrade deadlock that a second transaction would otherwise hit.

### Reader pool
- `SetMaxOpenConns(ReadPoolSize)` (default `runtime.NumCPU()` when `Config.ReadPoolSize == 0`) — real read concurrency.
- Configurable `SetConnMaxIdleTime`/`SetConnMaxLifetime` from `Config`.
- DSN uses default `_txlock=deferred` and bakes `query_only=ON` — a write accidentally routed to the reader fails loudly rather than silently misrouting. The documented escape hatch for a legitimate write-that-reads (e.g. `… RETURNING` issued through `QueryContext`) is `db.Writer()`.

### Pragma application
Pragmas are applied per connection via `modernc.org/sqlite`'s `_pragma=` DSN parameters (no custom `sql.Register` driver needed); each `_pragma=` runs on every new connection so all pooled conns are identical.

- Writer DSN pragmas: `journal_mode(WAL)`, `busy_timeout(<ms>)`, `synchronous(<mode>)`, `foreign_keys(<0|1>)`, `cache_size(<n>)`, `mmap_size(<bytes>)`, `temp_store(MEMORY)`, then any `WithPragma` additions.
- Reader DSN pragmas: the same set **minus `journal_mode`** (a `query_only` connection cannot change the journal mode — it would error — and readers inherit the writer's persisted WAL mode), with `query_only(1)` applied **last** so the preceding connection-level pragmas (which don't modify the database) still apply. `foreign_keys` and `synchronous` are per-connection settings allowed under `query_only`.

Pragma ordering in the reader DSN matters: `query_only(1)` must be the final `_pragma=` entry. `WithPragma` additions on the reader are inserted before `query_only(1)`.

## Public API

```go
package sqlite

// DB wraps the writer and reader pools over one SQLite database.
type DB struct { /* unexported: writer, reader *sql.DB, logger *slog.Logger, dsn metadata */ }

// Open resolves options, validates, builds both pools, verifies liveness with a
// ping on each, runs the migrator (if any) on the writer, and returns the live *DB.
func Open(ctx context.Context, opts ...Option) (*DB, error)

// Native-handle accessors — the escape hatch for full control and sqlc wiring
// (e.g. queries.New(db.Reader()) / queries.New(db.Writer())).
func (db *DB) Writer() *sql.DB
func (db *DB) Reader() *sql.DB

// Convenience routing. Documented heuristic: Exec/Begin write, Query/QueryRow read.
// Leaky cases (SELECT inside a write-tx, "… RETURNING" via QueryContext) use the
// accessors instead. query_only on the reader turns a misrouted write into a loud error.
func (db *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)   // → writer
func (db *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)    // → reader
func (db *DB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row           // → reader
func (db *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)                 // → writer (IMMEDIATE)

// Lifecycle.
func Close(db *DB, log *slog.Logger)                    // closes both pools, one log line; nil-tolerant
func Healthcheck(db *DB) func(context.Context) error    // pings writer and reader; wraps ErrHealthcheck

// Transactions run on the writer.
func WithTx(ctx context.Context, db *DB, fn func(*sql.Tx) error) error
func WithTxRetry(ctx context.Context, db *DB, fn func(*sql.Tx) error, opts ...RetryOption) error
type RetryOption func(*retryConfig)
func WithRetryAttempts(n int) RetryOption   // clamp <1 to 1; default 3
func WithRetryInterval(d time.Duration) RetryOption   // ignore <=0; default 50ms

// Error classification over modernc *sqlite.Error extended result codes.
func IsUniqueViolation(err error) bool      // SQLITE_CONSTRAINT_UNIQUE 2067 / _PRIMARYKEY 1555
func IsForeignKeyViolation(err error) bool  // SQLITE_CONSTRAINT_FOREIGNKEY 787
func IsBusy(err error) bool                 // SQLITE_BUSY 5 / SQLITE_LOCKED 6 (+ extended variants)
func IsNotFound(err error) bool             // sql.ErrNoRows

// Options.
func WithConfig(cfg Config) Option
func WithLogger(l *slog.Logger) Option       // nil rejected → ErrInvalidConfig
func WithMigrator(m Migrator) Option         // nil rejected → ErrInvalidConfig
func WithPragma(name, value string) Option   // repeatable; appended to both DSNs (reader: before query_only)

// Sentinels (single-line, errors.Is-matchable).
var (
    ErrInvalidConfig = errors.New("sqlite: invalid config")
    ErrConnect       = errors.New("sqlite: connect failed")
    ErrHealthcheck   = errors.New("sqlite: healthcheck failed")
)
```

`Migrator` is the same structural one-method seam as `data/postgres`, so `sqlite` never imports `migration`:

```go
type Migrator interface {
    Up(ctx context.Context, db *sql.DB) error
}
```

`WithMigrator` runs `Up` against `db.Writer()` after both pools are live and pinged, before `Open` returns; a failed migration fails `Open` (both pools closed).

The convenience routing methods make `*DB` usable where a minimal `DBTX`-style interface is expected, while the accessors keep the correctness-critical routing decision in the caller's hands for the leaky cases.

## Config

Env prefix `SQLITE_`. Env struct tags are inert (no loader imported); `DefaultConfig` is the single source of truth. Field order is subject to betteralign.

| Field | Type | Env | Default | Notes |
|---|---|---|---|---|
| `Path` | `string` | `SQLITE_PATH` | `""` (required) | file path; `:memory:` / `mode=memory` special-cased |
| `JournalMode` | `string` | `SQLITE_JOURNAL_MODE` | `WAL` | writer only; validated against a known set |
| `Synchronous` | `string` | `SQLITE_SYNCHRONOUS` | `NORMAL` | safe + fast under WAL; validated |
| `BusyTimeout` | `time.Duration` | `SQLITE_BUSY_TIMEOUT` | `5s` | safety net vs external writers/checkpointer |
| `ForeignKeys` | `bool` | `SQLITE_FOREIGN_KEYS` | `true` | per-connection PRAGMA |
| `CacheSize` | `int` | `SQLITE_CACHE_SIZE` | `-16000` | negative = KiB (~16 MB) |
| `MmapSize` | `int64` | `SQLITE_MMAP_SIZE` | `268435456` | 256 MB memory-mapped reads; `0` disables |
| `ReadPoolSize` | `int` | `SQLITE_READ_POOL_SIZE` | `0` → `runtime.NumCPU()` | reader `MaxOpenConns` |
| `ConnMaxIdleTime` | `time.Duration` | `SQLITE_CONN_MAX_IDLE_TIME` | `0` (keep warm) | reader pool |
| `ConnMaxLifetime` | `time.Duration` | `SQLITE_CONN_MAX_LIFETIME` | `0` (unbounded) | reader pool |

`temp_store=MEMORY` is baked (not a field). The writer pool is always `MaxOpenConns(1)` and is **not** configurable — that invariant is what makes the model correct.

Deliberate deviation from `data/postgres`: **no connect-retry knobs** (`RetryAttempts`/`RetryInterval`). SQLite is a local file — it opens deterministically or fails; retrying a bad path only delays the error. `Open` still verifies liveness with a ping and wraps failure in `ErrConnect`.

`Validate` (called by the caller after env load, and defensively by `Open`) returns an `ErrInvalidConfig`-wrapped joined error when:
- `Path == ""`.
- `ReadPoolSize < 0`.
- `BusyTimeout`/`ConnMaxIdleTime`/`ConnMaxLifetime` `< 0`.
- `JournalMode` not in {`WAL`,`DELETE`,`TRUNCATE`,`PERSIST`,`MEMORY`,`OFF`} (case-insensitive).
- `Synchronous` not in {`OFF`,`NORMAL`,`FULL`,`EXTRA`} (case-insensitive).

`MmapSize < 0` is rejected; `MmapSize == 0` is valid (disables mmap).

## In-memory handling

When `Path` is `:memory:` (or already contains `mode=memory`), the DSN builder rewrites it to a unique, isolated, shared-cache in-memory DSN of the form `file:<unique-name>?mode=memory&cache=shared`, where `<unique-name>` is generated per `Open` from a package-level `sync/atomic` counter (e.g. `memdb-<n>`) — process-unique with no external or forge-internal dependency. Shared-cache in-memory databases are scoped per process, so a monotonic counter is sufficient for uniqueness. Rationale:

- `cache=shared` makes all N reader connections and the writer connection see the **same** in-memory database (plain `:memory:` gives each connection its own private DB — the classic footgun that breaks a multi-conn pool).
- The unique name keeps every `Open` isolated from every other in the same process.
- WAL is **not** applied to memory DBs (unsupported); journal mode is effectively `MEMORY`. The reader/writer perf split still functions but under shared-cache locking rather than WAL — acceptable because memory mode is the dev/test path (correctness over max-perf).
- The pinned writer conn (idle/lifetime 0, `MaxIdleConns(1)`) keeps the memory DB alive for the process's use of the `*DB`.

## DSN builder

`dsn.go` assembles writer and reader DSN strings from the resolved `Config`:

- File paths are turned into a `file:` URI with correct percent-encoding so paths containing spaces, `?`, `#`, or other reserved characters are handled safely (never string-concatenated into the query). Relative and absolute paths both supported.
- Pragmas are emitted as ordered `_pragma=` params per the writer/reader rules above; `_txlock` set to `immediate` (writer) / `deferred` (reader).
- In-memory rewrite as described.
- This is the primary fuzz target (see Testing).

## Transactions

- `WithTx(ctx, db, fn)` begins on the writer (IMMEDIATE via `_txlock`), runs `fn`, commits on success, rolls back on error, and rolls back + re-raises on panic (the postgres `WithTx` control flow, over `*sql.Tx`). The rollback's own error is ignored once `fn` has failed.
- `WithTxRetry(ctx, db, fn, opts…)` wraps `WithTx` with an attempt budget, retrying only when the error is a busy/locked condition (`IsBusy`), backing off `interval · 2^attempt` capped at a package `maxRetryBackoff` (~30s), honoring `ctx.Done()` between attempts. Non-busy errors return immediately; a panic propagates without retry. Defaults: 3 attempts, 50 ms base interval (mirrors postgres `defaultRetryConfig`).

Because the writer is single-conn with IMMEDIATE locking, self-inflicted busy is already largely designed out; `WithTxRetry` covers contention from external writers (another process, the Litestream/backup checkpointer) hitting the `busy_timeout` ceiling.

## Error classification

`classify.go` extracts the extended result code from a `*sqlite.Error` (via `errors.As`/`errors.AsType`) and matches without the call site importing the driver:

- `IsUniqueViolation` — `2067` (`SQLITE_CONSTRAINT_UNIQUE`) or `1555` (`SQLITE_CONSTRAINT_PRIMARYKEY`).
- `IsForeignKeyViolation` — `787` (`SQLITE_CONSTRAINT_FOREIGNKEY`).
- `IsBusy` — `5`/`6` primary codes plus extended busy/locked variants (`SQLITE_BUSY_*`, `SQLITE_LOCKED_*`).
- `IsNotFound` — `errors.Is(err, sql.ErrNoRows)`.

Exact modernc code constants/API are confirmed against `modernc.org/sqlite` during implementation; the predicate contract above is the spec.

## Lifecycle

- `Close(db, log)` — nil-tolerant; logs one line (skipped when `log == nil`) and closes both pools (writer and reader). No ctx (close is synchronous). Intended as `defer Close(db, logger)` in `main` after the supervisor drains.
- `Healthcheck(db)` — returns `func(ctx) error` that pings **both** pools (writer and reader must be live for readiness), wrapping any failure in `ErrHealthcheck`. Hand to `/readyz`.

## Migrations (M1 — change to data/migration)

`data/migration` is currently hard-pinned to `goose.DialectPostgres` (`migration.go:47`). Add dialect selection:

- New exported `Dialect` type with values `Postgres` (default — preserves current behavior of `New(fsys)`) and `SQLite`.
- New option `WithDialect(d Dialect) Option` in `migration/options.go`; the resolved dialect maps to `goose.DialectPostgres` / `goose.DialectSQLite3` in `NewProvider`.
- Update the `migration` `doc.go` line "The dialect is fixed to PostgreSQL" to describe the option (default Postgres).
- `migration` already depends on goose (isolated); `goose.DialectSQLite3` works over any `database/sql` SQLite driver, so it runs against the modernc-backed `*sql.DB` from `db.Writer()`.

Consumer wiring:

```go
db, err := sqlite.Open(ctx,
    sqlite.WithConfig(cfg),
    sqlite.WithMigrator(migration.New(migrationsFS, migration.WithDialect(migration.SQLite))),
)
```

`sqlite` itself takes no dependency on `migration` (structural seam), identical to postgres. The migration change is a separate, small, backward-compatible edit shipped in the same PR.

## Testing & benchmarks

SQLite is embedded, so tests run **live everywhere including CI** with real temp-file DBs (`t.TempDir()`) and `:memory:` — no skip-without-DSN gating (a notable ergonomic win over the postgres suite).

Required coverage:
- Pragmas actually applied: query `PRAGMA journal_mode` (= `wal` for file DBs), `foreign_keys`, `busy_timeout`, `synchronous` on both writer and reader conns.
- Concurrency: many reader goroutines running `SELECT`s concurrently with a sustained writer loop; assert zero `SQLITE_BUSY` surfaced to the app and correct results.
- Reader safety: a write issued through the reader (`query_only`) returns an error.
- Writer serialization: concurrent `WithTx` writers all commit without app-level busy errors.
- In-memory isolation: two separate `Open(:memory:)` instances do not see each other's tables; within one instance, a table created via the writer is visible to the reader pool.
- Classification: trigger real unique, FK, and busy conditions and assert the predicates; `IsNotFound` on an empty `QueryRow`.
- Tx: `WithTx` commit/rollback/panic paths; `WithTxRetry` retries on a simulated/contended busy and gives up after the budget honoring ctx cancellation.
- Migrations: run a real goose SQLite migration set through `sqlite.WithMigrator(migration.New(fs, migration.WithDialect(migration.SQLite)))`; assert schema applied on the writer and visible to readers.
- Config/options: `Validate` rejects each invalid field; nil `WithLogger`/`WithMigrator` rejected; `WithPragma` overrides land in the DSN.
- Lifecycle: `Close` nil-tolerance; `Healthcheck` ok and failure (closed DB) paths.
- DSN builder fuzz (`dsn_test.go`): paths with spaces/`?`/`#`/unicode round-trip to a valid, correctly-escaped `file:` URI; in-memory rewrite always yields a unique shared-cache name.

Benchmarks (`bench_test.go`, required per repo policy): single-row insert (writer), point `SELECT` (reader), concurrent read+write throughput, `WithTx`. Follow with the mandated post-benchmark optimization pass (measured wins only) and put before/after numbers in the PR. The wrapper's routing methods must add negligible allocation over calling `*sql.DB` directly.

## Anti-scope

- No query builder / ORM / schema DSL — the consumer owns SQL (sqlc/hand-written), same as postgres.
- No connect-retry loop (local file).
- No backup / replication / Litestream orchestration — an ops/consumer concern (`busy_timeout` cooperates with an external checkpointer).
- No encryption (SQLCipher) — modernc is cgo-free with no crypto VFS; out of scope forever here.
- Not the `mattn/go-sqlite3` cgo driver — cgo-free `modernc.org/sqlite` only, isolated to this package.
- No auto-routing beyond the documented Exec/Query heuristic; the accessors are the escape hatch.
- No `WithScope` multi-tenant seam — like `data/postgres`, this is a stateless connection factory with no per-request state, so it is exempt from the framework's construction-time tenancy-seam rule; tenancy is a query concern handled via `data/tenant`.

## Dependencies

- `sqlite`: none forge-internal. External: `modernc.org/sqlite` (cgo-free driver, isolated to this package; added to `go.mod`). Its transitive `modernc.org/libc|mathutil|memory` are already present via goose.
- `data/migration`: gains the `WithDialect` option (goose already a dep); no new external module.

On merge: delete the `data/sqlite` entry from `docs/packages.md` (roadmap lists only unbuilt packages).

## Build order

1. `data/migration` `WithDialect` addition (small, backward-compatible; unblocks SQLite migrations).
2. `data/sqlite` core: `Config`/`Validate`, DSN builder (+ in-memory rewrite), `Open`/`DB`/pools, `Close`/`Healthcheck`.
3. `tx.go`, `classify.go`.
4. `WithMigrator` wiring + integration test against `migration` SQLite dialect.
5. `doc.go` runnable example; tests; benchmarks + optimization pass.
