# Database Connectivity Packages Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add five flat top-level packages — `postgres`, `migration`, `redis`, `mongo`, `opensearch` — that cut the per-app boilerplate of forge's data backends: connect-with-retry + `Config`/`Validate` + health/close lifecycle + boot-time schema setup + transactions + error classification, each returning the native driver client.

**Architecture:** Each connectivity package applies one copied convention (no shared base): an exported `Config` with inert `env:"…"` tags + `DefaultConfig()` (single source of truth) + `Validate()`; `Open(ctx, opts...)` that seeds `DefaultConfig`, applies functional options (`WithConfig` + `WithLogger` + a native-config escape hatch), validates, then connects with bounded exponential-backoff retry and a liveness ping; `Close(client, logger)` and `Healthcheck(client) func(ctx) error` lifecycle helpers. No package imports `supervisor` — pools are `main`-owned resources closed via `defer`. `migration` is a standalone goose runner over the stdlib `*sql.DB`; it meets `postgres` only at the one-method `postgres.Migrator` interface (`postgres` imports the pgx→sql bridge but not goose; `migration` imports goose but not pgx).

**Tech Stack:** Go 1.26 · `github.com/jackc/pgx/v5` (pgxpool, stdlib, pgconn) · `go.mongodb.org/mongo-driver/v2` · `github.com/redis/go-redis/v9` · `github.com/opensearch-project/opensearch-go/v4` · `github.com/pressly/goose/v3` · `github.com/stretchr/testify` (tests only).

This plan was derived from the design spec [`docs/superpowers/specs/2026-06-27-database-packages-design.md`](../specs/2026-06-27-database-packages-design.md).

## Global Constraints

Every task's requirements implicitly include this section. Values are copied verbatim from the spec and must not drift.

- **Module / Go version:** module `github.com/dmitrymomot/forge`; Go `1.26`. Each package lives at the repo **root** in its own flat directory (`postgres/`, `migration/`, `redis/`, `mongo/`, `opensearch/`); the package name equals the directory name. Do not create nested folders.
- **Pinned dependency versions (use exactly these in `go get`):** `github.com/jackc/pgx/v5@v5.10.0` · `go.mongodb.org/mongo-driver/v2@v2.7.0` · `github.com/redis/go-redis/v9@v9.21.0` · `github.com/opensearch-project/opensearch-go/v4@v4.6.0` · `github.com/pressly/goose/v3@v3.27.1`. Each driver dependency is confined to its single package (the package *is* the isolation boundary). `testify` is the only test dependency; no `testcontainers`.
- **Driver self-name clash:** the forge packages `redis`, `mongo`, and `opensearch` share their names with the driver packages, so **inside each package the driver is imported under an alias** (`goredis`, `mongodriver`, `osgo`/`osapi`) and **black-box tests import the forge package under an alias** (`forgeredis`, `forgemongo`, `forgeos`). `postgres` returns `*pgxpool.Pool` (package `pgxpool`, no clash), so it imports the forge package plainly.
- **Config convention:** exported `Config` struct with **inert `env:"NAME"` tags and NO `envDefault`** (defaults live solely in `DefaultConfig()`); `DefaultConfig() Config` is the single source of truth; `(Config) Validate() error` returns an `ErrInvalidConfig`-wrapped, single-line `errors.Join`. Required fields (URL/URI/Addresses) have no default, so `DefaultConfig()` alone fails `Validate`.
- **Options convention:** `type Option func(*config)` over an unexported `config` struct that embeds `Config` plus code values and `errs []error`. `Open` seeds `DefaultConfig()`, applies options in order, returns the joined `errs` if any, then `Validate()`s. `WithConfig(cfg)` sets the whole block. Nil func/pointer option arguments are **rejected** (append an `ErrInvalidConfig`-wrapped error to `errs`, surfaced by `Open`) — except `WithLogger`, where a nil logger is *allowed* in packages whose spec says so (redis/opensearch install a discard handler; postgres/mongo reject nil — follow each package's task). The native-config escape hatch runs **LAST** in `Open`, after the `Config` overlay.
- **Connect-retry:** a small in-package loop, no dependency on any `backoff` package: try connect+ping up to `RetryAttempts`; on failure wait `RetryInterval · 2^attempt` capped at ~30s, honoring `ctx` cancellation between attempts; `RetryAttempts <= 1` means a single attempt with no wait; on exhaustion return `ErrConnect` wrapped/joined with the last driver error; close any partially-opened client before returning.
- **Errors:** per-package sentinels (`ErrInvalidConfig`, `ErrConnect`, `ErrHealthcheck`, plus `migration.ErrMigrate`, `opensearch.ErrSetup`) declared in `errors.go`, single-line, `errors.Is`-matchable, no embedded stacks/blobs. The `Is…` classification predicates match the *driver's* errors (`errors.As`/`errors.Is`), not these sentinels.
- **Testing:** **black-box only** — test files use `package <pkg>_test`. Pure unit tests (config/validate/option-wiring/error-classification/topology-selection/retry-against-unreachable-address/fs-parsing) **always run** under `just test`. Integration tests that need a real server are **env-gated** and `t.Skip` when the variable is unset: `FORGE_TEST_POSTGRES_DSN`, `FORGE_TEST_REDIS_URL`, `FORGE_TEST_MONGO_URI` (+ `FORGE_TEST_MONGO_RS_URI` for transactions, `FORGE_TEST_MONGO_SHARDED_URI` for sharding), `FORGE_TEST_OPENSEARCH_ADDR`. No Docker is required for a green local `just test`.
- **Commands:** run a single test with `go test -race ./<pkg>/ -run TestName -v`; run a package with `just test ./<pkg>/...` (= `go clean -testcache && go test -race -cover`); lint with `just lint`; format with `just fmt`. `just lint` includes `betteralign` — if it reports struct field-order changes, run `just fmt` to apply them (field order does not affect behavior or the env-tag tests, which look up fields by name).
- **Commits:** conventional, scoped per package (`feat(postgres): …`, `docs(redis): …`), one commit at the end of each task. Work only on the current branch.

---
## Package: `postgres`

### Task PG-1: Config, DefaultConfig, Validate, errors

**Files:**
- Create: `postgres/config.go`
- Create: `postgres/errors.go`
- Test: `postgres/config_test.go`

**Interfaces:**
- Produces: `postgres.Config` struct; `postgres.DefaultConfig() Config`; `(Config) Validate() error`; sentinels `ErrInvalidConfig`, `ErrConnect`, `ErrHealthcheck`.

- [ ] **Step 1: Add the pgx dependency** (needed from this task onward; the test only touches `Config`, but later tasks in this package import pgxpool/pgconn/stdlib and we pin once)
Run:
```bash
go get github.com/jackc/pgx/v5@v5.10.0
```
Expected: `go.mod`/`go.sum` updated with `github.com/jackc/pgx/v5 v5.10.0` (brings `pgxpool`, `stdlib`, `pgconn` as subpackages of the same module).

- [ ] **Step 2: Write the failing test**
```go
package postgres_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/postgres"
)

func TestDefaultConfig(t *testing.T) {
	cfg := postgres.DefaultConfig()
	assert.Equal(t, int32(10), cfg.MaxConns)
	assert.Equal(t, int32(2), cfg.MinConns)
	assert.Equal(t, 30*time.Minute, cfg.MaxConnLifetime)
	assert.Equal(t, 10*time.Minute, cfg.MaxConnIdleTime)
	assert.Equal(t, time.Minute, cfg.HealthCheckPeriod)
	assert.Equal(t, 5*time.Second, cfg.ConnectTimeout)
	assert.Equal(t, 3, cfg.RetryAttempts)
	assert.Equal(t, time.Second, cfg.RetryInterval)
	assert.Empty(t, cfg.URL, "URL defaults empty and is required at Open time")

	// DefaultConfig alone fails Validate because URL is required.
	require.ErrorIs(t, cfg.Validate(), postgres.ErrInvalidConfig)

	// A default config with a URL filled in is valid.
	cfg.URL = "postgres://u:p@localhost:5432/db"
	require.NoError(t, cfg.Validate())
}

func TestConfig_Validate(t *testing.T) {
	const url = "postgres://u:p@localhost:5432/db"
	tests := map[string]postgres.Config{
		"empty url":         {URL: ""},
		"neg max conns":     {URL: url, MaxConns: -1},
		"neg min conns":     {URL: url, MinConns: -1},
		"min gt max":        {URL: url, MinConns: 5, MaxConns: 2},
		"neg conn lifetime": {URL: url, MaxConnLifetime: -1},
		"neg conn idle":     {URL: url, MaxConnIdleTime: -1},
		"neg health period": {URL: url, HealthCheckPeriod: -1},
		"neg connect to":    {URL: url, ConnectTimeout: -1},
		"neg retry attempt": {URL: url, RetryAttempts: -1},
		"neg retry interval": {URL: url, RetryInterval: -1},
	}
	for name, cfg := range tests {
		t.Run(name, func(t *testing.T) {
			err := cfg.Validate()
			require.Error(t, err)
			assert.ErrorIs(t, err, postgres.ErrInvalidConfig)
		})
	}

	// Zero MaxConns/MinConns are allowed (pgxpool fills its own defaults).
	require.NoError(t, postgres.Config{URL: url}.Validate())
}

func TestConfig_EnvTags(t *testing.T) {
	want := map[string]string{
		"URL":               "URL",
		"MinConns":          "MIN_CONNS",
		"MaxConns":          "MAX_CONNS",
		"MaxConnLifetime":   "MAX_CONN_LIFETIME",
		"MaxConnIdleTime":   "MAX_CONN_IDLE_TIME",
		"HealthCheckPeriod": "HEALTH_CHECK_PERIOD",
		"ConnectTimeout":    "CONNECT_TIMEOUT",
		"RetryAttempts":     "RETRY_ATTEMPTS",
		"RetryInterval":     "RETRY_INTERVAL",
	}
	typ := reflect.TypeFor[postgres.Config]()
	for fname, tag := range want {
		f, ok := typ.FieldByName(fname)
		require.Truef(t, ok, "field %s missing", fname)
		assert.Equalf(t, tag, f.Tag.Get("env"), "field %s env tag", fname)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**
Run: `go test -race ./postgres/ -run TestDefaultConfig -v`
Expected: FAIL (`undefined: postgres.DefaultConfig`, `undefined: postgres.Config`, `undefined: postgres.ErrInvalidConfig`).

- [ ] **Step 4: Write `postgres/errors.go`**
```go
package postgres

import "errors"

// Sentinel errors returned (often wrapped) by this package. Match with errors.Is.
//
// These are distinct from the Is… classification predicates (IsUniqueViolation and
// friends), which match the underlying *pgconn.PgError rather than these sentinels.
var (
	// ErrInvalidConfig is returned (joined) by Validate and Open when an option or
	// Config field has an invalid value.
	ErrInvalidConfig = errors.New("postgres: invalid config")
	// ErrConnect is returned by Open when the pool could not be built or the server
	// could not be reached within the configured retry budget.
	ErrConnect = errors.New("postgres: connect failed")
	// ErrHealthcheck wraps a failed liveness ping from the Healthcheck closure.
	ErrHealthcheck = errors.New("postgres: healthcheck failed")
)
```

- [ ] **Step 5: Write `postgres/config.go`**
```go
package postgres

import (
	"errors"
	"fmt"
	"time"
)

// Config holds the serializable settings for a connection pool. The env struct
// tags are inert strings — this package imports no config loader. Populate Config
// with any loader that reads env struct tags, typically by seeding from
// DefaultConfig and parsing the environment over it. Field order is subject to the
// repo's betteralign tooling.
type Config struct {
	URL               string        `env:"URL"`                 // postgres://… connection string (required)
	MaxConnLifetime   time.Duration `env:"MAX_CONN_LIFETIME"`   // close a conn this long after creation
	MaxConnIdleTime   time.Duration `env:"MAX_CONN_IDLE_TIME"`  // close an idle conn after this long
	HealthCheckPeriod time.Duration `env:"HEALTH_CHECK_PERIOD"` // pgxpool's own idle-conn check interval
	ConnectTimeout    time.Duration `env:"CONNECT_TIMEOUT"`     // per-attempt dial+handshake bound
	RetryInterval     time.Duration `env:"RETRY_INTERVAL"`      // base backoff between connect attempts
	MinConns          int32         `env:"MIN_CONNS"`           // pool floor
	MaxConns          int32         `env:"MAX_CONNS"`           // pool ceiling
	RetryAttempts     int           `env:"RETRY_ATTEMPTS"`      // total connect attempts; <=1 means one, no wait
}

// DefaultConfig returns production-sane pool defaults and is the single source of
// truth for them (there are no envDefault tags to drift from it). URL is left empty
// and must be supplied; DefaultConfig alone therefore fails Validate.
func DefaultConfig() Config {
	return Config{
		MaxConns:          10,
		MinConns:          2,
		MaxConnLifetime:   30 * time.Minute,
		MaxConnIdleTime:   10 * time.Minute,
		HealthCheckPeriod: time.Minute,
		ConnectTimeout:    5 * time.Second,
		RetryAttempts:     3,
		RetryInterval:     time.Second,
	}
}

// Validate reports whether the field values are usable, returning an
// ErrInvalidConfig-wrapped, single-line joined error otherwise. Callers may call it
// after loading from env (zero-trust); Open also calls it defensively.
func (c Config) Validate() error {
	var errs []error
	if c.URL == "" {
		errs = append(errs, fmt.Errorf("%w: URL must not be empty", ErrInvalidConfig))
	}
	if c.MaxConns < 0 {
		errs = append(errs, fmt.Errorf("%w: MaxConns must be >= 0", ErrInvalidConfig))
	}
	if c.MinConns < 0 {
		errs = append(errs, fmt.Errorf("%w: MinConns must be >= 0", ErrInvalidConfig))
	}
	if c.MaxConns > 0 && c.MinConns > c.MaxConns {
		errs = append(errs, fmt.Errorf("%w: MinConns must be <= MaxConns", ErrInvalidConfig))
	}
	for _, f := range []struct {
		name string
		d    time.Duration
	}{
		{"MaxConnLifetime", c.MaxConnLifetime},
		{"MaxConnIdleTime", c.MaxConnIdleTime},
		{"HealthCheckPeriod", c.HealthCheckPeriod},
		{"ConnectTimeout", c.ConnectTimeout},
		{"RetryInterval", c.RetryInterval},
	} {
		if f.d < 0 {
			errs = append(errs, fmt.Errorf("%w: %s must be >= 0", ErrInvalidConfig, f.name))
		}
	}
	if c.RetryAttempts < 0 {
		errs = append(errs, fmt.Errorf("%w: RetryAttempts must be >= 0", ErrInvalidConfig))
	}
	return errors.Join(errs...)
}
```

- [ ] **Step 6: Run tests to verify they pass**
Run: `just test ./postgres/...`
Expected: PASS

- [ ] **Step 7: Commit**
```bash
git add postgres/ go.mod go.sum && git commit -m "feat(postgres): add Config, DefaultConfig, Validate, sentinel errors"
```

---

### Task PG-2: options + Open with connect-retry

**Files:**
- Create: `postgres/options.go`
- Create: `postgres/postgres.go`
- Test: `postgres/options_test.go`
- Test: `postgres/open_test.go`

**Interfaces:**
- Produces: `postgres.Option`; `WithConfig(Config) Option`; `WithLogger(*slog.Logger) Option`; `WithPoolConfig(func(*pgxpool.Config)) Option`; `Open(ctx, ...Option) (*pgxpool.Pool, error)`.
- Internal: lowercase `config` struct embedding `Config` plus `logger *slog.Logger`, `poolConfig func(*pgxpool.Config)`, `errs []error`. (The `migrator` field and the `Migrator` type are added together in PG-6 — do NOT reference `Migrator` here, it does not exist yet.)

- [ ] **Step 1: Write the failing tests**

`postgres/options_test.go`:
```go
package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/postgres"
)

func TestOpen_NilOptionRejected(t *testing.T) {
	opts := map[string]postgres.Option{
		"logger":     postgres.WithLogger(nil),
		"poolconfig": postgres.WithPoolConfig(nil),
	}
	for name, opt := range opts {
		t.Run(name, func(t *testing.T) {
			// A valid URL is supplied so the only failure is the option's rejection.
			cfg := postgres.DefaultConfig()
			cfg.URL = "postgres://u:p@127.0.0.1:1/db"
			pool, err := postgres.Open(context.Background(),
				postgres.WithConfig(cfg),
				opt,
			)
			require.Error(t, err)
			assert.ErrorIs(t, err, postgres.ErrInvalidConfig)
			assert.Nil(t, pool)
		})
	}
}

func TestOpen_MissingURL(t *testing.T) {
	// No WithConfig => pure DefaultConfig => empty URL => Validate fails.
	pool, err := postgres.Open(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, postgres.ErrInvalidConfig)
	assert.Nil(t, pool)
}

func TestOpen_BadURL(t *testing.T) {
	// Non-empty but unparseable URL passes Validate, then fails ParseConfig.
	cfg := postgres.DefaultConfig()
	cfg.URL = "://not a url"
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.Error(t, err)
	assert.ErrorIs(t, err, postgres.ErrConnect)
	assert.Nil(t, pool)
}

func TestWithPoolConfig_RunsLast(t *testing.T) {
	// The escape hatch runs after the Config overlay, so it sees Config's MaxConns
	// and can override it. We assert it is invoked and observes the overlaid value.
	cfg := postgres.DefaultConfig()
	cfg.URL = "postgres://u:p@127.0.0.1:1/db"
	cfg.MaxConns = 7
	cfg.RetryAttempts = 1
	cfg.RetryInterval = time.Millisecond
	cfg.ConnectTimeout = 100 * time.Millisecond

	var sawMaxConns int32
	called := false
	// Open will ultimately fail to connect (unreachable addr), but the hatch runs
	// before the connect attempt, so we can observe it ran with the overlay applied.
	_, _ = postgres.Open(context.Background(),
		postgres.WithConfig(cfg),
		postgres.WithPoolConfig(func(pc *pgxpool.Config) {
			called = true
			sawMaxConns = pc.MaxConns
		}),
	)
	require.True(t, called, "WithPoolConfig fn must run inside Open")
	assert.Equal(t, int32(7), sawMaxConns, "hatch runs after the Config overlay")
}
```

`postgres/open_test.go`:
```go
package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/postgres"
)

func TestOpen_RetryExhausted(t *testing.T) {
	// Port 1 is unreachable; with 2 attempts and tiny waits Open must give up fast
	// and return ErrConnect joined with the last driver error.
	cfg := postgres.DefaultConfig()
	cfg.URL = "postgres://u:p@127.0.0.1:1/db"
	cfg.RetryAttempts = 2
	cfg.RetryInterval = time.Millisecond
	cfg.ConnectTimeout = 100 * time.Millisecond

	start := time.Now()
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.ErrorIs(t, err, postgres.ErrConnect)
	assert.Nil(t, pool)
	assert.Less(t, elapsed, 5*time.Second, "two short attempts must not block long")
}

func TestOpen_ContextCancelled(t *testing.T) {
	cfg := postgres.DefaultConfig()
	cfg.URL = "postgres://u:p@127.0.0.1:1/db"
	cfg.RetryAttempts = 100
	cfg.RetryInterval = time.Second
	cfg.ConnectTimeout = 100 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	start := time.Now()
	pool, err := postgres.Open(ctx, postgres.WithConfig(cfg))
	require.Error(t, err)
	assert.ErrorIs(t, err, postgres.ErrConnect)
	assert.Nil(t, pool)
	assert.Less(t, time.Since(start), 2*time.Second, "a cancelled ctx must short-circuit the retry loop")
}
```

- [ ] **Step 2: Run tests to verify they fail**
Run: `go test -race ./postgres/ -run TestOpen -v`
Expected: FAIL (`undefined: postgres.Open`, `undefined: postgres.WithLogger`).

- [ ] **Step 3: Write `postgres/options.go`**
```go
package postgres

import (
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// config holds resolved settings for a single Open. The embedded Config carries
// serializable data; the remaining fields are non-serializable code values.
// (PG-6 adds a `migrator Migrator` field here alongside the Migrator type.)
type config struct {
	logger     *slog.Logger
	poolConfig func(*pgxpool.Config)
	errs       []error
	Config
}

// Option configures Open. Invalid values accumulate and are returned by Open.
type Option func(*config)

// WithConfig sets the whole serializable data block at once. Build the argument
// from DefaultConfig() (or an env-parsed copy); a bare Config{} zeroes every limit
// and leaves URL empty (which fails Validate). Options apply in order — place
// WithConfig before any code option you want layered on top of it.
func WithConfig(cfg Config) Option {
	return func(c *config) { c.Config = cfg }
}

// WithLogger sets the slog.Logger used by Close and lifecycle logging. Default
// slog.Default(); a nil logger is rejected (ErrInvalidConfig).
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithLogger received a nil *slog.Logger", ErrInvalidConfig))
			return
		}
		c.logger = l
	}
}

// WithPoolConfig registers an escape hatch invoked inside Open, last, after the
// Config fields have been overlaid onto the parsed *pgxpool.Config. Use it for
// anything Config does not cover: query tracers, AfterConnect hooks, a custom TLS
// config. A nil func is rejected (ErrInvalidConfig).
func WithPoolConfig(fn func(*pgxpool.Config)) Option {
	return func(c *config) {
		if fn == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithPoolConfig received a nil func", ErrInvalidConfig))
			return
		}
		c.poolConfig = fn
	}
}
```

- [ ] **Step 4: Write `postgres/postgres.go`** (Open + the connect-retry loop; the migrator branch is added in PG-6)
```go
package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// maxRetryBackoff caps the exponential wait between connect attempts so a large
// RetryInterval or attempt count cannot push a single wait past ~30s.
const maxRetryBackoff = 30 * time.Second

// Open builds a connection pool from the resolved options, connects with bounded
// retry/backoff, and verifies liveness with a ping before returning the live
// *pgxpool.Pool. The caller owns the pool and should defer Close(pool, logger).
//
// Flow: start from DefaultConfig, apply options, surface any option errors,
// Validate, parse the URL, overlay the Config limits/timeouts onto the parsed
// *pgxpool.Config, run WithPoolConfig last, then connect-with-retry + ping. Any
// failure closes the partial pool and returns a sentinel-wrapped, single-line error.
func Open(ctx context.Context, opts ...Option) (*pgxpool.Pool, error) {
	cfg := config{Config: DefaultConfig()}
	for _, opt := range opts {
		opt(&cfg)
	}
	if len(cfg.errs) > 0 {
		return nil, errors.Join(cfg.errs...)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	logger := cfg.logger
	if logger == nil {
		logger = slog.Default()
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("%w: parse url: %v", ErrConnect, err)
	}

	// Overlay the serializable Config onto the parsed pool config.
	if cfg.MaxConns > 0 {
		poolCfg.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		poolCfg.MinConns = cfg.MinConns
	}
	if cfg.MaxConnLifetime > 0 {
		poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	}
	if cfg.MaxConnIdleTime > 0 {
		poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	}
	if cfg.HealthCheckPeriod > 0 {
		poolCfg.HealthCheckPeriod = cfg.HealthCheckPeriod
	}
	if cfg.ConnectTimeout > 0 {
		poolCfg.ConnConfig.ConnectTimeout = cfg.ConnectTimeout
	}

	// Escape hatch runs last, after the Config overlay.
	if cfg.poolConfig != nil {
		cfg.poolConfig(poolCfg)
	}

	pool, err := connectWithRetry(ctx, poolCfg, cfg.RetryAttempts, cfg.RetryInterval, logger)
	if err != nil {
		return nil, err
	}

	// Migrator wiring is added in PG-6.

	return pool, nil
}

// connectWithRetry builds the pool and pings it, retrying on failure with
// exponential backoff (RetryInterval · 2^attempt, capped at maxRetryBackoff) and
// honoring ctx cancellation between attempts. attempts <= 1 means a single attempt
// with no wait. On exhaustion it returns ErrConnect joined with the last error.
func connectWithRetry(ctx context.Context, poolCfg *pgxpool.Config, attempts int, interval time.Duration, logger *slog.Logger) (*pgxpool.Pool, error) {
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := range attempts {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrConnect, err)
		}

		pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
		if err == nil {
			if err = pool.Ping(ctx); err == nil {
				return pool, nil
			}
			pool.Close() // ping failed: drop the partial pool before retrying
		}
		lastErr = err

		if attempt == attempts-1 {
			break // do not wait after the final attempt
		}

		wait := backoff(interval, attempt)
		logger.Warn("postgres connect attempt failed; retrying",
			slog.Int("attempt", attempt+1),
			slog.Int("attempts", attempts),
			slog.Duration("wait", wait),
			slog.String("err", err.Error()),
		)
		select {
		case <-ctx.Done():
			return nil, errors.Join(ErrConnect, ctx.Err())
		case <-time.After(wait):
		}
	}

	return nil, errors.Join(ErrConnect, lastErr)
}

// backoff returns interval · 2^attempt, capped at maxRetryBackoff.
func backoff(interval time.Duration, attempt int) time.Duration {
	wait := interval << attempt // interval * 2^attempt
	if wait <= 0 || wait > maxRetryBackoff {
		return maxRetryBackoff
	}
	return wait
}
```

- [ ] **Step 5: Run tests to verify they pass**
Run: `just test ./postgres/...`
Expected: PASS

- [ ] **Step 6: Commit**
```bash
git add postgres/ && git commit -m "feat(postgres): add options and Open with bounded connect-retry"
```

---

### Task PG-3: Close + Healthcheck

**Files:**
- Create: `postgres/lifecycle.go`
- Test: `postgres/lifecycle_test.go`

**Interfaces:**
- Produces: `Close(pool *pgxpool.Pool, log *slog.Logger)`; `Healthcheck(pool *pgxpool.Pool) func(context.Context) error`.

- [ ] **Step 1: Write the failing test**
```go
package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/postgres"
)

func TestClose_NilLoggerTolerated(t *testing.T) {
	// Close must not panic on a nil pool or a nil logger (defensive in main's defer).
	assert.NotPanics(t, func() { postgres.Close(nil, nil) })
}

func TestHealthcheck_Integration(t *testing.T) {
	dsn := os.Getenv("FORGE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set FORGE_TEST_POSTGRES_DSN")
	}
	cfg := postgres.DefaultConfig()
	cfg.URL = dsn
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(t, err)
	defer postgres.Close(pool, nil)

	check := postgres.Healthcheck(pool)
	require.NotNil(t, check)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, check(ctx), "a live pool must report healthy")

	// After Close, the same closure must report an ErrHealthcheck-wrapped failure.
	postgres.Close(pool, nil)
	assert.ErrorIs(t, check(ctx), postgres.ErrHealthcheck)
}
```

- [ ] **Step 2: Run test to verify it fails**
Run: `go test -race ./postgres/ -run TestClose -v`
Expected: FAIL (`undefined: postgres.Close`).

- [ ] **Step 3: Write the implementation**
```go
package postgres

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Close logs a single line and closes the pool. It is the resource counterpart to
// Open, meant as `defer Close(pool, logger)` in main so it runs after the
// supervisor has drained every service. A nil pool or nil logger is tolerated: the
// close still happens (when the pool is non-nil); the log line is skipped when the
// logger is nil. It takes no ctx because pgxpool.Close is synchronous.
func Close(pool *pgxpool.Pool, log *slog.Logger) {
	if pool == nil {
		return
	}
	if log != nil {
		log.Info("closing postgres pool")
	}
	pool.Close()
}

// Healthcheck returns a stateless closure that pings the pool, wrapping any failure
// in ErrHealthcheck. Its func(context.Context) error shape is exactly what a
// readiness/liveness probe wants; hand it to the app's /readyz handler.
func Healthcheck(pool *pgxpool.Pool) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := pool.Ping(ctx); err != nil {
			return fmt.Errorf("%w: %v", ErrHealthcheck, err)
		}
		return nil
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**
Run: `just test ./postgres/...`
Expected: PASS (the integration test skips without `FORGE_TEST_POSTGRES_DSN`).

- [ ] **Step 5: Commit**
```bash
git add postgres/ && git commit -m "feat(postgres): add Close and Healthcheck lifecycle helpers"
```

---

### Task PG-4: WithTx + WithTxRetry + RetryOption

**Files:**
- Create: `postgres/tx.go`
- Test: `postgres/tx_test.go`

**Interfaces:**
- Produces: `WithTx(ctx, pool, fn) error`; `WithTxRetry(ctx, pool, fn, ...RetryOption) error`; `RetryOption`; `WithRetryAttempts(int) RetryOption`; `WithRetryInterval(time.Duration) RetryOption`.
- Uses `IsSerializationFailure` from PG-5 for the retry predicate. To keep tasks independently testable, PG-4 defines a small **internal** `isSerializationFailure(err error) bool` helper here; PG-5 adds the exported public predicate that reuses the same SQLSTATE check. (When PG-5 lands, the exported `IsSerializationFailure` becomes the canonical one; this internal helper may delegate to it. Both are acceptable since they share the SQLSTATE set `40001`/`40P01`.)

- [ ] **Step 1: Write the failing test**
```go
package postgres_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/postgres"
)

func TestRetryOption_Defaults(t *testing.T) {
	// The defaults are observable through behavior in the env-gated test; here we
	// assert the options are constructible and chainable without panic.
	assert.NotPanics(t, func() {
		_ = postgres.WithRetryAttempts(5)
		_ = postgres.WithRetryInterval(10 * time.Millisecond)
	})
}

func TestWithTx_Integration(t *testing.T) {
	dsn := os.Getenv("FORGE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set FORGE_TEST_POSTGRES_DSN")
	}
	pool := openTestPool(t, dsn)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `CREATE TEMP TABLE tx_test (id int PRIMARY KEY)`)
	require.NoError(t, err)

	// Commit path.
	require.NoError(t, postgres.WithTx(ctx, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO tx_test (id) VALUES (1)`)
		return err
	}))

	// Rollback path: fn returns an error => the row must not be visible.
	wantErr := errors.New("boom")
	err = postgres.WithTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO tx_test (id) VALUES (2)`); err != nil {
			return err
		}
		return wantErr
	})
	require.ErrorIs(t, err, wantErr)

	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM tx_test`).Scan(&count))
	assert.Equal(t, 1, count, "the rolled-back insert must not persist")
}

func TestWithTx_PanicRollsBackAndRepanics(t *testing.T) {
	dsn := os.Getenv("FORGE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set FORGE_TEST_POSTGRES_DSN")
	}
	pool := openTestPool(t, dsn)
	ctx := context.Background()

	assert.PanicsWithValue(t, "kaboom", func() {
		_ = postgres.WithTx(ctx, pool, func(_ pgx.Tx) error {
			panic("kaboom")
		})
	})
}

func TestWithTxRetry_RetriesOnSerializationFailure(t *testing.T) {
	dsn := os.Getenv("FORGE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set FORGE_TEST_POSTGRES_DSN")
	}
	pool := openTestPool(t, dsn)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS txr_test (id int PRIMARY KEY, n int)`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO txr_test (id, n) VALUES (1, 0) ON CONFLICT (id) DO UPDATE SET n = 0`)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DROP TABLE txr_test`) })

	// Two SERIALIZABLE transactions that read-then-write the same row force a 40001
	// on one of them; WithTxRetry must transparently retry it to success.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	start := make(chan struct{})
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = postgres.WithTxRetry(ctx, pool, func(tx pgx.Tx) error {
				var n int
				if err := tx.QueryRow(ctx, `SELECT n FROM txr_test WHERE id = 1`).Scan(&n); err != nil {
					return err
				}
				_, err := tx.Exec(ctx, `UPDATE txr_test SET n = $1 WHERE id = 1`, n+1)
				return err
			}, postgres.WithRetryAttempts(10), postgres.WithRetryInterval(time.Millisecond))
		}(i)
	}
	close(start)
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])

	var n int
	require.NoError(t, pool.QueryRow(ctx, `SELECT n FROM txr_test WHERE id = 1`).Scan(&n))
	assert.Equal(t, 2, n, "both serialized increments must land after retry")
}

func openTestPool(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	cfg := postgres.DefaultConfig()
	cfg.URL = dsn
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(func() { postgres.Close(pool, nil) })
	return pool
}
```

- [ ] **Step 2: Run test to verify it fails**
Run: `go test -race ./postgres/ -run TestRetryOption_Defaults -v`
Expected: FAIL (`undefined: postgres.WithRetryAttempts`).

- [ ] **Step 3: Write the implementation** (`postgres/tx.go`)
```go
package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// retryConfig holds the resolved WithTxRetry knobs.
type retryConfig struct {
	interval time.Duration
	attempts int
}

func defaultRetryConfig() retryConfig {
	return retryConfig{attempts: 3, interval: 50 * time.Millisecond}
}

// RetryOption tunes WithTxRetry's serialization-failure retry loop.
type RetryOption func(*retryConfig)

// WithRetryAttempts sets the total number of attempts (including the first). Values
// below 1 are clamped to 1. Default 3.
func WithRetryAttempts(n int) RetryOption {
	return func(c *retryConfig) {
		if n < 1 {
			n = 1
		}
		c.attempts = n
	}
}

// WithRetryInterval sets the base backoff between retries (interval · 2^attempt,
// capped at maxRetryBackoff). Default 50ms.
func WithRetryInterval(d time.Duration) RetryOption {
	return func(c *retryConfig) {
		if d > 0 {
			c.interval = d
		}
	}
}

// WithTx begins a transaction, runs fn, and commits on success or rolls back on
// error. If fn panics, the transaction is rolled back and the panic is re-raised.
// The rollback's own error is ignored once fn has already failed (the original
// error is the meaningful one).
func WithTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}

	panicked := true
	defer func() {
		if panicked {
			_ = tx.Rollback(ctx) // fn panicked; undo and let the panic propagate
		}
	}()

	if err := fn(tx); err != nil {
		panicked = false
		_ = tx.Rollback(ctx)
		return err
	}

	panicked = false
	return tx.Commit(ctx)
}

// WithTxRetry is WithTx plus an automatic retry loop: when the transaction fails
// with a serialization failure or deadlock (SQLSTATE 40001 / 40P01), it backs off
// and retries up to the configured attempt budget. Any other error returns
// immediately. A panic propagates without retry (WithTx re-raises it).
func WithTxRetry(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error, opts ...RetryOption) error {
	rc := defaultRetryConfig()
	for _, opt := range opts {
		opt(&rc)
	}

	var lastErr error
	for attempt := range rc.attempts {
		err := WithTx(ctx, pool, fn)
		if err == nil {
			return nil
		}
		if !isSerializationFailure(err) {
			return err // non-retryable
		}
		lastErr = err

		if attempt == rc.attempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return errors.Join(lastErr, ctx.Err())
		case <-time.After(backoff(rc.interval, attempt)):
		}
	}
	return lastErr
}
```

Add the internal SQLSTATE predicate to `postgres/tx.go` (PG-5 replaces its body with a delegation to the exported public mirror):
```go
import "github.com/jackc/pgx/v5/pgconn"

// isSerializationFailure reports whether err carries SQLSTATE 40001 (serialization
// failure) or 40P01 (deadlock detected) — the two retryable transaction codes.
func isSerializationFailure(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "40001" || pgErr.Code == "40P01"
}
```

- [ ] **Step 4: Run tests to verify they pass**
Run: `just test ./postgres/...`
Expected: PASS (transaction integration tests skip without `FORGE_TEST_POSTGRES_DSN`; `TestRetryOption_Defaults` runs).

- [ ] **Step 5: Commit**
```bash
git add postgres/ && git commit -m "feat(postgres): add WithTx, WithTxRetry, and RetryOption"
```

---

### Task PG-5: error-classification helpers

**Files:**
- Create: `postgres/classify.go`
- Test: `postgres/classify_test.go`
- Edit: `postgres/tx.go` (replace the internal `isSerializationFailure` with a delegation to the new public `IsSerializationFailure`)

**Interfaces:**
- Produces: `IsUniqueViolation(err) bool`; `IsForeignKeyViolation(err) bool`; `IsNotFound(err) bool`; `IsSerializationFailure(err) bool`.

- [ ] **Step 1: Write the failing test** (fully unit-testable with synthetic `*pgconn.PgError` and `pgx.ErrNoRows` — no server needed)
```go
package postgres_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/postgres"
)

func pgErr(code string) error {
	// Wrap to prove the predicates unwrap with errors.As, not a bare type assert.
	return fmt.Errorf("query failed: %w", &pgconn.PgError{Code: code})
}

func TestIsUniqueViolation(t *testing.T) {
	assert.True(t, postgres.IsUniqueViolation(pgErr("23505")))
	assert.False(t, postgres.IsUniqueViolation(pgErr("23503")))
	assert.False(t, postgres.IsUniqueViolation(errors.New("plain")))
	assert.False(t, postgres.IsUniqueViolation(nil))
}

func TestIsForeignKeyViolation(t *testing.T) {
	assert.True(t, postgres.IsForeignKeyViolation(pgErr("23503")))
	assert.False(t, postgres.IsForeignKeyViolation(pgErr("23505")))
	assert.False(t, postgres.IsForeignKeyViolation(nil))
}

func TestIsNotFound(t *testing.T) {
	assert.True(t, postgres.IsNotFound(pgx.ErrNoRows))
	assert.True(t, postgres.IsNotFound(fmt.Errorf("wrapped: %w", pgx.ErrNoRows)))
	assert.False(t, postgres.IsNotFound(pgErr("23505")))
	assert.False(t, postgres.IsNotFound(nil))
}

func TestIsSerializationFailure(t *testing.T) {
	assert.True(t, postgres.IsSerializationFailure(pgErr("40001")))
	assert.True(t, postgres.IsSerializationFailure(pgErr("40P01")))
	assert.False(t, postgres.IsSerializationFailure(pgErr("23505")))
	assert.False(t, postgres.IsSerializationFailure(nil))
}
```

- [ ] **Step 2: Run test to verify it fails**
Run: `go test -race ./postgres/ -run TestIsUniqueViolation -v`
Expected: FAIL (`undefined: postgres.IsUniqueViolation`).

- [ ] **Step 3: Write `postgres/classify.go`**
```go
package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// PostgreSQL SQLSTATE codes recognized by the classification predicates.
const (
	sqlstateUniqueViolation     = "23505"
	sqlstateForeignKeyViolation = "23503"
	sqlstateSerializationFail   = "40001"
	sqlstateDeadlockDetected    = "40P01"
)

// sqlState extracts the SQLSTATE code from err if it (or anything it wraps) is a
// *pgconn.PgError; otherwise it returns "".
func sqlState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// IsUniqueViolation reports whether err is a unique-constraint violation (SQLSTATE
// 23505). Use it instead of importing pgconn and matching codes at the call site.
func IsUniqueViolation(err error) bool {
	return sqlState(err) == sqlstateUniqueViolation
}

// IsForeignKeyViolation reports whether err is a foreign-key violation (SQLSTATE
// 23503).
func IsForeignKeyViolation(err error) bool {
	return sqlState(err) == sqlstateForeignKeyViolation
}

// IsNotFound reports whether err is pgx.ErrNoRows — a missing row, not a failure.
func IsNotFound(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

// IsSerializationFailure reports whether err is a serialization failure (40001) or
// a detected deadlock (40P01) — the two codes WithTxRetry retries.
func IsSerializationFailure(err error) bool {
	code := sqlState(err)
	return code == sqlstateSerializationFail || code == sqlstateDeadlockDetected
}
```

- [ ] **Step 4: Collapse the internal helper in `postgres/tx.go`**
Replace the internal `isSerializationFailure` function and its `pgconn` import in `tx.go` with a one-line delegation so there is a single SQLSTATE source of truth:
```go
// isSerializationFailure delegates to the public predicate so the retry loop and
// classification helpers share one SQLSTATE definition.
func isSerializationFailure(err error) bool {
	return IsSerializationFailure(err)
}
```
(Remove the now-unused `"github.com/jackc/pgx/v5/pgconn"` import from `tx.go`.)

- [ ] **Step 5: Run tests to verify they pass**
Run: `just test ./postgres/...`
Expected: PASS

- [ ] **Step 6: Commit**
```bash
git add postgres/ && git commit -m "feat(postgres): add SQLSTATE error-classification predicates"
```

---

### Task PG-6: Migrator interface + WithMigrator + stdlib bridge wired into Open

**Files:**
- Create: `postgres/migrator.go`
- Edit: `postgres/options.go` (add `WithMigrator`)
- Edit: `postgres/postgres.go` (run the migrator after a live+pinged pool)
- Test: `postgres/migrator_test.go`

**Interfaces:**
- Produces: `type Migrator interface { Up(ctx context.Context, db *sql.DB) error }`; `WithMigrator(m Migrator) Option`. Wires the `stdlib.OpenDBFromPool` bridge into `Open`.

- [ ] **Step 1: Write the failing test** (pure unit tests with a fake migrator; the fail-Open-on-migration-error path is env-gated since it needs a live pool)
```go
package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/postgres"
)

// fakeMigrator records whether Up was called and returns a fixed error.
type fakeMigrator struct {
	called bool
	err    error
}

func (f *fakeMigrator) Up(_ context.Context, db *sql.DB) error {
	f.called = true
	return f.err
}

func TestWithMigrator_NilRejected(t *testing.T) {
	cfg := postgres.DefaultConfig()
	cfg.URL = "postgres://u:p@127.0.0.1:1/db"
	pool, err := postgres.Open(context.Background(),
		postgres.WithConfig(cfg),
		postgres.WithMigrator(nil),
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, postgres.ErrInvalidConfig)
	assert.Nil(t, pool)
}

func TestWithMigrator_NotRunWhenConnectFails(t *testing.T) {
	// Unreachable addr: Open never reaches a live pool, so the migrator must not run.
	cfg := postgres.DefaultConfig()
	cfg.URL = "postgres://u:p@127.0.0.1:1/db"
	cfg.RetryAttempts = 1
	cfg.ConnectTimeout = 100 * time.Millisecond
	fm := &fakeMigrator{}
	pool, err := postgres.Open(context.Background(),
		postgres.WithConfig(cfg),
		postgres.WithMigrator(fm),
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, postgres.ErrConnect)
	assert.Nil(t, pool)
	assert.False(t, fm.called, "migrator must not run when the pool never came up")
}

func TestWithMigrator_RunsAfterConnect_Integration(t *testing.T) {
	dsn := os.Getenv("FORGE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set FORGE_TEST_POSTGRES_DSN")
	}
	cfg := postgres.DefaultConfig()
	cfg.URL = dsn

	// Success path: the migrator runs and Open returns a live pool.
	ok := &fakeMigrator{}
	pool, err := postgres.Open(context.Background(),
		postgres.WithConfig(cfg),
		postgres.WithMigrator(ok),
	)
	require.NoError(t, err)
	require.NotNil(t, pool)
	assert.True(t, ok.called, "migrator must run after a live+pinged pool")
	postgres.Close(pool, nil)

	// Failure path: a migrator error fails Open and leaks no pool.
	boom := errors.New("migration boom")
	fail := &fakeMigrator{err: boom}
	pool, err = postgres.Open(context.Background(),
		postgres.WithConfig(cfg),
		postgres.WithMigrator(fail),
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, boom, "a failed migration must surface as a failed Open")
	assert.Nil(t, pool)
	assert.True(t, fail.called)
}
```

- [ ] **Step 2: Run test to verify it fails**
Run: `go test -race ./postgres/ -run TestWithMigrator_NilRejected -v`
Expected: FAIL (`undefined: postgres.WithMigrator`).

- [ ] **Step 3: Write `postgres/migrator.go`**
```go
package postgres

import (
	"context"
	"database/sql"
)

// Migrator is the one-method seam between this package and migration. It is
// structural: postgres does not import migration, and *migration.Migrator satisfies
// this interface, so postgres.WithMigrator(migration.New(fsys)) just works.
//
// Up applies pending schema changes against the supplied *sql.DB, which Open
// bridges from the live pool with stdlib.OpenDBFromPool. The *sql.DB shares the
// pool's connections and must not be closed by the implementation.
type Migrator interface {
	Up(ctx context.Context, db *sql.DB) error
}
```

- [ ] **Step 4: Add the `migrator` field and `WithMigrator` to `postgres/options.go`**

First add the `migrator Migrator` field to the `config` struct (the `Migrator` type now exists from Step 3), so it becomes:
```go
type config struct {
	logger     *slog.Logger
	poolConfig func(*pgxpool.Config)
	migrator   Migrator
	errs       []error
	Config
}
```
Then add the option:
```go
// WithMigrator registers a Migrator that Open runs after the pool is live and
// pinged but before Open returns. A failed migration fails Open. A nil Migrator is
// rejected (ErrInvalidConfig). Pass migration.New(fsys) directly — *migration.Migrator
// satisfies the Migrator interface.
func WithMigrator(m Migrator) Option {
	return func(c *config) {
		if m == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithMigrator received a nil Migrator", ErrInvalidConfig))
			return
		}
		c.migrator = m
	}
}
```

- [ ] **Step 5: Wire the bridge into `Open`** in `postgres/postgres.go`. Add the stdlib import and replace the `// Migrator wiring is added in PG-6.` comment with the run block:
```go
import (
	// ... existing imports ...
	"github.com/jackc/pgx/v5/stdlib"
)
```
Replace the placeholder comment in `Open` with:
```go
	if cfg.migrator != nil {
		// OpenDBFromPool shares the pool's connections; the *sql.DB must NOT be
		// closed or it would tear down the live pool. A failed migration is a failed
		// Open, so we close the pool and surface the error.
		sqlDB := stdlib.OpenDBFromPool(pool)
		if err := cfg.migrator.Up(ctx, sqlDB); err != nil {
			pool.Close()
			return nil, fmt.Errorf("postgres: migrate: %w", err)
		}
	}
```

- [ ] **Step 6: Run tests to verify they pass**
Run: `just test ./postgres/...`
Expected: PASS (the integration migrator test skips without `FORGE_TEST_POSTGRES_DSN`; nil-rejection and not-run-on-connect-fail run).

- [ ] **Step 7: Commit**
```bash
git add postgres/ && git commit -m "feat(postgres): add Migrator seam, WithMigrator, and stdlib bridge in Open"
```

---

### Task PG-7: doc.go + lint

**Files:**
- Create: `postgres/doc.go`

**Interfaces:**
- Produces: package documentation with an env-var table and a runnable example, matching `supervisor/doc.go` style.

- [ ] **Step 1: Write `postgres/doc.go`**
```go
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
//		_ = env.ParseWithOptions(&cfg, env.Options{Prefix: "DATABASE_"})
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
// Error classification: IsUniqueViolation, IsForeignKeyViolation, IsNotFound, and
// IsSerializationFailure match the underlying *pgconn.PgError (or pgx.ErrNoRows) so
// call sites branch without importing pgconn or matching SQLSTATE strings.
//
// Sentinel errors returned by this package — ErrInvalidConfig, ErrConnect,
// ErrHealthcheck — are single-line and matchable with errors.Is.
//
// Configuration is supplied through Config, whose env struct tags are inert (no
// loader is imported). DefaultConfig is the single source of truth for defaults:
//
//	Field              Env var (no prefix)   Default
//	URL                URL                    "" (required)
//	MaxConns           MAX_CONNS              10
//	MinConns           MIN_CONNS             2
//	MaxConnLifetime    MAX_CONN_LIFETIME     30m
//	MaxConnIdleTime    MAX_CONN_IDLE_TIME    10m
//	HealthCheckPeriod  HEALTH_CHECK_PERIOD   1m
//	ConnectTimeout     CONNECT_TIMEOUT       5s
//	RetryAttempts      RETRY_ATTEMPTS        3
//	RetryInterval      RETRY_INTERVAL        1s
package postgres
```

- [ ] **Step 2: Run lint and the full package test**
Run: `just lint && just test ./postgres/...`
Expected: PASS (no lint findings; all tests green/skip).

- [ ] **Step 3: Commit**
```bash
git add postgres/ && git commit -m "docs(postgres): add package doc with env table and runnable example"
```

---

## Package: `migration`

### Task MIG-1: New / Migrator / Up (goose Provider API) + WithTable / WithLogger + errors

**Files:**
- Create: `migration/errors.go`
- Create: `migration/options.go`
- Create: `migration/migration.go`
- Test: `migration/migration_test.go`

**Interfaces:**
- Produces: `migration.New(fsys fs.FS, opts ...Option) *Migrator`; `(*Migrator) Up(ctx context.Context, db *sql.DB) error`; `WithTable(name string) Option`; `WithLogger(l *slog.Logger) Option`; sentinels `ErrInvalidConfig`, `ErrMigrate`.
- `*migration.Migrator` structurally satisfies `postgres.Migrator` (same `Up(ctx, *sql.DB) error` signature).

- [ ] **Step 1: Add the goose dependency**
Run:
```bash
go get github.com/pressly/goose/v3@v3.27.1
```
Expected: `go.mod`/`go.sum` updated with `github.com/pressly/goose/v3 v3.27.1`.

- [ ] **Step 2: Write the failing test** (unit: `New` applies the default table; env-gated: a real migration creates a table and a second `Up` is a no-op)
```go
package migration_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"testing/fstest"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/migration"
)

// oneMigration is a minimal goose SQL migration creating a table.
var oneMigration = fstest.MapFS{
	"00001_create_widgets.sql": &fstest.MapFile{Data: []byte(`-- +goose Up
CREATE TABLE widgets (id bigserial PRIMARY KEY, name text NOT NULL);
-- +goose Down
DROP TABLE widgets;
`)},
}

func TestNew_ReturnsMigrator(t *testing.T) {
	// New never returns an error and never touches a database — it only stores
	// config. The default version table is applied lazily inside Up.
	m := migration.New(oneMigration)
	require.NotNil(t, m)

	m = migration.New(oneMigration, migration.WithTable("custom_versions"))
	require.NotNil(t, m)
}

func TestUp_EmptyFS_IsNoop(t *testing.T) {
	dsn := os.Getenv("FORGE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set FORGE_TEST_POSTGRES_DSN")
	}
	db := openDB(t, dsn)

	// An fsys with no migration files must succeed as a no-op, not error out, so an
	// app that embeds an empty migrations dir still boots.
	m := migration.New(fstest.MapFS{})
	require.NoError(t, m.Up(context.Background(), db))
}

func TestUp_AppliesMigration_Integration(t *testing.T) {
	dsn := os.Getenv("FORGE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set FORGE_TEST_POSTGRES_DSN")
	}
	db := openDB(t, dsn)
	t.Cleanup(func() {
		_, _ = db.Exec(`DROP TABLE IF EXISTS widgets`)
		_, _ = db.Exec(`DROP TABLE IF EXISTS schema_migrations`)
	})

	m := migration.New(oneMigration)
	ctx := context.Background()

	require.NoError(t, m.Up(ctx, db))

	// The migration's table now exists.
	var exists bool
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'widgets')`,
	).Scan(&exists))
	assert.True(t, exists, "the migration must have created the widgets table")

	// The default goose version table is named schema_migrations.
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'schema_migrations')`,
	).Scan(&exists))
	assert.True(t, exists, "the default version table must be schema_migrations")

	// A second Up is an idempotent no-op (no pending migrations).
	require.NoError(t, m.Up(ctx, db), "re-running Up with nothing pending must succeed")
}

func openDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	require.NoError(t, db.Ping())
	t.Cleanup(func() { _ = db.Close() })
	return db
}
```

- [ ] **Step 3: Run test to verify it fails**
Run: `go test -race ./migration/ -run TestNew_ReturnsMigrator -v`
Expected: FAIL (`undefined: migration.New`).

- [ ] **Step 4: Write `migration/errors.go`**
```go
package migration

import "errors"

// Sentinel errors returned (often wrapped) by this package. Match with errors.Is.
var (
	// ErrInvalidConfig is returned by an option when it receives an invalid value.
	ErrInvalidConfig = errors.New("migration: invalid config")
	// ErrMigrate wraps a failure to build the goose provider or apply migrations.
	ErrMigrate = errors.New("migration: migrate failed")
)
```

- [ ] **Step 5: Write `migration/options.go`**
```go
package migration

import "log/slog"

// DefaultTable is the goose version table name used when WithTable is not supplied.
const DefaultTable = "schema_migrations"

// config holds the resolved Migrator settings. table always carries DefaultTable
// unless WithTable overrides it; logger is optional.
type config struct {
	logger *slog.Logger
	table  string
}

// Option configures a Migrator built by New.
type Option func(*config)

// WithTable sets the goose version table name. An empty name is ignored, leaving
// DefaultTable ("schema_migrations") in place.
func WithTable(name string) Option {
	return func(c *config) {
		if name != "" {
			c.table = name
		}
	}
}

// WithLogger sets an slog.Logger for migration progress lines. A nil logger is
// ignored (goose's output is suppressed via a no-op adapter).
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l != nil {
			c.logger = l
		}
	}
}
```

- [ ] **Step 6: Write `migration/migration.go`** (instance-based goose Provider; `Up` builds the provider lazily so an empty fsys does not error at construction; dialect fixed to Postgres)
```go
package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/pressly/goose/v3"
)

// Migrator applies pending up-migrations from an fs.FS against a *sql.DB using
// goose's instance-based Provider API (no global mutable state). It is up-only:
// there is no Down/Version/Status. The zero value is not usable; build one with New.
//
// *Migrator structurally satisfies the postgres.Migrator interface, so it can be
// passed straight to postgres.WithMigrator.
type Migrator struct {
	fsys fs.FS
	cfg  config
}

// New returns a Migrator that applies the migrations rooted at fsys. Migrations live
// at the root of fsys; embed a subdirectory with fs.Sub if needed. The dialect is
// fixed to PostgreSQL. New never returns an error and never contacts a database — it
// only stores configuration; the goose provider is built per call to Up.
func New(fsys fs.FS, opts ...Option) *Migrator {
	cfg := config{table: DefaultTable}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Migrator{fsys: fsys, cfg: cfg}
}

// Up applies all pending migrations against db. It builds a fresh goose Provider
// (dialect Postgres, the configured version table) and runs provider.Up. An fsys
// with no migration files is treated as a successful no-op. The db is owned by the
// caller and is never closed here. Errors wrap ErrMigrate and are single-line.
func (m *Migrator) Up(ctx context.Context, db *sql.DB) error {
	opts := []goose.ProviderOption{goose.WithTableName(m.cfg.table)}
	if m.cfg.logger != nil {
		opts = append(opts, goose.WithLogger(slogGooseLogger{m.cfg.logger}))
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, db, m.fsys, opts...)
	if err != nil {
		// An empty fs.FS is a no-op, not a failure: an app that embeds an empty
		// migrations directory should still boot cleanly.
		if errors.Is(err, goose.ErrNoMigrations) {
			return nil
		}
		return fmt.Errorf("%w: new provider: %v", ErrMigrate, err)
	}

	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("%w: %v", ErrMigrate, err)
	}
	return nil
}

// slogGooseLogger adapts an *slog.Logger to goose's Logger interface so migration
// progress is emitted as structured log lines instead of stdlib log output.
type slogGooseLogger struct {
	l *slog.Logger
}

func (g slogGooseLogger) Printf(format string, v ...any) {
	g.l.Info(fmt.Sprintf(format, v...))
}

func (g slogGooseLogger) Fatalf(format string, v ...any) {
	g.l.Error(fmt.Sprintf(format, v...))
}
```

- [ ] **Step 7: Run tests to verify they pass**
Run: `just test ./migration/...`
Expected: PASS (the integration tests skip without `FORGE_TEST_POSTGRES_DSN`; `TestNew_ReturnsMigrator` runs).

- [ ] **Step 8: Commit**
```bash
git add migration/ go.mod go.sum && git commit -m "feat(migration): add up-only Migrator over goose Provider API"
```

---

### Task MIG-2: doc.go

**Files:**
- Create: `migration/doc.go`

**Interfaces:**
- Produces: package documentation with a runnable example and the migration-seam note, matching `supervisor/doc.go` style.

- [ ] **Step 1: Write `migration/doc.go`**
```go
// Package migration applies pending up-migrations from an embedded fs.FS against a
// *sql.DB, using goose's instance-based Provider API so two Migrators never clobber
// each other's global state. It is deliberately up-only — it applies all pending
// migrations and nothing else. There is no Down/Version/Status/reset/redo; rollbacks
// and inspection are done with the goose CLI against the same version table, out of
// band.
//
// New stores configuration only; the goose Provider is built per Up call, which
// takes the *sql.DB the caller already owns. The dialect is fixed to PostgreSQL,
// the framework's declared database. Migrations live at the root of fsys; embed a
// subdirectory with fs.Sub if needed.
//
//	//go:embed migrations/*.sql
//	var migrationsFS embed.FS
//
//	func main() {
//		ctx, stop := supervisor.NewContext()
//		defer stop()
//
//		sub, _ := fs.Sub(migrationsFS, "migrations")
//
//		pool, err := postgres.Open(ctx,
//			postgres.WithConfig(cfg),
//			postgres.WithMigrator(migration.New(sub)), // up-migrate on boot
//		)
//		if err != nil {
//			slog.Error("db open failed", "err", err)
//			os.Exit(1)
//		}
//		defer postgres.Close(pool, slog.Default())
//	}
//
// The migration seam: *Migrator structurally satisfies the one-method
// postgres.Migrator interface (Up(ctx, *sql.DB) error), so the value is passed
// straight to postgres.WithMigrator with no adapter. postgres bridges its pool to a
// *sql.DB with stdlib.OpenDBFromPool and calls Up; this package never imports pgx,
// and postgres never imports goose. A failed migration fails postgres.Open.
//
// An fs.FS containing no migration files is a successful no-op, so embedding an empty
// migrations directory still boots cleanly.
//
// Options: WithTable sets the goose version table (default "schema_migrations");
// WithLogger routes goose progress through an *slog.Logger. Errors wrap the
// single-line sentinels ErrInvalidConfig and ErrMigrate and are matchable with
// errors.Is.
package migration
```

- [ ] **Step 2: Run lint and the full package test**
Run: `just lint && just test ./migration/...`
Expected: PASS (no lint findings; tests green/skip).

- [ ] **Step 3: Commit**
```bash
git add migration/ && git commit -m "docs(migration): add package doc with example and seam note"
```
## Package: `redis` (Redis + Valkey; standalone / cluster / sentinel)

> **Internal driver alias.** The package is named `redis`, so inside every `.go` file the go-redis driver is imported aliased as `goredis "github.com/redis/go-redis/v9"` to avoid the self-clash. The public API still returns `goredis.UniversalClient`, `goredis.Cmdable`, etc., which render in godoc as `redis.UniversalClient` / `redis.Cmdable` (the driver's own package name). Task signatures below use the godoc spelling `redis.UniversalClient` for readability; the implementation uses the `goredis.*` alias.
>
> Tests are **black-box** (`package redis_test`). They import forge's package aliased as `forgeredis "github.com/dmitrymomot/forge/redis"` and the driver as `goredis "github.com/redis/go-redis/v9"`.

### Task RD-1: Config, DefaultConfig, Validate, errors

**Files:**
- Create: `redis/config.go`
- Create: `redis/errors.go`
- Test: `redis/config_test.go`

**Interfaces:**
- Produces: `redis.Config`; `DefaultConfig() Config`; `(Config) Validate() error`; `ErrInvalidConfig`/`ErrConnect`/`ErrHealthcheck`.

- [ ] **Step 1: Write the failing test**
```go
package redis_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgeredis "github.com/dmitrymomot/forge/redis"
)

func TestDefaultConfig(t *testing.T) {
	cfg := forgeredis.DefaultConfig()
	assert.Equal(t, 10, cfg.PoolSize)
	assert.Equal(t, 5*time.Second, cfg.DialTimeout)
	assert.Equal(t, 3*time.Second, cfg.ReadTimeout)
	assert.Equal(t, 3*time.Second, cfg.WriteTimeout)
	assert.Equal(t, 3, cfg.RetryAttempts)
	assert.Equal(t, time.Second, cfg.RetryInterval)
	// Fields left at zero / driver-default on purpose.
	assert.Empty(t, cfg.Addresses, "Addresses has no default; the consumer must supply it")
	assert.Empty(t, cfg.MasterName)
	assert.Empty(t, cfg.Username)
	assert.Empty(t, cfg.Password)
	assert.Zero(t, cfg.DB)
	assert.Zero(t, cfg.MinIdleConns)
	assert.Zero(t, cfg.ConnMaxIdleTime)
	// DefaultConfig alone is not valid: Addresses is required.
	require.Error(t, cfg.Validate())
}

func TestConfig_Validate(t *testing.T) {
	good := forgeredis.DefaultConfig()
	good.Addresses = []string{"127.0.0.1:6379"}
	require.NoError(t, good.Validate())

	bad := map[string]forgeredis.Config{
		"empty addresses":   {Addresses: nil},
		"neg dial timeout":  {Addresses: []string{"127.0.0.1:6379"}, DialTimeout: -1},
		"neg read timeout":  {Addresses: []string{"127.0.0.1:6379"}, ReadTimeout: -1},
		"neg write timeout": {Addresses: []string{"127.0.0.1:6379"}, WriteTimeout: -1},
		"neg conn idle":     {Addresses: []string{"127.0.0.1:6379"}, ConnMaxIdleTime: -1},
		"neg retry attempts": {Addresses: []string{"127.0.0.1:6379"}, RetryAttempts: -1},
		"neg retry interval": {Addresses: []string{"127.0.0.1:6379"}, RetryInterval: -1},
	}
	for name, cfg := range bad {
		t.Run(name, func(t *testing.T) {
			err := cfg.Validate()
			require.Error(t, err)
			assert.ErrorIs(t, err, forgeredis.ErrInvalidConfig)
		})
	}
}

func TestConfig_EnvTags(t *testing.T) {
	want := map[string]string{
		"Addresses":       "ADDRESSES",
		"MasterName":      "MASTER_NAME",
		"Username":        "USERNAME",
		"Password":        "PASSWORD",
		"DB":              "DB",
		"PoolSize":        "POOL_SIZE",
		"MinIdleConns":    "MIN_IDLE_CONNS",
		"DialTimeout":     "DIAL_TIMEOUT",
		"ReadTimeout":     "READ_TIMEOUT",
		"WriteTimeout":    "WRITE_TIMEOUT",
		"ConnMaxIdleTime": "CONN_MAX_IDLE_TIME",
		"RetryAttempts":   "RETRY_ATTEMPTS",
		"RetryInterval":   "RETRY_INTERVAL",
	}
	typ := reflect.TypeFor[forgeredis.Config]()
	for fname, tag := range want {
		f, ok := typ.FieldByName(fname)
		require.Truef(t, ok, "field %s missing", fname)
		assert.Equalf(t, tag, f.Tag.Get("env"), "field %s env tag", fname)
	}
}

func TestErrSentinels_Distinct(t *testing.T) {
	// The three sentinels are distinct, matchable values.
	assert.NotErrorIs(t, forgeredis.ErrConnect, forgeredis.ErrInvalidConfig)
	assert.NotErrorIs(t, forgeredis.ErrHealthcheck, forgeredis.ErrConnect)
	assert.NotErrorIs(t, forgeredis.ErrInvalidConfig, forgeredis.ErrHealthcheck)
}
```
- [ ] **Step 2: Run test to verify it fails**
Run: `go test -race ./redis/ -run TestDefaultConfig -v`
Expected: FAIL (build error: `forgeredis.DefaultConfig` / `forgeredis.Config` undefined)
- [ ] **Step 3: Write the implementation**

`redis/errors.go`:
```go
package redis

import "errors"

// Sentinel errors returned by this package, wrapped around the underlying driver
// error. Match them with errors.Is. They are single-line and carry no embedded
// stacks or multi-line blobs, per the framework's structured-logging rule.
var (
	// ErrInvalidConfig is returned (joined) by Validate and Open when a Config
	// field or an option has an unusable value.
	ErrInvalidConfig = errors.New("redis: invalid config")
	// ErrConnect is returned by Open when the client cannot reach the server
	// after exhausting the bounded connect-retry loop.
	ErrConnect = errors.New("redis: connect failed")
	// ErrHealthcheck is returned by the Healthcheck closure when a PING fails.
	ErrHealthcheck = errors.New("redis: healthcheck failed")
)
```

`redis/config.go`:
```go
package redis

import (
	"errors"
	"fmt"
	"time"
)

// Config holds the serializable settings for a Redis (or Valkey) client. The env
// struct tags are inert strings — this package imports no config loader. Seed from
// DefaultConfig and parse the environment over it with whatever loader reads env
// struct tags. Field order is subject to the repo's betteralign tooling.
//
// Topology is selected from these fields by go-redis's NewUniversalClient: a single
// Addresses entry with an empty MasterName is standalone, multiple entries are a
// cluster, and a non-empty MasterName is sentinel/failover (Addresses then lists the
// sentinels). DB applies to standalone/sentinel only; cluster ignores it.
type Config struct {
	Addresses       []string      `env:"ADDRESSES"`     // 1 = standalone, many = cluster; sentinels when MasterName set
	MasterName      string        `env:"MASTER_NAME"`   // set -> sentinel/failover mode
	Username        string        `env:"USERNAME"`      // ACL username (Redis 6+); empty for legacy auth
	Password        string        `env:"PASSWORD"`      // empty when the server requires no auth
	DB              int           `env:"DB"`            // standalone/sentinel only (cluster ignores it)
	PoolSize        int           `env:"POOL_SIZE"`     // max connections per node
	MinIdleConns    int           `env:"MIN_IDLE_CONNS"`
	DialTimeout     time.Duration `env:"DIAL_TIMEOUT"`
	ReadTimeout     time.Duration `env:"READ_TIMEOUT"`
	WriteTimeout    time.Duration `env:"WRITE_TIMEOUT"`
	ConnMaxIdleTime time.Duration `env:"CONN_MAX_IDLE_TIME"`
	RetryAttempts   int           `env:"RETRY_ATTEMPTS"` // bounded connect-retry attempts in Open
	RetryInterval   time.Duration `env:"RETRY_INTERVAL"` // base backoff between connect attempts
}

// DefaultConfig returns production-sane defaults and is the single source of truth
// for them (there are no envDefault tags to drift from it). Addresses is left empty
// on purpose — it has no universal default and must be supplied; DefaultConfig alone
// therefore fails Validate.
func DefaultConfig() Config {
	return Config{
		PoolSize:      10,
		DialTimeout:   5 * time.Second,
		ReadTimeout:   3 * time.Second,
		WriteTimeout:  3 * time.Second,
		RetryAttempts: 3,
		RetryInterval: 1 * time.Second,
	}
}

// Validate reports whether the field values are usable, returning an
// ErrInvalidConfig-wrapped, single-line joined error otherwise. Open calls it
// defensively; callers may call it after env-loading (zero-trust).
func (c Config) Validate() error {
	var errs []error
	if len(c.Addresses) == 0 {
		errs = append(errs, fmt.Errorf("%w: Addresses must not be empty", ErrInvalidConfig))
	}
	for _, f := range []struct {
		name string
		d    time.Duration
	}{
		{"DialTimeout", c.DialTimeout},
		{"ReadTimeout", c.ReadTimeout},
		{"WriteTimeout", c.WriteTimeout},
		{"ConnMaxIdleTime", c.ConnMaxIdleTime},
		{"RetryInterval", c.RetryInterval},
	} {
		if f.d < 0 {
			errs = append(errs, fmt.Errorf("%w: %s must be >= 0", ErrInvalidConfig, f.name))
		}
	}
	if c.RetryAttempts < 0 {
		errs = append(errs, fmt.Errorf("%w: RetryAttempts must be >= 0", ErrInvalidConfig))
	}
	return errors.Join(errs...)
}
```
- [ ] **Step 4: Run tests to verify they pass**
Run: `just test ./redis/...`
Expected: PASS
- [ ] **Step 5: Commit**
```bash
git add redis/ && git commit -m "feat(redis): add Config, DefaultConfig, Validate, sentinel errors"
```

---

### Task RD-2: go-redis dependency, options, `buildOptions`, and `Open` with topology selection + connect-retry

**Files:**
- Create: `redis/options.go`
- Create: `redis/redis.go`
- Test: `redis/options_test.go`
- Test: `redis/open_test.go`
- Modify: `go.mod` / `go.sum` (via `go get`)

**Interfaces:**
- Produces: `Option`; `WithConfig(Config) Option`; `WithLogger(*slog.Logger) Option`; `WithUniversalOptions(func(*redis.UniversalOptions)) Option`; `Open(ctx, ...Option) (redis.UniversalClient, error)`. Internal: `type config`, `buildOptions(Config) *goredis.UniversalOptions`.
- Depends on: RD-1 (`Config`, sentinels).

- [ ] **Step 1: Add the dependency**
Run:
```bash
go get github.com/redis/go-redis/v9@v9.21.0
```
Expected: `go.mod` now requires `github.com/redis/go-redis/v9 v9.21.0`.

- [ ] **Step 2: Write the failing tests**

`redis/options_test.go`:
```go
package redis_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgeredis "github.com/dmitrymomot/forge/redis"
)

func TestOpen_NilOptionRejected(t *testing.T) {
	opts := map[string]forgeredis.Option{
		"universaloptions": forgeredis.WithUniversalOptions(nil),
		"logger":           forgeredis.WithLogger(nil),
	}
	for name, opt := range opts {
		t.Run(name, func(t *testing.T) {
			// A valid Addresses is supplied so the only fault is the nil option:
			// the failure must be ErrInvalidConfig, surfaced before any dial.
			c, err := forgeredis.Open(t.Context(),
				forgeredis.WithConfig(validConfig()),
				opt,
			)
			require.Error(t, err)
			assert.ErrorIs(t, err, forgeredis.ErrInvalidConfig)
			assert.Nil(t, c, "no client is returned on a config/option error")
		})
	}
}

func TestOpen_InvalidConfigRejected(t *testing.T) {
	// Pure DefaultConfig has no Addresses -> Validate fails -> ErrInvalidConfig,
	// with no network attempt.
	c, err := forgeredis.Open(t.Context())
	require.Error(t, err)
	assert.ErrorIs(t, err, forgeredis.ErrInvalidConfig)
	assert.Nil(t, c)
}
```

`redis/open_test.go`:
```go
package redis_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgeredis "github.com/dmitrymomot/forge/redis"
	goredis "github.com/redis/go-redis/v9"
)

// validConfig is a tiny, fast Config used by tests that must pass Validate but never
// (or only briefly) touch the network.
func validConfig() forgeredis.Config {
	cfg := forgeredis.DefaultConfig()
	cfg.Addresses = []string{"127.0.0.1:6379"}
	return cfg
}

// slogDiscard is the shared test logger.
func slogDiscard() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestBuildOptions(t *testing.T) {
	// buildOptions is unexported, so it is exercised indirectly: Open(...) feeds the
	// produced *goredis.UniversalOptions to goredis.NewUniversalClient, which we can
	// observe through WithUniversalOptions — the escape hatch runs LAST and receives
	// the fully-built options, letting us assert the Config -> UniversalOptions map.
	cfg := validConfig()
	cfg.Addresses = []string{"10.0.0.1:6379", "10.0.0.2:6379"}
	cfg.MasterName = "mymaster"
	cfg.Username = "user"
	cfg.Password = "secret"
	cfg.DB = 7
	cfg.PoolSize = 42
	cfg.MinIdleConns = 5
	cfg.ConnMaxIdleTime = 9 * time.Minute
	cfg.RetryAttempts = 1 // single attempt so Open returns fast after observing opts
	cfg.RetryInterval = time.Millisecond
	cfg.DialTimeout = 50 * time.Millisecond
	cfg.ReadTimeout = 50 * time.Millisecond
	cfg.WriteTimeout = 50 * time.Millisecond

	var seen *goredis.UniversalOptions
	// The dial will fail (nothing listening), but the escape hatch fires BEFORE the
	// dial, capturing the mapped options regardless of connectivity.
	_, _ = forgeredis.Open(t.Context(),
		forgeredis.WithConfig(cfg),
		forgeredis.WithUniversalOptions(func(o *goredis.UniversalOptions) {
			seen = o
		}),
	)
	require.NotNil(t, seen, "the escape hatch must run with the built options")
	assert.Equal(t, cfg.Addresses, seen.Addrs)
	assert.Equal(t, "mymaster", seen.MasterName)
	assert.Equal(t, "user", seen.Username)
	assert.Equal(t, "secret", seen.Password)
	assert.Equal(t, 7, seen.DB)
	assert.Equal(t, 42, seen.PoolSize)
	assert.Equal(t, 5, seen.MinIdleConns)
	assert.Equal(t, 9*time.Minute, seen.ConnMaxIdleTime)
	assert.Equal(t, 50*time.Millisecond, seen.DialTimeout)
	assert.Equal(t, 50*time.Millisecond, seen.ReadTimeout)
	assert.Equal(t, 50*time.Millisecond, seen.WriteTimeout)
	// Topology note: goredis.NewUniversalClient selects standalone/cluster/sentinel
	// from Addrs + MasterName above — here MasterName is set, so it builds a
	// failover (sentinel) client. The mapping is what we assert; the selection is the
	// driver's documented behavior.
}

func TestOpen_RetryExhausted(t *testing.T) {
	cfg := forgeredis.DefaultConfig()
	cfg.Addresses = []string{"127.0.0.1:1"} // port 1: nothing listens, refused fast
	cfg.RetryAttempts = 2
	cfg.RetryInterval = time.Millisecond
	cfg.DialTimeout = 50 * time.Millisecond
	cfg.ReadTimeout = 50 * time.Millisecond
	cfg.WriteTimeout = 50 * time.Millisecond

	start := time.Now()
	c, err := forgeredis.Open(t.Context(), forgeredis.WithConfig(cfg))
	require.Error(t, err)
	assert.ErrorIs(t, err, forgeredis.ErrConnect, "exhausted retries report ErrConnect")
	assert.Nil(t, c, "a failed Open returns no client and leaks none")
	// 2 attempts with a 1ms base backoff must not hang; generous ceiling for CI.
	assert.Less(t, time.Since(start), 10*time.Second)
}

func TestOpen_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // already cancelled: Open must honor it and not spin the full backoff

	cfg := forgeredis.DefaultConfig()
	cfg.Addresses = []string{"127.0.0.1:1"}
	cfg.RetryAttempts = 50
	cfg.RetryInterval = time.Second // huge base; if ctx were ignored this would hang
	cfg.DialTimeout = 50 * time.Millisecond

	start := time.Now()
	c, err := forgeredis.Open(ctx, forgeredis.WithConfig(cfg))
	require.Error(t, err)
	assert.Nil(t, c)
	assert.Less(t, time.Since(start), 5*time.Second, "Open must abort promptly on a cancelled ctx")
}
```

- [ ] **Step 3: Run tests to verify they fail**
Run: `go test -race ./redis/ -run 'TestOpen|TestBuildOptions' -v`
Expected: FAIL (`forgeredis.Open` / `forgeredis.WithUniversalOptions` undefined)

- [ ] **Step 4: Write the implementation**

`redis/options.go`:
```go
package redis

import (
	"fmt"
	"log/slog"

	goredis "github.com/redis/go-redis/v9"
)

// config holds the resolved settings for one Open call. The embedded Config carries
// serializable data; the remaining fields are non-serializable code values.
type config struct {
	logger           *slog.Logger
	universalOptions func(*goredis.UniversalOptions)
	errs             []error
	Config
}

// Option configures Open. Invalid values accumulate in the config and are returned
// (joined, ErrInvalidConfig-wrapped) by Open before any network I/O.
type Option func(*config)

// WithConfig sets the whole serializable data block at once. Build the argument from
// DefaultConfig() (or an env-parsed copy); a bare Config{} zeroes the timeouts and
// fails Validate. Options apply in order — place WithConfig first if later
// convenience options should take precedence.
func WithConfig(cfg Config) Option {
	return func(c *config) { c.Config = cfg }
}

// WithLogger sets the slog.Logger used by Close for its lifecycle line. A nil logger
// is rejected (ErrInvalidConfig); pass a discard logger to silence logging instead.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithLogger received a nil *slog.Logger", ErrInvalidConfig))
			return
		}
		c.logger = l
	}
}

// WithUniversalOptions registers an escape hatch that runs LAST in Open, after the
// Config overlay, on the fully-built *goredis.UniversalOptions. Use it for anything
// the serializable fields don't cover — TLSConfig, OnConnect, a custom Dialer. A nil
// func is rejected (ErrInvalidConfig).
func WithUniversalOptions(fn func(*goredis.UniversalOptions)) Option {
	return func(c *config) {
		if fn == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithUniversalOptions received a nil func", ErrInvalidConfig))
			return
		}
		c.universalOptions = fn
	}
}

// buildOptions maps a validated Config onto a *goredis.UniversalOptions. It is a pure
// function (no I/O), so the Config -> options mapping is unit-testable without a
// server. goredis.NewUniversalClient then selects the topology from the result:
// a single Addr with no MasterName -> standalone, multiple Addrs -> cluster, a
// non-empty MasterName -> sentinel/failover.
func buildOptions(cfg Config) *goredis.UniversalOptions {
	return &goredis.UniversalOptions{
		Addrs:           cfg.Addresses,
		MasterName:      cfg.MasterName,
		Username:        cfg.Username,
		Password:        cfg.Password,
		DB:              cfg.DB,
		PoolSize:        cfg.PoolSize,
		MinIdleConns:    cfg.MinIdleConns,
		DialTimeout:     cfg.DialTimeout,
		ReadTimeout:     cfg.ReadTimeout,
		WriteTimeout:    cfg.WriteTimeout,
		ConnMaxIdleTime: cfg.ConnMaxIdleTime,
	}
}
```

`redis/redis.go`:
```go
package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// maxConnectBackoff caps the exponential backoff between connect attempts so a large
// RetryInterval or RetryAttempts cannot produce an unbounded wait.
const maxConnectBackoff = 30 * time.Second

// Open builds a Redis (or Valkey) client from options and returns it only once a PING
// has confirmed a live server. It starts from DefaultConfig, applies the options in
// order, surfaces any accumulated option errors and a failed Validate as an
// ErrInvalidConfig-wrapped error (before any network I/O), maps the Config onto
// *goredis.UniversalOptions, runs the WithUniversalOptions escape hatch LAST,
// constructs a topology-appropriate client via goredis.NewUniversalClient, then pings
// with bounded retry/backoff. On failure it closes the partially-opened client and
// returns an ErrConnect-wrapped, single-line error — leaking nothing.
//
// The returned value is the goredis.UniversalClient interface; *goredis.Client,
// *goredis.ClusterClient, and *goredis.FailoverClient all satisfy it.
func Open(ctx context.Context, opts ...Option) (goredis.UniversalClient, error) {
	c := &config{Config: DefaultConfig()}
	for _, opt := range opts {
		opt(c)
	}
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}

	uopts := buildOptions(c.Config)
	if c.universalOptions != nil {
		c.universalOptions(uopts) // escape hatch runs LAST, on the fully-built options
	}

	client := goredis.NewUniversalClient(uopts)
	if err := pingWithRetry(ctx, client, c.Config); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

// pingWithRetry pings the server up to RetryAttempts times, waiting
// RetryInterval·2^attempt (capped at maxConnectBackoff) between tries and honoring
// ctx cancellation during the wait. RetryAttempts <= 1 means a single attempt with no
// wait. After exhausting attempts it returns ErrConnect joined with the last error.
func pingWithRetry(ctx context.Context, client goredis.UniversalClient, cfg Config) error {
	attempts := cfg.RetryAttempts
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%w: %v", ErrConnect, err)
		}
		if err := client.Ping(ctx).Err(); err != nil {
			lastErr = err
		} else {
			return nil
		}

		// No wait after the final attempt.
		if attempt == attempts-1 {
			break
		}
		wait := backoff(cfg.RetryInterval, attempt)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("%w: %v", ErrConnect, ctx.Err())
		case <-timer.C:
		}
	}
	return fmt.Errorf("%w: %v", ErrConnect, lastErr)
}

// backoff returns base·2^attempt, capped at maxConnectBackoff. A non-positive base
// yields no wait.
func backoff(base time.Duration, attempt int) time.Duration {
	if base <= 0 {
		return 0
	}
	d := base << attempt // base · 2^attempt
	// Guard the shift overflowing into a negative duration on large attempt counts.
	if d <= 0 || d > maxConnectBackoff {
		return maxConnectBackoff
	}
	return d
}
```
- [ ] **Step 5: Run tests to verify they pass**
Run: `just test ./redis/...`
Expected: PASS
- [ ] **Step 6: Commit**
```bash
git add go.mod go.sum redis/ && git commit -m "feat(redis): add Open with topology selection, options, and connect-retry"
```

---

### Task RD-3: `Close` and `Healthcheck`

**Files:**
- Create: `redis/lifecycle.go`
- Test: `redis/lifecycle_test.go`

**Interfaces:**
- Produces: `Close(c redis.UniversalClient, log *slog.Logger)`; `Healthcheck(c redis.UniversalClient) func(context.Context) error`.
- Depends on: RD-2 (`Open`), RD-1 (`ErrHealthcheck`). Test helper `slogDiscard()` is defined in `redis/open_test.go` (RD-2).

- [ ] **Step 1: Write the failing test**

`redis/lifecycle_test.go`:
```go
package redis_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgeredis "github.com/dmitrymomot/forge/redis"
)

func TestClose_NilTolerant(t *testing.T) {
	// Close must not panic on a nil client and/or a nil logger; it is a defer helper
	// in main and has to be defensive on every shutdown path.
	assert.NotPanics(t, func() { forgeredis.Close(nil, nil) })
	assert.NotPanics(t, func() { forgeredis.Close(nil, slogDiscard()) })
}

func TestHealthcheck_OK(t *testing.T) {
	addr := os.Getenv("FORGE_TEST_REDIS_URL")
	if addr == "" {
		t.Skip("set FORGE_TEST_REDIS_URL (host:port) to run the redis integration test")
	}
	cfg := forgeredis.DefaultConfig()
	cfg.Addresses = []string{addr}

	c, err := forgeredis.Open(t.Context(), forgeredis.WithConfig(cfg))
	require.NoError(t, err)
	defer forgeredis.Close(c, slogDiscard())

	probe := forgeredis.Healthcheck(c)
	require.NotNil(t, probe)
	require.NoError(t, probe(t.Context()), "a live server must pass the healthcheck")
}
```
- [ ] **Step 2: Run test to verify it fails**
Run: `go test -race ./redis/ -run TestClose_NilTolerant -v`
Expected: FAIL (`forgeredis.Close` undefined)
- [ ] **Step 3: Write the implementation**

`redis/lifecycle.go`:
```go
package redis

import (
	"context"
	"fmt"
	"log/slog"

	goredis "github.com/redis/go-redis/v9"
)

// Close logs a single "closing redis client" line (when log is non-nil) and closes
// the client. It is the defer helper used in main — `defer Close(client, logger)` —
// so it runs after supervisor.Run returns, once in-flight work has drained. It takes
// no context because the driver's Close is synchronous. A nil client and/or a nil
// logger are tolerated: the log line is skipped and no close is attempted on nil.
func Close(c goredis.UniversalClient, log *slog.Logger) {
	if c == nil {
		return
	}
	if err := c.Close(); err != nil {
		if log != nil {
			log.Error("redis client close failed", "err", err)
		}
		return
	}
	if log != nil {
		log.Info("closing redis client")
	}
}

// Healthcheck returns a stateless closure that PINGs the server, wrapping any failure
// in ErrHealthcheck. The closure has the exact func(context.Context) error shape a
// readiness/liveness probe wants; hand it to the app's /readyz handler. It is safe to
// call on every probe.
func Healthcheck(c goredis.UniversalClient) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := c.Ping(ctx).Err(); err != nil {
			return fmt.Errorf("%w: %v", ErrHealthcheck, err)
		}
		return nil
	}
}
```
- [ ] **Step 4: Run tests to verify they pass**
Run: `just test ./redis/...`
Expected: PASS (the env-gated `TestHealthcheck_OK` is skipped unless `FORGE_TEST_REDIS_URL` is set)
- [ ] **Step 5: Commit**
```bash
git add redis/ && git commit -m "feat(redis): add Close and Healthcheck lifecycle helpers"
```

---

### Task RD-4: `IsNil` predicate and `GetJSON` / `SetJSON` typed conveniences

**Files:**
- Create: `redis/json.go`
- Test: `redis/json_test.go`

**Interfaces:**
- Produces: `IsNil(err error) bool`; `GetJSON[T any](ctx, c redis.Cmdable, key string) (T, error)`; `SetJSON(ctx, c redis.Cmdable, key string, v any, ttl time.Duration) error`.
- Depends on: RD-2 (`Open`), RD-3 (`Close`).

- [ ] **Step 1: Write the failing test**

`redis/json_test.go`:
```go
package redis_test

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgeredis "github.com/dmitrymomot/forge/redis"
	goredis "github.com/redis/go-redis/v9"
)

func TestIsNil(t *testing.T) {
	// goredis.Nil is the cache-miss sentinel; IsNil must recognize it, including when
	// it has been wrapped, and reject unrelated errors.
	assert.True(t, forgeredis.IsNil(goredis.Nil))
	assert.True(t, forgeredis.IsNil(fmt.Errorf("get failed: %w", goredis.Nil)))
	assert.False(t, forgeredis.IsNil(nil))
	assert.False(t, forgeredis.IsNil(errors.New("some other error")))
}

type jsonValue struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestGetSetJSON_RoundTrip(t *testing.T) {
	addr := os.Getenv("FORGE_TEST_REDIS_URL")
	if addr == "" {
		t.Skip("set FORGE_TEST_REDIS_URL (host:port) to run the redis integration test")
	}
	cfg := forgeredis.DefaultConfig()
	cfg.Addresses = []string{addr}

	c, err := forgeredis.Open(t.Context(), forgeredis.WithConfig(cfg))
	require.NoError(t, err)
	defer forgeredis.Close(c, slogDiscard())

	key := fmt.Sprintf("forge:test:json:%d", time.Now().UnixNano())
	t.Cleanup(func() { c.Del(t.Context(), key) })

	want := jsonValue{Name: "forge", Count: 7}
	require.NoError(t, forgeredis.SetJSON(t.Context(), c, key, want, time.Minute))

	got, err := forgeredis.GetJSON[jsonValue](t.Context(), c, key)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestGetJSON_Miss(t *testing.T) {
	addr := os.Getenv("FORGE_TEST_REDIS_URL")
	if addr == "" {
		t.Skip("set FORGE_TEST_REDIS_URL (host:port) to run the redis integration test")
	}
	cfg := forgeredis.DefaultConfig()
	cfg.Addresses = []string{addr}

	c, err := forgeredis.Open(t.Context(), forgeredis.WithConfig(cfg))
	require.NoError(t, err)
	defer forgeredis.Close(c, slogDiscard())

	missingKey := fmt.Sprintf("forge:test:missing:%d", time.Now().UnixNano())
	got, err := forgeredis.GetJSON[jsonValue](t.Context(), c, missingKey)
	require.Error(t, err)
	assert.True(t, forgeredis.IsNil(err), "a missing key must surface as a goredis.Nil miss")
	assert.Equal(t, jsonValue{}, got, "a miss returns the zero value of T")
}
```
- [ ] **Step 2: Run test to verify it fails**
Run: `go test -race ./redis/ -run TestIsNil -v`
Expected: FAIL (`forgeredis.IsNil` undefined)
- [ ] **Step 3: Write the implementation**

`redis/json.go`:
```go
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// IsNil reports whether err is (or wraps) goredis.Nil, the sentinel go-redis returns
// for a key that does not exist — a cache miss, not a failure. App code branches with
// IsNil instead of importing the driver to compare against goredis.Nil.
func IsNil(err error) bool {
	return errors.Is(err, goredis.Nil)
}

// GetJSON fetches key and json.Unmarshals it into a T. On a cache miss it returns the
// zero T together with the goredis.Nil error, so callers can branch with IsNil(err);
// any other Get or Unmarshal failure is returned with the zero T. It operates over
// goredis.Cmdable, so it works against a client, a pipeline, or a transaction.
func GetJSON[T any](ctx context.Context, c goredis.Cmdable, key string) (T, error) {
	var v T
	b, err := c.Get(ctx, key).Bytes()
	if err != nil {
		return v, err // goredis.Nil on a miss; the driver error otherwise
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return v, err
	}
	return v, nil
}

// SetJSON json.Marshals v and stores it at key with the given ttl (0 = no expiry,
// per go-redis Set semantics). It operates over goredis.Cmdable, so it works against
// a client, a pipeline, or a transaction.
func SetJSON(ctx context.Context, c goredis.Cmdable, key string, v any, ttl time.Duration) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.Set(ctx, key, b, ttl).Err()
}
```
- [ ] **Step 4: Run tests to verify they pass**
Run: `just test ./redis/...`
Expected: PASS (`TestIsNil` runs always; the round-trip and miss tests are skipped unless `FORGE_TEST_REDIS_URL` is set)
- [ ] **Step 5: Commit**
```bash
git add redis/ && git commit -m "feat(redis): add IsNil predicate and GetJSON/SetJSON conveniences"
```

---

### Task RD-5: Package documentation (`doc.go`) and lint

**Files:**
- Create: `redis/doc.go`

**Interfaces:**
- Produces: package-level godoc (env-var table + runnable example), in the `supervisor/doc.go` style.
- Depends on: RD-1…RD-4 (documents the full surface).

- [ ] **Step 1: Write the implementation**

`redis/doc.go`:
```go
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
```
- [ ] **Step 2: Run the full package test suite**
Run: `just test ./redis/...`
Expected: PASS (doc.go adds no behavior; this confirms the package still builds and tests pass)
- [ ] **Step 3: Lint**
Run: `just lint`
Expected: PASS (no findings in `redis/`)
- [ ] **Step 4: Commit**
```bash
git add redis/ && git commit -m "docs(redis): add package doc with env-var table and example"
```
## Package: `mongo`

> MongoDB official driver **v2** (`go.mongodb.org/mongo-driver/v2`), pinned to `v2.7.0`,
> targeting MongoDB server 8.x. The forge package is itself named `mongo`, so **inside the
> package the driver is always imported under the alias `mongodriver`**
> (`mongodriver "go.mongodb.org/mongo-driver/v2/mongo"`); the sub-packages
> `.../mongo/options`, `.../mongo/readpref`, `.../mongo/readconcern`,
> `.../mongo/writeconcern`, and `go.mongodb.org/mongo-driver/v2/bson` keep their natural
> names. SPEC-facing signatures below write the return type as `*mongo.Client` for
> readability — in the actual `.go` files that is `*mongodriver.Client`. Black-box tests use
> `package mongo_test` and import the forge package aliased `forgemongo` and the driver
> aliased `mongodriver`.
>
> Verified v2.7.0 API (do not deviate): `mongodriver.Connect(opts ...*options.ClientOptions) (*mongodriver.Client, error)` (**no `ctx` argument**) · `options.Client() *options.ClientOptions` (chainable `ApplyURI`/`SetMaxPoolSize`/`SetMinPoolSize`/`SetConnectTimeout`/`SetServerSelectionTimeout`/`SetMaxConnIdleTime`/`SetReadPreference`/`SetReadConcern`/`SetWriteConcern`) · `(*mongodriver.Client).Ping(ctx, *readpref.ReadPref) error` · `(*mongodriver.Client).Disconnect(ctx) error` · `(*mongodriver.Client).StartSession() (*mongodriver.Session, error)` · `(*mongodriver.Session).WithTransaction(ctx, fn func(ctx) (any, error), opts...) (any, error)` (**fn returns `(any, error)`**) · `(*mongodriver.Session).EndSession(ctx)` · `(*mongodriver.Collection).Indexes() mongodriver.IndexView` · `(mongodriver.IndexView).CreateMany(ctx, []mongodriver.IndexModel) ([]string, error)` · `mongodriver.IndexModel{Keys any; Options *options.IndexOptionsBuilder}` · `(*mongodriver.Database).RunCommand(ctx, cmd any) *mongodriver.SingleResult` (`.Err()`) · sentinel `mongodriver.ErrNoDocuments` · classification helper `mongodriver.IsDuplicateKeyError(err) bool` · error types `mongodriver.CommandError{Code int32}`, `mongodriver.WriteException{WriteErrors mongodriver.WriteErrors}`, `mongodriver.WriteError{Code int}`, `mongodriver.BulkWriteException{WriteErrors []mongodriver.BulkWriteError}`. `readpref.Primary()` etc.; `readconcern.Local()/Majority()/Snapshot()/Available()/Linearizable()`; `writeconcern.Majority()/Journaled()/Unacknowledged()` plus `&writeconcern.WriteConcern{W: <int|"majority">}`.

### Task MG-1: Config, DefaultConfig, Validate, errors, concern parsers

**Files:**
- Create: `mongo/errors.go`
- Create: `mongo/config.go`
- Test: `mongo/config_test.go`

**Interfaces:**
- Produces: `mongo.Config`; `DefaultConfig() Config`; `(Config) Validate() error`; sentinels `ErrInvalidConfig`/`ErrConnect`/`ErrHealthcheck`; unexported pure parsers `parseReadPreference`/`parseReadConcern`/`parseWriteConcern`.

- [ ] **Step 0: Add the driver dependency** (required before this task's `config.go`, which imports the driver's readconcern/readpref/writeconcern subpackages)
Run:
```bash
go get go.mongodb.org/mongo-driver/v2@v2.7.0
```
Expected: `go.mod`/`go.sum` updated with `go.mongodb.org/mongo-driver/v2 v2.7.0`. Include `go.mod`/`go.sum` in this task's commit.

- [ ] **Step 1: Write the failing test**
```go
package mongo_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgemongo "github.com/dmitrymomot/forge/mongo"
)

func TestDefaultConfig(t *testing.T) {
	cfg := forgemongo.DefaultConfig()
	assert.Empty(t, cfg.URI, "URI has no default; it is required")
	assert.Empty(t, cfg.Database)
	assert.Equal(t, uint64(100), cfg.MaxPoolSize)
	assert.Equal(t, uint64(0), cfg.MinPoolSize)
	assert.Equal(t, 10*time.Second, cfg.ConnectTimeout)
	assert.Equal(t, 10*time.Second, cfg.ServerSelectionTimeout)
	assert.Equal(t, time.Duration(0), cfg.MaxConnIdleTime)
	assert.Empty(t, cfg.ReadPreference)
	assert.Empty(t, cfg.ReadConcern)
	assert.Empty(t, cfg.WriteConcern)
	assert.Equal(t, 3, cfg.RetryAttempts)
	assert.Equal(t, time.Second, cfg.RetryInterval)

	// DefaultConfig alone is not usable: URI is required.
	require.ErrorIs(t, cfg.Validate(), forgemongo.ErrInvalidConfig)

	// With a URI it validates (empty concerns => driver defaults).
	cfg.URI = "mongodb://127.0.0.1:27017"
	require.NoError(t, cfg.Validate())
}

func TestConfig_Validate(t *testing.T) {
	base := func() forgemongo.Config {
		c := forgemongo.DefaultConfig()
		c.URI = "mongodb://127.0.0.1:27017"
		return c
	}

	t.Run("empty URI", func(t *testing.T) {
		c := base()
		c.URI = ""
		require.ErrorIs(t, c.Validate(), forgemongo.ErrInvalidConfig)
	})
	t.Run("unknown ReadPreference", func(t *testing.T) {
		c := base()
		c.ReadPreference = "bogus"
		require.ErrorIs(t, c.Validate(), forgemongo.ErrInvalidConfig)
	})
	t.Run("unknown ReadConcern", func(t *testing.T) {
		c := base()
		c.ReadConcern = "bogus"
		require.ErrorIs(t, c.Validate(), forgemongo.ErrInvalidConfig)
	})
	t.Run("unknown WriteConcern", func(t *testing.T) {
		c := base()
		c.WriteConcern = "bogus"
		require.ErrorIs(t, c.Validate(), forgemongo.ErrInvalidConfig)
	})
	t.Run("negative RetryAttempts", func(t *testing.T) {
		c := base()
		c.RetryAttempts = -1
		require.ErrorIs(t, c.Validate(), forgemongo.ErrInvalidConfig)
	})
	t.Run("negative durations rejected", func(t *testing.T) {
		c := base()
		c.ConnectTimeout = -1
		require.ErrorIs(t, c.Validate(), forgemongo.ErrInvalidConfig)
	})

	t.Run("valid concern names accepted", func(t *testing.T) {
		c := base()
		c.ReadPreference = "secondaryPreferred"
		c.ReadConcern = "majority"
		c.WriteConcern = "majority"
		require.NoError(t, c.Validate())
	})
	t.Run("numeric WriteConcern accepted", func(t *testing.T) {
		c := base()
		c.WriteConcern = "2"
		require.NoError(t, c.Validate())
	})
}

func TestConfig_EnvTags(t *testing.T) {
	want := map[string]string{
		"URI":                    "URI",
		"Database":               "DATABASE",
		"MaxPoolSize":            "MAX_POOL_SIZE",
		"MinPoolSize":            "MIN_POOL_SIZE",
		"ConnectTimeout":         "CONNECT_TIMEOUT",
		"ServerSelectionTimeout": "SERVER_SELECTION_TIMEOUT",
		"MaxConnIdleTime":        "MAX_CONN_IDLE_TIME",
		"ReadPreference":         "READ_PREFERENCE",
		"ReadConcern":            "READ_CONCERN",
		"WriteConcern":           "WRITE_CONCERN",
		"RetryAttempts":          "RETRY_ATTEMPTS",
		"RetryInterval":          "RETRY_INTERVAL",
	}
	typ := reflect.TypeFor[forgemongo.Config]()
	for fname, tag := range want {
		f, ok := typ.FieldByName(fname)
		require.Truef(t, ok, "field %s missing", fname)
		assert.Equalf(t, tag, f.Tag.Get("env"), "field %s env tag", fname)
	}
}
```
- [ ] **Step 2: Run test to verify it fails**
Run: `go test -race ./mongo/ -run TestDefaultConfig -v`
Expected: FAIL (package/symbols undefined — `mongo` package does not compile yet)
- [ ] **Step 3: Write the implementation**

`mongo/errors.go`:
```go
package mongo

import "errors"

// Sentinel errors returned (often wrapped) by this package. Match with errors.Is.
// They are single-line and carry no embedded blobs; failures wrap them with the
// underlying driver error via fmt.Errorf("%w: %v", …) or errors.Join.
var (
	// ErrInvalidConfig is returned (joined) when an option or Config field has an invalid value.
	ErrInvalidConfig = errors.New("mongo: invalid config")
	// ErrConnect is returned when Open exhausts its connect/ping retries.
	ErrConnect = errors.New("mongo: connect failed")
	// ErrHealthcheck is returned by the Healthcheck closure when a ping fails.
	ErrHealthcheck = errors.New("mongo: healthcheck failed")
)
```

`mongo/config.go`:
```go
package mongo

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

// Config holds the serializable settings for a Mongo client. The env struct tags
// are inert strings — this package imports no config loader. Seed from
// DefaultConfig and parse the environment over it with whatever loader reads env
// tags. Defaults live solely in DefaultConfig (there are no envDefault tags to
// drift from it). Field order is subject to the repo's betteralign tooling.
type Config struct {
	URI                    string        `env:"URI"`                      // mongodb://… (required)
	Database               string        `env:"DATABASE"`                 // optional default db name
	ReadPreference         string        `env:"READ_PREFERENCE"`          // primary, primaryPreferred, secondary, secondaryPreferred, nearest
	ReadConcern            string        `env:"READ_CONCERN"`             // local, majority, available, linearizable, snapshot
	WriteConcern           string        `env:"WRITE_CONCERN"`            // majority, journaled, unacknowledged, or a w-number ("1", "2", …)
	MaxPoolSize            uint64        `env:"MAX_POOL_SIZE"`
	MinPoolSize            uint64        `env:"MIN_POOL_SIZE"`
	ConnectTimeout         time.Duration `env:"CONNECT_TIMEOUT"`
	ServerSelectionTimeout time.Duration `env:"SERVER_SELECTION_TIMEOUT"`
	MaxConnIdleTime        time.Duration `env:"MAX_CONN_IDLE_TIME"`
	RetryInterval          time.Duration `env:"RETRY_INTERVAL"`
	RetryAttempts          int           `env:"RETRY_ATTEMPTS"`
}

// DefaultConfig returns production-sane defaults and is the single source of truth
// for them. URI has no default — it is required, so DefaultConfig alone fails
// Validate. Empty concern strings mean "use the driver default" (no override).
func DefaultConfig() Config {
	return Config{
		MaxPoolSize:            100,
		MinPoolSize:            0,
		ConnectTimeout:         10 * time.Second,
		ServerSelectionTimeout: 10 * time.Second,
		MaxConnIdleTime:        0,
		RetryAttempts:          3,
		RetryInterval:          time.Second,
	}
}

// Validate reports whether the field values are usable, returning an
// ErrInvalidConfig-wrapped, single-line joined error otherwise. Open calls it
// defensively; callers may call it after env-loading. Unknown concern strings are
// rejected here by deferring to the same pure parsers Open uses.
func (c Config) Validate() error {
	var errs []error
	if c.URI == "" {
		errs = append(errs, fmt.Errorf("%w: URI must not be empty", ErrInvalidConfig))
	}
	for _, f := range []struct {
		name string
		d    time.Duration
	}{
		{"ConnectTimeout", c.ConnectTimeout},
		{"ServerSelectionTimeout", c.ServerSelectionTimeout},
		{"MaxConnIdleTime", c.MaxConnIdleTime},
		{"RetryInterval", c.RetryInterval},
	} {
		if f.d < 0 {
			errs = append(errs, fmt.Errorf("%w: %s must be >= 0", ErrInvalidConfig, f.name))
		}
	}
	if c.RetryAttempts < 0 {
		errs = append(errs, fmt.Errorf("%w: RetryAttempts must be >= 0", ErrInvalidConfig))
	}
	if _, err := parseReadPreference(c.ReadPreference); err != nil {
		errs = append(errs, fmt.Errorf("%w: %v", ErrInvalidConfig, err))
	}
	if _, err := parseReadConcern(c.ReadConcern); err != nil {
		errs = append(errs, fmt.Errorf("%w: %v", ErrInvalidConfig, err))
	}
	if _, err := parseWriteConcern(c.WriteConcern); err != nil {
		errs = append(errs, fmt.Errorf("%w: %v", ErrInvalidConfig, err))
	}
	return errors.Join(errs...)
}

// parseReadPreference maps a Config string to a driver read preference. An empty
// string yields (nil, nil): leave the driver default in place. An unknown value is
// an error. Pure and server-free so Validate and tests can call it directly.
func parseReadPreference(s string) (*readpref.ReadPref, error) {
	switch s {
	case "":
		return nil, nil
	case "primary":
		return readpref.Primary(), nil
	case "primaryPreferred":
		return readpref.PrimaryPreferred(), nil
	case "secondary":
		return readpref.Secondary(), nil
	case "secondaryPreferred":
		return readpref.SecondaryPreferred(), nil
	case "nearest":
		return readpref.Nearest(), nil
	default:
		return nil, fmt.Errorf("unknown ReadPreference %q", s)
	}
}

// parseReadConcern maps a Config string to a driver read concern. Empty => (nil,
// nil) (driver default); unknown => error.
func parseReadConcern(s string) (*readconcern.ReadConcern, error) {
	switch s {
	case "":
		return nil, nil
	case "local":
		return readconcern.Local(), nil
	case "majority":
		return readconcern.Majority(), nil
	case "available":
		return readconcern.Available(), nil
	case "linearizable":
		return readconcern.Linearizable(), nil
	case "snapshot":
		return readconcern.Snapshot(), nil
	default:
		return nil, fmt.Errorf("unknown ReadConcern %q", s)
	}
}

// parseWriteConcern maps a Config string to a driver write concern. Empty => (nil,
// nil) (driver default). Named concerns (majority, journaled, unacknowledged) and
// a plain w-number (e.g. "1", "2") are accepted; anything else is an error.
func parseWriteConcern(s string) (*writeconcern.WriteConcern, error) {
	switch s {
	case "":
		return nil, nil
	case "majority":
		return writeconcern.Majority(), nil
	case "journaled":
		return writeconcern.Journaled(), nil
	case "unacknowledged":
		return writeconcern.Unacknowledged(), nil
	default:
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			return &writeconcern.WriteConcern{W: n}, nil
		}
		return nil, fmt.Errorf("unknown WriteConcern %q", s)
	}
}
```
- [ ] **Step 4: Add the pure-parser unit test**

Append to `mongo/config_test.go`:
```go
func TestParseConcerns(t *testing.T) {
	// The parsers are unexported; assert their behavior through Validate, which is
	// their only caller and surfaces every rejection as ErrInvalidConfig. Empty
	// strings must be accepted (driver default), valid names accepted, junk rejected.
	withURI := func(mut func(*forgemongo.Config)) forgemongo.Config {
		c := forgemongo.DefaultConfig()
		c.URI = "mongodb://127.0.0.1:27017"
		mut(&c)
		return c
	}

	// Empty concerns are valid (driver defaults).
	require.NoError(t, withURI(func(*forgemongo.Config) {}).Validate())

	readPrefs := []string{"primary", "primaryPreferred", "secondary", "secondaryPreferred", "nearest"}
	for _, rp := range readPrefs {
		require.NoErrorf(t, withURI(func(c *forgemongo.Config) { c.ReadPreference = rp }).Validate(), "ReadPreference %q", rp)
	}
	readConcerns := []string{"local", "majority", "available", "linearizable", "snapshot"}
	for _, rc := range readConcerns {
		require.NoErrorf(t, withURI(func(c *forgemongo.Config) { c.ReadConcern = rc }).Validate(), "ReadConcern %q", rc)
	}
	writeConcerns := []string{"majority", "journaled", "unacknowledged", "0", "1", "2"}
	for _, wc := range writeConcerns {
		require.NoErrorf(t, withURI(func(c *forgemongo.Config) { c.WriteConcern = wc }).Validate(), "WriteConcern %q", wc)
	}

	// Junk is rejected for each concern.
	require.ErrorIs(t, withURI(func(c *forgemongo.Config) { c.ReadPreference = "nope" }).Validate(), forgemongo.ErrInvalidConfig)
	require.ErrorIs(t, withURI(func(c *forgemongo.Config) { c.ReadConcern = "nope" }).Validate(), forgemongo.ErrInvalidConfig)
	require.ErrorIs(t, withURI(func(c *forgemongo.Config) { c.WriteConcern = "nope" }).Validate(), forgemongo.ErrInvalidConfig)
	require.ErrorIs(t, withURI(func(c *forgemongo.Config) { c.WriteConcern = "-1" }).Validate(), forgemongo.ErrInvalidConfig)
}
```
- [ ] **Step 5: Run tests to verify they pass**
Run: `just test ./mongo/...`
Expected: PASS
- [ ] **Step 6: Commit**
```bash
git add go.mod go.sum mongo/ && git commit -m "feat(mongo): add Config, DefaultConfig, Validate, concern parsers, sentinels"
```

---

### Task MG-2: dependency, options, and `Open` (connect-retry)

**Files:**
- Create: `mongo/options.go`
- Create: `mongo/mongo.go`
- Test: `mongo/options_test.go`
- Test: `mongo/open_test.go`

**Interfaces:**
- Produces: internal `config` struct + `type Option func(*config)`; `WithConfig`/`WithLogger`/`WithClientOptions`; `Open(ctx, opts...) (*mongo.Client, error)`.

The driver dependency (`go.mongodb.org/mongo-driver/v2@v2.7.0`) was already added in MG-1 Step 0; no `go get` is needed here.

- [ ] **Step 2: Write the failing tests**

`mongo/options_test.go`:
```go
package mongo_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgemongo "github.com/dmitrymomot/forge/mongo"
)

func TestOpen_NilOptionRejected(t *testing.T) {
	// Nil function/pointer arguments accumulate an ErrInvalidConfig and are surfaced
	// by Open before any connection attempt. A valid URI is supplied so the failure
	// is unambiguously the option's rejection, not a missing-URI validation error.
	cfg := forgemongo.DefaultConfig()
	cfg.URI = "mongodb://127.0.0.1:27017"

	opts := map[string]forgemongo.Option{
		"clientoptions": forgemongo.WithClientOptions(nil),
		"logger":        forgemongo.WithLogger(nil),
	}
	for name, opt := range opts {
		t.Run(name, func(t *testing.T) {
			c, err := forgemongo.Open(t.Context(), forgemongo.WithConfig(cfg), opt)
			require.Error(t, err)
			assert.Nil(t, c)
			assert.ErrorIs(t, err, forgemongo.ErrInvalidConfig)
		})
	}
}

func TestOpen_MissingURIFailsValidate(t *testing.T) {
	// Omitting WithConfig runs on pure DefaultConfig(), whose empty URI fails Validate.
	c, err := forgemongo.Open(t.Context())
	require.Error(t, err)
	assert.Nil(t, c)
	assert.ErrorIs(t, err, forgemongo.ErrInvalidConfig)
}
```

`mongo/open_test.go`:
```go
package mongo_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgemongo "github.com/dmitrymomot/forge/mongo"
)

func TestOpen_RetryExhausted(t *testing.T) {
	// Point at an unreachable address with a tiny server-selection timeout so each
	// ping fails fast. With RetryAttempts=2 and a tiny interval the loop exhausts
	// quickly and returns ErrConnect (not ErrInvalidConfig). Bounded by a generous
	// test timeout to catch a hung loop.
	cfg := forgemongo.DefaultConfig()
	cfg.URI = "mongodb://127.0.0.1:1/?serverSelectionTimeoutMS=100"
	cfg.RetryAttempts = 2
	cfg.RetryInterval = 10 * time.Millisecond

	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := forgemongo.Open(t.Context(), forgemongo.WithConfig(cfg))
		assert.Nil(t, c)
		require.Error(t, err)
		assert.ErrorIs(t, err, forgemongo.ErrConnect)
	}()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Open did not return; retry loop likely hung")
	}
}

func TestOpen_ContextCancelled(t *testing.T) {
	// A pre-cancelled context aborts the retry loop promptly with a wrapped ErrConnect.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	cfg := forgemongo.DefaultConfig()
	cfg.URI = "mongodb://127.0.0.1:1/?serverSelectionTimeoutMS=100"
	cfg.RetryAttempts = 5
	cfg.RetryInterval = time.Second

	c, err := forgemongo.Open(ctx, forgemongo.WithConfig(cfg))
	assert.Nil(t, c)
	require.Error(t, err)
	assert.ErrorIs(t, err, forgemongo.ErrConnect)
}
```

- [ ] **Step 3: Run tests to verify they fail**
Run: `go test -race ./mongo/ -run TestOpen -v`
Expected: FAIL (`Open`, `WithClientOptions`, etc. undefined)

- [ ] **Step 4: Write the options**

`mongo/options.go`:
```go
package mongo

import (
	"fmt"
	"log/slog"

	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// config holds resolved settings for a single Open. The embedded Config carries
// serializable data; the remaining fields are non-serializable code values.
type config struct {
	logger        *slog.Logger
	clientOptions func(*options.ClientOptions)
	errs          []error
	Config
}

// Option configures Open. Invalid values accumulate and are returned by Open.
type Option func(*config)

// WithConfig sets the whole serializable data block at once. Build the argument
// from DefaultConfig() (or an env-parsed copy); a bare Config{} has an empty URI
// and fails Validate. Options apply in order — place WithConfig before any option
// you want to take precedence.
func WithConfig(cfg Config) Option {
	return func(c *config) { c.Config = cfg }
}

// WithLogger sets the slog.Logger used by Close for its lifecycle line. A nil
// logger is rejected (ErrInvalidConfig); pass slog.New(slog.DiscardHandler) to
// silence logging instead.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithLogger received a nil *slog.Logger", ErrInvalidConfig))
			return
		}
		c.logger = l
	}
}

// WithClientOptions is the native-driver escape hatch: it runs LAST in Open, after
// the Config-derived *options.ClientOptions have been built, so anything Config
// does not cover (TLS, monitors, custom dialer, auth) stays reachable. A nil func
// is rejected (ErrInvalidConfig).
func WithClientOptions(fn func(*options.ClientOptions)) Option {
	return func(c *config) {
		if fn == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithClientOptions received a nil func", ErrInvalidConfig))
			return
		}
		c.clientOptions = fn
	}
}
```

- [ ] **Step 5: Write `Open` and the connect-retry loop**

`mongo/mongo.go`:
```go
package mongo

import (
	"context"
	"errors"
	"fmt"
	"time"

	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

// maxRetryBackoff caps the exponential wait between connect attempts.
const maxRetryBackoff = 30 * time.Second

// Open turns a Config (typically env-loaded) into a live, pooled, health-checkable
// *mongo.Client: it applies options over DefaultConfig(), validates, builds the
// driver client options from Config (URI, pool limits, timeouts, read/write
// concerns), runs the WithClientOptions escape hatch last, connects, then pings
// with bounded exponential-backoff retry. On failure it disconnects any partially
// opened client and returns a sentinel-wrapped, single-line error. The caller owns
// the returned client and closes it with Close(client, logger) in main.
func Open(ctx context.Context, opts ...Option) (*mongodriver.Client, error) {
	c := config{Config: DefaultConfig()}
	for _, opt := range opts {
		opt(&c)
	}
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}

	clientOpts, err := buildClientOptions(c.Config)
	if err != nil {
		return nil, err // already ErrInvalidConfig-wrapped
	}
	if c.clientOptions != nil {
		c.clientOptions(clientOpts) // escape hatch runs LAST
	}

	client, err := connectWithRetry(ctx, c.Config, clientOpts)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// buildClientOptions assembles *options.ClientOptions from Config. The concern
// parsers are pure and were already accepted by Validate, but errors are still
// checked defensively. Empty concern strings leave the driver defaults untouched.
func buildClientOptions(cfg Config) (*options.ClientOptions, error) {
	o := options.Client().ApplyURI(cfg.URI)
	o.SetMaxPoolSize(cfg.MaxPoolSize)
	o.SetMinPoolSize(cfg.MinPoolSize)
	if cfg.ConnectTimeout > 0 {
		o.SetConnectTimeout(cfg.ConnectTimeout)
	}
	if cfg.ServerSelectionTimeout > 0 {
		o.SetServerSelectionTimeout(cfg.ServerSelectionTimeout)
	}
	if cfg.MaxConnIdleTime > 0 {
		o.SetMaxConnIdleTime(cfg.MaxConnIdleTime)
	}

	rp, err := parseReadPreference(cfg.ReadPreference)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}
	if rp != nil {
		o.SetReadPreference(rp)
	}
	rc, err := parseReadConcern(cfg.ReadConcern)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}
	if rc != nil {
		o.SetReadConcern(rc)
	}
	wc, err := parseWriteConcern(cfg.WriteConcern)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}
	if wc != nil {
		o.SetWriteConcern(wc)
	}
	return o, nil
}

// connectWithRetry connects and pings with bounded exponential backoff. Each
// attempt builds a fresh client (Connect does not dial; the Ping confirms a live
// server). On a failed ping the client is disconnected before retrying so nothing
// leaks. The wait is RetryInterval·2^attempt capped at maxRetryBackoff and honors
// ctx cancellation. After RetryAttempts it returns ErrConnect joined with the last
// driver error. RetryAttempts <= 1 means a single attempt with no wait.
func connectWithRetry(ctx context.Context, cfg Config, clientOpts *options.ClientOptions) (*mongodriver.Client, error) {
	attempts := cfg.RetryAttempts
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrConnect, err)
		}

		client, err := mongodriver.Connect(clientOpts)
		if err != nil {
			lastErr = err
		} else if err = client.Ping(ctx, readpref.Primary()); err != nil {
			lastErr = err
			// Disconnect the partially opened client under a short bounded context.
			disconnect(client)
		} else {
			return client, nil
		}

		// No wait after the final attempt.
		if attempt == attempts-1 {
			break
		}
		wait := backoff(cfg.RetryInterval, attempt)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("%w: %v", ErrConnect, ctx.Err())
		case <-timer.C:
		}
	}
	return nil, fmt.Errorf("%w: %v", ErrConnect, lastErr)
}

// backoff returns interval·2^attempt capped at maxRetryBackoff (and never below 0).
func backoff(interval time.Duration, attempt int) time.Duration {
	if interval <= 0 {
		return 0
	}
	wait := interval << attempt // interval * 2^attempt
	if wait <= 0 || wait > maxRetryBackoff {
		return maxRetryBackoff
	}
	return wait
}

// disconnect tears down a client under a short bounded context, ignoring errors
// (the caller is already on a failure path).
func disconnect(client *mongodriver.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = client.Disconnect(ctx)
}
```
- [ ] **Step 6: Run tests to verify they pass**
Run: `just test ./mongo/...`
Expected: PASS
- [ ] **Step 7: Commit**
```bash
git add go.mod go.sum mongo/ && git commit -m "feat(mongo): add driver dep, options, and Open with connect-retry"
```

---

### Task MG-3: `Close` and `Healthcheck`

**Files:**
- Modify: `mongo/mongo.go`
- Test: `mongo/lifecycle_test.go`

**Interfaces:**
- Produces: `Close(c *mongo.Client, log *slog.Logger)`; `Healthcheck(c *mongo.Client) func(context.Context) error`.

- [ ] **Step 1: Write the failing tests**

`mongo/lifecycle_test.go`:
```go
package mongo_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgemongo "github.com/dmitrymomot/forge/mongo"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
)

func TestClose_NilLoggerTolerated(t *testing.T) {
	// Close must tolerate a nil *mongo.Client and a nil logger without panicking;
	// the log line is simply skipped. This is the pure, server-free contract.
	assert.NotPanics(t, func() {
		forgemongo.Close(nil, nil)
	})
	assert.NotPanics(t, func() {
		forgemongo.Close(nil, slog.New(slog.DiscardHandler))
	})
}

func mongoURI(t *testing.T) string {
	t.Helper()
	uri := os.Getenv("FORGE_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("FORGE_TEST_MONGO_URI not set; skipping integration test")
	}
	return uri
}

func openTestClient(t *testing.T) *mongodriver.Client {
	t.Helper()
	cfg := forgemongo.DefaultConfig()
	cfg.URI = mongoURI(t)
	c, err := forgemongo.Open(t.Context(), forgemongo.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(func() { forgemongo.Close(c, nil) })
	return c
}

func TestHealthcheck_Integration(t *testing.T) {
	c := openTestClient(t)

	hc := forgemongo.Healthcheck(c)
	require.NotNil(t, hc)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.NoError(t, hc(ctx), "healthcheck against a live server must succeed")
}

func TestHealthcheck_FailsWrapped(t *testing.T) {
	c := openTestClient(t)
	// Close the client, then the healthcheck must fail with an ErrHealthcheck-wrapped
	// error rather than panicking.
	forgemongo.Close(c, nil)

	hc := forgemongo.Healthcheck(c)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	err := hc(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, forgemongo.ErrHealthcheck)
}
```
- [ ] **Step 2: Run tests to verify they fail**
Run: `go test -race ./mongo/ -run 'TestClose|TestHealthcheck' -v`
Expected: FAIL (`Close`, `Healthcheck` undefined). The integration tests skip without `FORGE_TEST_MONGO_URI`; `TestClose_NilLoggerTolerated` must compile-fail then pass.

- [ ] **Step 3: Write the implementation**

Append to `mongo/mongo.go` (and add `"log/slog"` to its imports):
```go
// Close logs a single "closing mongo client" line and disconnects the client
// under a short internal bounded context (5s), keeping the uniform no-ctx
// signature. Used as `defer Close(client, logger)` in main, so it runs after
// supervisor.Run returns — i.e. after every service has drained, the only point at
// which disconnecting is guaranteed not to race in-flight work. A nil logger is
// tolerated (the client still closes; the log line is skipped); a nil client is a
// no-op.
func Close(c *mongodriver.Client, log *slog.Logger) {
	if c == nil {
		return
	}
	if log != nil {
		log.Info("closing mongo client")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Disconnect(ctx); err != nil && log != nil {
		log.Warn("mongo client disconnect failed", "err", err)
	}
}

// Healthcheck returns a stateless closure that pings the primary, wrapping any
// failure in ErrHealthcheck. Its func(context.Context) error shape is exactly what
// a readiness/liveness probe wants; hand it to the app's /readyz handler. Safe to
// call on every probe.
func Healthcheck(c *mongodriver.Client) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := c.Ping(ctx, readpref.Primary()); err != nil {
			return fmt.Errorf("%w: %v", ErrHealthcheck, err)
		}
		return nil
	}
}
```
- [ ] **Step 4: Run tests to verify they pass**
Run: `just test ./mongo/...`
Expected: PASS (integration tests SKIP without `FORGE_TEST_MONGO_URI`; the nil-logger unit test passes)
- [ ] **Step 5: Commit**
```bash
git add mongo/ && git commit -m "feat(mongo): add Close and Healthcheck lifecycle helpers"
```

---

### Task MG-4: `WithTransaction`

**Files:**
- Create: `mongo/transaction.go`
- Test: `mongo/transaction_test.go`

**Interfaces:**
- Produces: `WithTransaction(ctx context.Context, c *mongo.Client, fn func(ctx context.Context) error) error`.

- [ ] **Step 1: Write the failing test**

`mongo/transaction_test.go`:
```go
package mongo_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgemongo "github.com/dmitrymomot/forge/mongo"
	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
)

// WithTransaction needs a replica set / mongos. Gate on a dedicated env var so a
// standalone FORGE_TEST_MONGO_URI does not fail the suite.
func replicaSetURI(t *testing.T) string {
	t.Helper()
	uri := os.Getenv("FORGE_TEST_MONGO_RS_URI")
	if uri == "" {
		t.Skip("FORGE_TEST_MONGO_RS_URI not set; WithTransaction needs a replica set")
	}
	return uri
}

func openRSClient(t *testing.T) *mongodriver.Client {
	t.Helper()
	cfg := forgemongo.DefaultConfig()
	cfg.URI = replicaSetURI(t)
	c, err := forgemongo.Open(t.Context(), forgemongo.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(func() { forgemongo.Close(c, nil) })
	return c
}

func TestWithTransaction_CommitsOnSuccess(t *testing.T) {
	c := openRSClient(t)
	coll := c.Database("forge_test").Collection("txn_commit")
	t.Cleanup(func() { _ = coll.Drop(context.Background()) })
	require.NoError(t, coll.Drop(t.Context()))

	err := forgemongo.WithTransaction(t.Context(), c, func(ctx context.Context) error {
		_, err := coll.InsertOne(ctx, bson.D{{Key: "k", Value: "v"}})
		return err
	})
	require.NoError(t, err)

	n, err := coll.CountDocuments(t.Context(), bson.D{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), n, "a committed insert must be visible")
}

func TestWithTransaction_AbortsOnError(t *testing.T) {
	c := openRSClient(t)
	coll := c.Database("forge_test").Collection("txn_abort")
	t.Cleanup(func() { _ = coll.Drop(context.Background()) })
	require.NoError(t, coll.Drop(t.Context()))

	sentinel := errors.New("boom")
	err := forgemongo.WithTransaction(t.Context(), c, func(ctx context.Context) error {
		if _, e := coll.InsertOne(ctx, bson.D{{Key: "k", Value: "v"}}); e != nil {
			return e
		}
		return sentinel // force an abort after the write
	})
	require.ErrorIs(t, err, sentinel, "fn's error must propagate")

	n, err := coll.CountDocuments(t.Context(), bson.D{})
	require.NoError(t, err)
	assert.Equal(t, int64(0), n, "an aborted insert must not be visible")
}
```

- [ ] **Step 2: Run test to verify it fails**
Run: `go test -race ./mongo/ -run TestWithTransaction -v`
Expected: FAIL to compile (`WithTransaction` undefined). Once it compiles, the tests SKIP without `FORGE_TEST_MONGO_RS_URI`.

- [ ] **Step 3: Write the implementation**

`mongo/transaction.go`:
```go
package mongo

import (
	"context"

	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
)

// WithTransaction runs fn inside a single multi-document transaction: it starts a
// session, calls the driver's Session.WithTransaction (which commits when fn
// returns nil and aborts when fn returns an error or panics), and ends the session.
// fn must perform its operations using the context passed to it — that context
// carries the session, so any collection call made with another context runs
// outside the transaction.
//
// This requires a replica set or a sharded (mongos) deployment; on a standalone
// server the driver returns its own error verbatim (transactions are unsupported
// there). The driver may run fn more than once on transient transaction errors, so
// fn must be idempotent in its own bookkeeping.
func WithTransaction(ctx context.Context, c *mongodriver.Client, fn func(ctx context.Context) error) error {
	sess, err := c.StartSession()
	if err != nil {
		return err
	}
	defer sess.EndSession(ctx)

	// The driver's WithTransaction callback returns (any, error); forge's fn returns
	// only an error, so the result value is unused (nil).
	_, err = sess.WithTransaction(ctx, func(sessCtx context.Context) (any, error) {
		return nil, fn(sessCtx)
	})
	return err
}
```
- [ ] **Step 4: Run tests to verify they pass**
Run: `just test ./mongo/...`
Expected: PASS (transaction tests SKIP without `FORGE_TEST_MONGO_RS_URI`)
- [ ] **Step 5: Commit**
```bash
git add mongo/ && git commit -m "feat(mongo): add WithTransaction helper (requires replica set)"
```

---

### Task MG-5: `EnsureIndexes`, `EnableSharding`, `ShardCollection`

**Files:**
- Create: `mongo/setup.go`
- Test: `mongo/setup_test.go`

**Interfaces:**
- Produces: `EnsureIndexes(ctx, db *mongo.Database, specs map[string][]mongo.IndexModel) error`; `EnableSharding(ctx, c *mongo.Client, db string) error`; `ShardCollection(ctx, c *mongo.Client, namespace string, key bson.D) error`.

- [ ] **Step 1: Write the failing test**

`mongo/setup_test.go`:
```go
package mongo_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgemongo "github.com/dmitrymomot/forge/mongo"
	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestEnsureIndexes_EmptySpecsNoOp(t *testing.T) {
	// Empty (and nil) specs must be a no-op that returns nil without touching the
	// server — so it is safe to call with a nil *mongo.Database in a unit test.
	require.NoError(t, forgemongo.EnsureIndexes(t.Context(), nil, nil))
	require.NoError(t, forgemongo.EnsureIndexes(t.Context(), nil, map[string][]mongodriver.IndexModel{}))
}

func TestEnsureIndexes_Integration(t *testing.T) {
	c := openTestClient(t) // from lifecycle_test.go (skips without FORGE_TEST_MONGO_URI)
	db := c.Database("forge_test")
	t.Cleanup(func() { _ = db.Collection("idx_users").Drop(context.Background()) })

	specs := map[string][]mongodriver.IndexModel{
		"idx_users": {
			{
				Keys:    bson.D{{Key: "email", Value: 1}},
				Options: options.Index().SetUnique(true).SetName("uniq_email"),
			},
			{
				Keys: bson.D{{Key: "created_at", Value: -1}},
			},
		},
	}

	// First run creates the indexes; second run is idempotent (CreateMany by spec).
	require.NoError(t, forgemongo.EnsureIndexes(t.Context(), db, specs))
	require.NoError(t, forgemongo.EnsureIndexes(t.Context(), db, specs), "re-running EnsureIndexes must be idempotent")

	cur, err := db.Collection("idx_users").Indexes().List(t.Context())
	require.NoError(t, err)
	var idx []bson.M
	require.NoError(t, cur.All(t.Context(), &idx))
	// _id_ + uniq_email + created_at index = 3.
	assert.Len(t, idx, 3)
}

func TestSharding_Integration(t *testing.T) {
	// Sharding commands require a mongos (sharded cluster). Gate on a dedicated var.
	uri := os.Getenv("FORGE_TEST_MONGO_SHARDED_URI")
	if uri == "" {
		t.Skip("FORGE_TEST_MONGO_SHARDED_URI not set; sharding needs a mongos")
	}
	cfg := forgemongo.DefaultConfig()
	cfg.URI = uri
	c, err := forgemongo.Open(t.Context(), forgemongo.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(func() { forgemongo.Close(c, nil) })

	require.NoError(t, forgemongo.EnableSharding(t.Context(), c, "forge_test"))
	require.NoError(t, forgemongo.ShardCollection(t.Context(), c, "forge_test.sharded", bson.D{{Key: "_id", Value: "hashed"}}))
}
```

- [ ] **Step 2: Run test to verify it fails**
Run: `go test -race ./mongo/ -run 'TestEnsureIndexes|TestSharding' -v`
Expected: FAIL to compile (`EnsureIndexes`/`EnableSharding`/`ShardCollection` undefined). Integration cases SKIP without their env vars; `TestEnsureIndexes_EmptySpecsNoOp` must pass.

- [ ] **Step 3: Write the implementation**

`mongo/setup.go`:
```go
package mongo

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
)

// EnsureIndexes creates the declared indexes per collection, idempotently. specs
// maps a collection name to its index models; for each entry it calls
// coll.Indexes().CreateMany, which the server treats as create-if-absent by index
// spec, so re-running EnsureIndexes is a safe no-op once the indexes exist. An
// empty or nil specs map is a no-op (db is not touched, so it may be nil). Intended
// to run once at boot, after Open.
func EnsureIndexes(ctx context.Context, db *mongodriver.Database, specs map[string][]mongodriver.IndexModel) error {
	if len(specs) == 0 {
		return nil
	}
	for name, models := range specs {
		if len(models) == 0 {
			continue
		}
		if _, err := db.Collection(name).Indexes().CreateMany(ctx, models); err != nil {
			return fmt.Errorf("mongo: ensure indexes for %q: %w", name, err)
		}
	}
	return nil
}

// EnableSharding enables sharding on a database via the admin "enableSharding"
// command. It requires a sharded (mongos) deployment; on a non-sharded server the
// driver's error is returned verbatim rather than masked.
func EnableSharding(ctx context.Context, c *mongodriver.Client, db string) error {
	cmd := bson.D{{Key: "enableSharding", Value: db}}
	if err := c.Database("admin").RunCommand(ctx, cmd).Err(); err != nil {
		return fmt.Errorf("mongo: enable sharding on %q: %w", db, err)
	}
	return nil
}

// ShardCollection shards a collection via the admin "shardCollection" command.
// namespace is the fully qualified "db.collection"; key is the shard-key spec
// (e.g. bson.D{{Key: "_id", Value: "hashed"}} or a ranged key). It requires a
// sharded (mongos) deployment; on a non-sharded server the driver's error is
// returned verbatim.
func ShardCollection(ctx context.Context, c *mongodriver.Client, namespace string, key bson.D) error {
	cmd := bson.D{
		{Key: "shardCollection", Value: namespace},
		{Key: "key", Value: key},
	}
	if err := c.Database("admin").RunCommand(ctx, cmd).Err(); err != nil {
		return fmt.Errorf("mongo: shard collection %q: %w", namespace, err)
	}
	return nil
}
```
- [ ] **Step 4: Run tests to verify they pass**
Run: `just test ./mongo/...`
Expected: PASS (sharding/index integration tests SKIP without their env vars; the empty-specs no-op passes)
- [ ] **Step 5: Commit**
```bash
git add mongo/ && git commit -m "feat(mongo): add EnsureIndexes, EnableSharding, ShardCollection"
```

---

### Task MG-6: `IsDuplicateKey` and `IsNotFound`

**Files:**
- Create: `mongo/classify.go`
- Test: `mongo/classify_test.go`

**Interfaces:**
- Produces: `IsDuplicateKey(err error) bool`; `IsNotFound(err error) bool`.

- [ ] **Step 1: Write the failing test**

`mongo/classify_test.go`:
```go
package mongo_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	forgemongo "github.com/dmitrymomot/forge/mongo"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
)

func TestIsDuplicateKey(t *testing.T) {
	// CommandError carries an int32 Code; WriteError/WriteException carry int Codes.
	// E11000 is the duplicate-key code in every form, including wrapped and inside a
	// BulkWriteException.
	cmdErr := mongodriver.CommandError{Code: 11000, Message: "E11000 duplicate key error"}
	writeExc := mongodriver.WriteException{
		WriteErrors: mongodriver.WriteErrors{{Code: 11000, Message: "E11000 duplicate key"}},
	}
	bulkExc := mongodriver.BulkWriteException{
		WriteErrors: []mongodriver.BulkWriteError{
			{WriteError: mongodriver.WriteError{Code: 11000, Message: "E11000 duplicate key"}},
		},
	}

	assert.True(t, forgemongo.IsDuplicateKey(cmdErr))
	assert.True(t, forgemongo.IsDuplicateKey(writeExc))
	assert.True(t, forgemongo.IsDuplicateKey(bulkExc))
	// Wrapped is still detected (errors.As traverses the chain).
	assert.True(t, forgemongo.IsDuplicateKey(fmt.Errorf("insert failed: %w", writeExc)))

	// Non-duplicate codes and unrelated errors are false.
	assert.False(t, forgemongo.IsDuplicateKey(mongodriver.CommandError{Code: 26}))
	assert.False(t, forgemongo.IsDuplicateKey(errors.New("nope")))
	assert.False(t, forgemongo.IsDuplicateKey(nil))
}

func TestIsNotFound(t *testing.T) {
	assert.True(t, forgemongo.IsNotFound(mongodriver.ErrNoDocuments))
	assert.True(t, forgemongo.IsNotFound(fmt.Errorf("decode: %w", mongodriver.ErrNoDocuments)))
	assert.False(t, forgemongo.IsNotFound(errors.New("other")))
	assert.False(t, forgemongo.IsNotFound(nil))
}
```
- [ ] **Step 2: Run test to verify it fails**
Run: `go test -race ./mongo/ -run 'TestIsDuplicateKey|TestIsNotFound' -v`
Expected: FAIL (`IsDuplicateKey`, `IsNotFound` undefined)

- [ ] **Step 3: Write the implementation**

`mongo/classify.go`:
```go
package mongo

import (
	"errors"

	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
)

// IsDuplicateKey reports whether err is a MongoDB duplicate-key error (E11000 and
// its relatives), including when it is wrapped or nested inside a WriteException or
// BulkWriteException. It delegates to the driver's own IsDuplicateKeyError, which
// inspects the error chain via the ServerError interface, so app code can ask "was
// this a duplicate key?" without unwrapping driver error types by hand.
func IsDuplicateKey(err error) bool {
	return mongodriver.IsDuplicateKeyError(err)
}

// IsNotFound reports whether err is the driver's "no documents in result"
// sentinel, returned by SingleResult.Decode / FindOne when nothing matched. It
// traverses the error chain, so a wrapped ErrNoDocuments still matches.
func IsNotFound(err error) bool {
	return errors.Is(err, mongodriver.ErrNoDocuments)
}
```
- [ ] **Step 4: Run tests to verify they pass**
Run: `just test ./mongo/...`
Expected: PASS
- [ ] **Step 5: Commit**
```bash
git add mongo/ && git commit -m "feat(mongo): add IsDuplicateKey and IsNotFound error classifiers"
```

---

### Task MG-7: package doc (`doc.go`) and lint

**Files:**
- Create: `mongo/doc.go`

**Interfaces:**
- Produces: package documentation only (no new symbols).

- [ ] **Step 1: Write `doc.go`**

`mongo/doc.go`:
```go
// Package mongo turns a Config (typically env-loaded) into a live, pooled,
// health-checkable MongoDB client and layers the recurring boot-time chores —
// transactions, index/shard provisioning, and error classification — over the
// official driver (go.mongodb.org/mongo-driver/v2). It returns the native
// *mongo.Client and never hides it: full driver access stays available.
//
// The forge package is itself named mongo, so callers that also import the driver
// alias the driver (the idiomatic Go resolution):
//
//	import (
//		forgemongo "github.com/dmitrymomot/forge/mongo"
//		mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
//	)
//
// # Lifecycle
//
// Open applies functional options over DefaultConfig, validates, builds the driver
// client options (URI, pool limits, timeouts, read/write concerns), connects, and
// pings with bounded exponential-backoff retry so a container that races its
// database does not crash-loop. Hand Healthcheck(client) to a readiness probe and
// defer Close(client, logger) in main so it runs after supervisor.Run returns —
// after every service has drained, the only point at which disconnecting cannot
// race in-flight work.
//
//	func main() {
//		ctx, stop := supervisor.NewContext()
//		defer stop()
//
//		cfg := mongo.DefaultConfig()
//		_ = env.ParseWithOptions(&cfg, env.Options{Prefix: "MONGO_"})
//
//		client, err := mongo.Open(ctx, mongo.WithConfig(cfg), mongo.WithLogger(logger))
//		if err != nil {
//			logger.Error("mongo open failed", "err", err)
//			os.Exit(1)
//		}
//		defer mongo.Close(client, logger)
//
//		err = supervisor.Run(ctx,
//			supervisor.WithService(httpserver.New(routes(client))),
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
// sharded deployments and return the driver's error verbatim on a non-sharded
// server.
//
//	specs := map[string][]mongodriver.IndexModel{
//		"users": {{Keys: bson.D{{Key: "email", Value: 1}}, Options: options.Index().SetUnique(true)}},
//	}
//	if err := mongo.EnsureIndexes(ctx, client.Database("app"), specs); err != nil {
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
//	URI                                URI                     "" (required)
//	DATABASE                           Database                ""
//	READ_PREFERENCE                    ReadPreference          "" (driver default)
//	READ_CONCERN                       ReadConcern             "" (driver default)
//	WRITE_CONCERN                      WriteConcern            "" (driver default)
//	MAX_POOL_SIZE                      MaxPoolSize             100
//	MIN_POOL_SIZE                      MinPoolSize             0
//	CONNECT_TIMEOUT                    ConnectTimeout          10s
//	SERVER_SELECTION_TIMEOUT           ServerSelectionTimeout  10s
//	MAX_CONN_IDLE_TIME                 MaxConnIdleTime         0 (no cap)
//	RETRY_ATTEMPTS                     RetryAttempts           3
//	RETRY_INTERVAL                     RetryInterval           1s
//
// READ_PREFERENCE accepts: primary, primaryPreferred, secondary,
// secondaryPreferred, nearest. READ_CONCERN accepts: local, majority, available,
// linearizable, snapshot. WRITE_CONCERN accepts: majority, journaled,
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
```
- [ ] **Step 2: Build, vet, and lint the package**
Run:
```bash
go build ./mongo/... && go vet ./mongo/... && just lint
```
Expected: clean (no build/vet/lint errors). If `just lint` reports `betteralign` field-order changes, accept its reordering of the `Config`/`config` struct fields (it does not affect behavior).
- [ ] **Step 3: Run the full package test suite once more**
Run: `just test ./mongo/...`
Expected: PASS (integration tests SKIP)
- [ ] **Step 4: Commit**
```bash
git add mongo/ && git commit -m "docs(mongo): add package doc with env-var table and examples"
```
## Package: `opensearch`

> **Driver:** `github.com/opensearch-project/opensearch-go/v4@v4.6.0`. Inside the package the driver is imported aliased so the forge package itself can be named `opensearch`:
> `osgo "github.com/opensearch-project/opensearch-go/v4"` and `osapi "github.com/opensearch-project/opensearch-go/v4/opensearchapi"`. In task signatures the SPEC-facing return type is written `*opensearch.Client` for readability; that is `*osgo.Client` (the base client) in code.
>
> **Verified v4.6.0 API facts (do not deviate):**
> - `osgo.NewClient(osgo.Config{...}) (*osgo.Client, error)` builds the base client. `osgo.Config` has `Addresses []string`, `Username`, `Password`, `MaxRetries int`, and `Transport http.RoundTripper` (the request timeout and `InsecureSkipVerify` live on a `*http.Transport` you build and assign to `Config.Transport` — **not** on `osgo.Config`). There is **no** `RequestTimeout` field on `osgo.Config`.
> - The base client only exposes `Transport opensearchtransport.Interface` and `Do(ctx, req, dataPointer) (*osgo.Response, error)`. Typed requests live on the **api client** `*osapi.Client`, built with `osapi.NewClient(osapi.Config{Client: osgo.Config{...}}) (*osapi.Client, error)`. There is **no public function to wrap an existing base client**, so the package re-derives an api client from a base via the verified pattern: build a throwaway api client with `osapi.NewClient(osapi.Config{...})`, then assign `apiClient.Client = base`. Every typed call routes through `apiClient.Client.Do`, so it uses the base's transport. This is the `apiFor(base)` internal helper.
> - Liveness/health uses `apiClient.Cluster.Health(ctx, nil) (*osapi.ClusterHealthResp, error)` or `apiClient.Info(ctx, nil) (*osapi.InfoResp, error)`.
> - `apiClient.Indices.Exists(ctx, osapi.IndicesExistsReq{Indices: []string{name}}) (*osgo.Response, error)` — a HEAD; on a missing index it returns a non-nil `*osgo.Response` with `StatusCode == 404` and a plain non-typed error. Detect absence via `resp.StatusCode == http.StatusNotFound`, **not** via `IsNotFound`.
> - `apiClient.Indices.Create(ctx, osapi.IndicesCreateReq{Index, Body: io.Reader}) (*osapi.IndicesCreateResp, error)`.
> - `apiClient.IndexTemplate.Create(ctx, osapi.IndexTemplateCreateReq{IndexTemplate, Body: io.Reader}) (*osapi.IndexTemplateCreateResp, error)` — PUT `_index_template`, upsert by name (idempotent).
> - `apiClient.Indices.Mapping.Put(ctx, osapi.MappingPutReq{Indices: []string{name}, Body: io.Reader}) (*osapi.MappingPutResp, error)` — additive mappings.
> - **Error classification:** v4 has no single typed api error. Parsed errors are `*osgo.StructError` and `*osgo.StringError`, both carrying a `Status int`. `IsNotFound` matches either via `errors.As` and `Status == http.StatusNotFound (404)`.

### Task OS-1: Config, DefaultConfig, Validate, errors

**Files:**
- Create: `opensearch/config.go`
- Create: `opensearch/errors.go`
- Test: `opensearch/config_test.go`

**Interfaces:**
- Produces: `opensearch.Config`; `DefaultConfig() Config`; `(Config) Validate() error`; sentinels `ErrInvalidConfig`, `ErrConnect`, `ErrHealthcheck`, `ErrSetup`.

- [ ] **Step 1: Write the failing test**
```go
package opensearch_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgeos "github.com/dmitrymomot/forge/opensearch"
)

func TestDefaultConfig(t *testing.T) {
	cfg := forgeos.DefaultConfig()
	assert.Equal(t, 3, cfg.MaxRetries)
	assert.Equal(t, 10*time.Second, cfg.RequestTimeout)
	assert.Equal(t, 3, cfg.RetryAttempts)
	assert.Equal(t, time.Second, cfg.RetryInterval)
	assert.False(t, cfg.InsecureSkipVerify)
	assert.Empty(t, cfg.Addresses)
	assert.Empty(t, cfg.Username)
	assert.Empty(t, cfg.Password)
	// DefaultConfig alone is not valid: Addresses is required.
	require.Error(t, cfg.Validate())

	// A minimally valid config validates clean.
	cfg.Addresses = []string{"http://localhost:9200"}
	require.NoError(t, cfg.Validate())
}

func TestConfig_Validate(t *testing.T) {
	base := func() forgeos.Config {
		c := forgeos.DefaultConfig()
		c.Addresses = []string{"http://localhost:9200"}
		return c
	}

	bad := map[string]forgeos.Config{
		"empty addresses": func() forgeos.Config { c := base(); c.Addresses = nil; return c }(),
		"blank address":   func() forgeos.Config { c := base(); c.Addresses = []string{"  "}; return c }(),
		"neg max retries": func() forgeos.Config { c := base(); c.MaxRetries = -1; return c }(),
		"neg req timeout": func() forgeos.Config { c := base(); c.RequestTimeout = -1; return c }(),
		"neg retry att":   func() forgeos.Config { c := base(); c.RetryAttempts = -1; return c }(),
		"neg retry intvl": func() forgeos.Config { c := base(); c.RetryInterval = -1; return c }(),
	}
	for name, cfg := range bad {
		t.Run(name, func(t *testing.T) {
			err := cfg.Validate()
			require.Error(t, err)
			assert.ErrorIs(t, err, forgeos.ErrInvalidConfig)
		})
	}

	require.NoError(t, base().Validate())
}

func TestConfig_EnvTags(t *testing.T) {
	want := map[string]string{
		"Addresses":          "ADDRESSES",
		"Username":            "USERNAME",
		"Password":            "PASSWORD",
		"InsecureSkipVerify":  "INSECURE_SKIP_VERIFY",
		"MaxRetries":          "MAX_RETRIES",
		"RequestTimeout":      "REQUEST_TIMEOUT",
		"RetryAttempts":       "RETRY_ATTEMPTS",
		"RetryInterval":       "RETRY_INTERVAL",
	}
	typ := reflect.TypeFor[forgeos.Config]()
	for fname, tag := range want {
		f, ok := typ.FieldByName(fname)
		require.Truef(t, ok, "field %s missing", fname)
		assert.Equalf(t, tag, f.Tag.Get("env"), "field %s env tag", fname)
	}
	// Addresses is []string; caarlos0/env parses comma-separated values into it,
	// but the tag itself is still the plain name ADDRESSES.
	f, _ := typ.FieldByName("Addresses")
	assert.Equal(t, reflect.TypeOf([]string{}), f.Type)
}
```
- [ ] **Step 2: Run test to verify it fails**
Run: `go test -race ./opensearch/ -run TestDefaultConfig -v`
Expected: FAIL (package/`DefaultConfig` undefined)
- [ ] **Step 3: Write the implementation**

`opensearch/errors.go`:
```go
package opensearch

import "errors"

// Sentinel errors returned (often wrapped) by this package. Match with errors.Is.
var (
	// ErrInvalidConfig is returned (joined) when a Config field or option value is invalid.
	ErrInvalidConfig = errors.New("opensearch: invalid config")
	// ErrConnect is returned by Open when the cluster could not be reached within the retry budget.
	ErrConnect = errors.New("opensearch: connect failed")
	// ErrHealthcheck is returned by the Healthcheck closure when a liveness probe fails.
	ErrHealthcheck = errors.New("opensearch: healthcheck failed")
	// ErrSetup is returned by Setup.Apply when index/template provisioning fails.
	ErrSetup = errors.New("opensearch: setup failed")
)
```

`opensearch/config.go`:
```go
package opensearch

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Config holds the serializable settings for an OpenSearch client. The env struct
// tags are inert strings — this package imports no config loader. Populate Config
// with any loader that reads env struct tags, typically by seeding from
// DefaultConfig and parsing the environment over it. Addresses is a []string; a
// comma-separated env value (caarlos0/env's default) parses into it under the
// ADDRESSES key. Field order is subject to the repo's betteralign tooling.
type Config struct {
	Addresses          []string      `env:"ADDRESSES"`            // node URLs, e.g. https://os:9200 (required)
	Username           string        `env:"USERNAME"`             // HTTP basic auth user
	Password           string        `env:"PASSWORD"`             // HTTP basic auth password
	InsecureSkipVerify bool          `env:"INSECURE_SKIP_VERIFY"` // skip TLS verification (dev/self-signed only)
	MaxRetries         int           `env:"MAX_RETRIES"`          // driver retry count on retriable status codes
	RequestTimeout     time.Duration `env:"REQUEST_TIMEOUT"`      // per-request timeout applied via transport + ctx
	RetryAttempts      int           `env:"RETRY_ATTEMPTS"`       // Open's bounded connect-retry attempts
	RetryInterval      time.Duration `env:"RETRY_INTERVAL"`       // base backoff between connect attempts
}

// DefaultConfig returns production-sane defaults and is the single source of truth
// for them (there are no envDefault tags to drift from it). Addresses is left empty
// and is required, so DefaultConfig alone fails Validate.
func DefaultConfig() Config {
	return Config{
		MaxRetries:     3,
		RequestTimeout: 10 * time.Second,
		RetryAttempts:  3,
		RetryInterval:  time.Second,
	}
}

// Validate reports whether the field values are usable, returning an
// ErrInvalidConfig-wrapped, single-line joined error otherwise. Open calls it
// defensively; callers may call it after loading from env.
func (c Config) Validate() error {
	var errs []error
	if len(c.Addresses) == 0 {
		errs = append(errs, fmt.Errorf("%w: Addresses must not be empty", ErrInvalidConfig))
	}
	for i, a := range c.Addresses {
		if strings.TrimSpace(a) == "" {
			errs = append(errs, fmt.Errorf("%w: Addresses[%d] must not be blank", ErrInvalidConfig, i))
		}
	}
	if c.MaxRetries < 0 {
		errs = append(errs, fmt.Errorf("%w: MaxRetries must be >= 0", ErrInvalidConfig))
	}
	if c.RequestTimeout < 0 {
		errs = append(errs, fmt.Errorf("%w: RequestTimeout must be >= 0", ErrInvalidConfig))
	}
	if c.RetryAttempts < 0 {
		errs = append(errs, fmt.Errorf("%w: RetryAttempts must be >= 0", ErrInvalidConfig))
	}
	if c.RetryInterval < 0 {
		errs = append(errs, fmt.Errorf("%w: RetryInterval must be >= 0", ErrInvalidConfig))
	}
	return errors.Join(errs...)
}
```
- [ ] **Step 4: Run tests to verify they pass**
Run: `just test ./opensearch/...`
Expected: PASS
- [ ] **Step 5: Commit**
```bash
git add opensearch/ && git commit -m "feat(opensearch): add Config, DefaultConfig, Validate"
```

### Task OS-2: Driver dependency, options, and `Open` (build driver config + connect-retry)

**Files:**
- Modify: `go.mod`, `go.sum` (add the driver)
- Create: `opensearch/options.go`
- Create: `opensearch/open.go`
- Test: `opensearch/options_test.go`
- Test: `opensearch/open_test.go`

**Interfaces:**
- Produces: `Option`; `WithConfig(Config) Option`; `WithLogger(*slog.Logger) Option`; `WithClientConfig(func(*osgo.Config)) Option`; `Open(ctx, ...Option) (*opensearch.Client, error)`; internal `config` struct and `apiFor(*osgo.Client) *osapi.Client` helper.

- [ ] **Step 1: Add the driver dependency**
Run:
```bash
go get github.com/opensearch-project/opensearch-go/v4@v4.6.0
```
Expected: `go.mod`/`go.sum` updated with `github.com/opensearch-project/opensearch-go/v4 v4.6.0`.

- [ ] **Step 2: Write the failing test**

`opensearch/options_test.go`:
```go
package opensearch_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgeos "github.com/dmitrymomot/forge/opensearch"
)

func TestOpen_NilOptionRejected(t *testing.T) {
	opts := map[string]forgeos.Option{
		"client config": forgeos.WithClientConfig(nil),
	}
	for name, opt := range opts {
		t.Run(name, func(t *testing.T) {
			cfg := forgeos.DefaultConfig()
			cfg.Addresses = []string{"http://127.0.0.1:9200"}
			// A valid Config is supplied so the failure is unambiguously the nil
			// option's rejection (ErrInvalidConfig), not a Validate failure.
			_, err := forgeos.Open(t.Context(), forgeos.WithConfig(cfg), opt)
			require.Error(t, err)
			assert.ErrorIs(t, err, forgeos.ErrInvalidConfig)
		})
	}
}

func TestOpen_InvalidConfigRejected(t *testing.T) {
	// No Addresses -> Validate fails before any connection attempt.
	_, err := forgeos.Open(t.Context())
	require.Error(t, err)
	assert.ErrorIs(t, err, forgeos.ErrInvalidConfig)
}

func TestOpen_NilLoggerAllowed(t *testing.T) {
	// A nil logger is not a validation error; it is replaced by a discard logger.
	// Point at an unreachable address with a 1-attempt budget so Open returns fast
	// with ErrConnect (proving WithLogger(nil) was accepted, not rejected).
	cfg := forgeos.DefaultConfig()
	cfg.Addresses = []string{"http://127.0.0.1:1"}
	cfg.RetryAttempts = 1
	cfg.RetryInterval = time.Millisecond
	cfg.RequestTimeout = 50 * time.Millisecond
	_, err := forgeos.Open(t.Context(), forgeos.WithConfig(cfg), forgeos.WithLogger(nil))
	require.Error(t, err)
	assert.ErrorIs(t, err, forgeos.ErrConnect)
	assert.NotErrorIs(t, err, forgeos.ErrInvalidConfig)
}
```

`opensearch/open_test.go`:
```go
package opensearch_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgeos "github.com/dmitrymomot/forge/opensearch"
)

func TestOpen_RetryExhausted(t *testing.T) {
	// Port 1 is unreachable; with a tiny budget Open exhausts retries quickly and
	// returns ErrConnect (joined with the last driver/transport error).
	cfg := forgeos.DefaultConfig()
	cfg.Addresses = []string{"http://127.0.0.1:1"}
	cfg.RetryAttempts = 2
	cfg.RetryInterval = 2 * time.Millisecond
	cfg.RequestTimeout = 50 * time.Millisecond

	start := time.Now()
	_, err := forgeos.Open(t.Context(), forgeos.WithConfig(cfg))
	require.Error(t, err)
	assert.ErrorIs(t, err, forgeos.ErrConnect)
	// Two attempts + one backoff must not take anywhere near the per-call ceiling.
	assert.Less(t, time.Since(start), 10*time.Second)
}

func TestOpen_ContextCancelledMidBackoff(t *testing.T) {
	cfg := forgeos.DefaultConfig()
	cfg.Addresses = []string{"http://127.0.0.1:1"}
	cfg.RetryAttempts = 50
	cfg.RetryInterval = 200 * time.Millisecond
	cfg.RequestTimeout = 50 * time.Millisecond

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	_, err := forgeos.Open(ctx, forgeos.WithConfig(cfg))
	require.Error(t, err)
	// Cancellation surfaces as ErrConnect joined with ctx.Err(); the loop must not
	// run all 50 attempts.
	assert.ErrorIs(t, err, forgeos.ErrConnect)
}
```
- [ ] **Step 3: Run test to verify it fails**
Run: `go test -race ./opensearch/ -run TestOpen_RetryExhausted -v`
Expected: FAIL (`Open`/`WithClientConfig` undefined)
- [ ] **Step 4: Write the implementation**

`opensearch/options.go`:
```go
package opensearch

import (
	"fmt"
	"log/slog"

	osgo "github.com/opensearch-project/opensearch-go/v4"
)

// config holds resolved settings for a single Open call. The embedded Config
// carries serializable data; the remaining fields are non-serializable code values.
type config struct {
	logger       *slog.Logger
	clientConfig func(*osgo.Config)
	errs         []error
	Config
}

// Option configures Open. Invalid values accumulate and are returned by Open.
type Option func(*config)

// WithConfig sets the whole serializable data block at once. Build the argument
// from DefaultConfig() (or an env-parsed copy); a bare Config{} fails Validate.
// Options apply in order — place WithConfig before any code options it should not
// clobber.
func WithConfig(cfg Config) Option {
	return func(c *config) { c.Config = cfg }
}

// WithLogger sets the slog.Logger for lifecycle logging. Default slog.Default();
// nil installs a discard handler at Open time (it is not a validation error).
func WithLogger(l *slog.Logger) Option {
	return func(c *config) { c.logger = l }
}

// WithClientConfig is the native-config escape hatch: it mutates the driver's
// osgo.Config after the Config overlay and runs LAST in Open, so anything Config
// does not cover (a custom Transport/TLS, a Signer, a Logger) stays reachable. A
// nil func is rejected (ErrInvalidConfig).
func WithClientConfig(fn func(*osgo.Config)) Option {
	return func(c *config) {
		if fn == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithClientConfig received a nil func", ErrInvalidConfig))
			return
		}
		c.clientConfig = fn
	}
}
```

`opensearch/open.go`:
```go
package opensearch

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	osgo "github.com/opensearch-project/opensearch-go/v4"
	osapi "github.com/opensearch-project/opensearch-go/v4/opensearchapi"
)

// maxBackoff caps the exponential connect-retry wait.
const maxBackoff = 30 * time.Second

// Open builds an OpenSearch client from Config plus options, then verifies the
// cluster is reachable with a bounded retry/backoff. It returns the base
// *opensearch.Client (callers wrap it with opensearchapi for typed requests). On
// failure it returns a sentinel-wrapped, single-line error and leaks nothing.
func Open(ctx context.Context, opts ...Option) (*osgo.Client, error) {
	c := config{Config: DefaultConfig()}
	for _, opt := range opts {
		opt(&c)
	}
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	if err := c.Config.Validate(); err != nil {
		return nil, err
	}

	logger := c.logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	// Build the driver config. RequestTimeout and InsecureSkipVerify live on the
	// transport, not osgo.Config; MaxRetries is a driver field.
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: c.InsecureSkipVerify}, //nolint:gosec // opt-in via Config for dev/self-signed
		ResponseHeaderTimeout: c.RequestTimeout,
	}
	driverCfg := osgo.Config{
		Addresses:  c.Addresses,
		Username:   c.Username,
		Password:   c.Password,
		MaxRetries: c.MaxRetries,
		Transport:  transport,
	}
	// Escape hatch runs LAST so it can override anything above.
	if c.clientConfig != nil {
		c.clientConfig(&driverCfg)
	}

	base, err := osgo.NewClient(driverCfg)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConnect, err)
	}

	if err := waitForCluster(ctx, base, c.Config, logger); err != nil {
		return nil, err
	}

	logger.Info("opensearch: connected", slog.Any("addresses", c.Addresses))
	return base, nil
}

// waitForCluster pings the cluster (cluster health) until it responds or the retry
// budget is spent. Backoff is RetryInterval * 2^attempt capped at maxBackoff; ctx
// cancellation is honored between attempts. RetryAttempts <= 1 means a single
// attempt with no wait.
func waitForCluster(ctx context.Context, base *osgo.Client, cfg Config, logger *slog.Logger) error {
	attempts := cfg.RetryAttempts
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := range attempts {
		if attempt > 0 {
			wait := backoff(cfg.RetryInterval, attempt)
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return fmt.Errorf("%w: %v", ErrConnect, errors.Join(ctx.Err(), lastErr))
			case <-timer.C:
			}
		}

		lastErr = ping(ctx, base, cfg.RequestTimeout)
		if lastErr == nil {
			return nil
		}
		logger.Warn("opensearch: connect attempt failed",
			slog.Int("attempt", attempt+1),
			slog.Int("attempts", attempts),
			slog.Any("err", lastErr),
		)
	}
	return fmt.Errorf("%w: %v", ErrConnect, lastErr)
}

// backoff returns interval * 2^attempt, capped at maxBackoff.
func backoff(interval time.Duration, attempt int) time.Duration {
	if interval <= 0 {
		return 0
	}
	wait := interval << attempt
	if wait <= 0 || wait > maxBackoff { // overflow or over the cap
		return maxBackoff
	}
	return wait
}

// ping issues a cluster-health request under a per-attempt timeout derived from
// RequestTimeout (0 means no extra deadline beyond ctx).
func ping(ctx context.Context, base *osgo.Client, timeout time.Duration) error {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	api := apiFor(base)
	if _, err := api.Cluster.Health(ctx, nil); err != nil {
		return err
	}
	return nil
}

// apiFor wraps an existing base client in an opensearchapi.Client so typed
// requests reuse the base's transport. opensearch-go v4 exposes no public wrap
// function, so a throwaway api client is built and its base swapped out; every
// typed call routes through apiClient.Client.Do.
func apiFor(base *osgo.Client) *osapi.Client {
	api, err := osapi.NewClient(osapi.Config{Client: osgo.Config{Addresses: []string{"http://localhost:9200"}}})
	if err != nil {
		// osapi.NewClient with a syntactically valid address cannot fail; guard anyway.
		return &osapi.Client{Client: base}
	}
	api.Client = base
	return api
}
```
- [ ] **Step 5: Run tests to verify they pass**
Run: `just test ./opensearch/...`
Expected: PASS
- [ ] **Step 6: Commit**
```bash
git add go.mod go.sum opensearch/ && git commit -m "feat(opensearch): add driver dep, options, and Open with connect-retry"
```

### Task OS-3: `Close` and `Healthcheck`

**Files:**
- Create: `opensearch/lifecycle.go`
- Test: `opensearch/lifecycle_test.go`

**Interfaces:**
- Produces: `Close(c *opensearch.Client, log *slog.Logger)`; `Healthcheck(c *opensearch.Client) func(context.Context) error`.

- [ ] **Step 1: Write the failing test**

`opensearch/lifecycle_test.go`:
```go
package opensearch_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgeos "github.com/dmitrymomot/forge/opensearch"
)

func TestClose_NilTolerated(t *testing.T) {
	// Close must tolerate a nil client and a nil logger without panicking; it never
	// touches the network (the HTTP client owns no persistent sockets to release).
	assert.NotPanics(t, func() { forgeos.Close(nil, nil) })

	// When a live server is available, Close on a real client is also a no-op.
	addr := os.Getenv("FORGE_TEST_OPENSEARCH_ADDR")
	if addr == "" {
		return
	}
	cfg := forgeos.DefaultConfig()
	cfg.Addresses = []string{addr}
	cfg.RetryAttempts = 5
	cfg.RetryInterval = 500 * time.Millisecond
	cfg.RequestTimeout = 5 * time.Second
	client, err := forgeos.Open(t.Context(), forgeos.WithConfig(cfg))
	require.NoError(t, err)
	assert.NotPanics(t, func() { forgeos.Close(client, nil) })
}

func TestHealthcheck_Integration(t *testing.T) {
	addr := os.Getenv("FORGE_TEST_OPENSEARCH_ADDR")
	if addr == "" {
		t.Skip("set FORGE_TEST_OPENSEARCH_ADDR to run the opensearch integration test")
	}
	cfg := forgeos.DefaultConfig()
	cfg.Addresses = []string{addr}
	cfg.RetryAttempts = 5
	cfg.RetryInterval = 500 * time.Millisecond
	cfg.RequestTimeout = 5 * time.Second

	client, err := forgeos.Open(t.Context(), forgeos.WithConfig(cfg))
	require.NoError(t, err)
	defer forgeos.Close(client, nil)

	check := forgeos.Healthcheck(client)
	require.NotNil(t, check)
	require.NoError(t, check(t.Context()))
}
```
- [ ] **Step 2: Run test to verify it fails**
Run: `go test -race ./opensearch/ -run TestClose_NilTolerated -v`
Expected: FAIL (`Close`/`Healthcheck` undefined)
- [ ] **Step 3: Write the implementation**

`opensearch/lifecycle.go`:
```go
package opensearch

import (
	"context"
	"fmt"
	"log/slog"

	osgo "github.com/opensearch-project/opensearch-go/v4"
)

// Close logs a single "closing" line. The opensearch-go client is HTTP-based and
// owns no long-lived sockets the package controls, so there is nothing to release —
// this helper exists for lifecycle symmetry with the other connectivity packages
// so every backend reads Open / Close / Healthcheck. Used as
// `defer Close(client, logger)` in main. A nil logger is tolerated (the log line is
// skipped); a nil client is tolerated (no-op).
func Close(c *osgo.Client, log *slog.Logger) {
	if log != nil {
		log.Info("opensearch: closing client")
	}
	_ = c
}

// Healthcheck returns a stateless closure that probes the cluster's health,
// wrapping any failure in ErrHealthcheck. Its func(context.Context) error shape is
// exactly what a readiness/liveness probe wants; it is safe to call on every probe.
func Healthcheck(c *osgo.Client) func(context.Context) error {
	return func(ctx context.Context) error {
		api := apiFor(c)
		if _, err := api.Cluster.Health(ctx, nil); err != nil {
			return fmt.Errorf("%w: %v", ErrHealthcheck, err)
		}
		return nil
	}
}
```
- [ ] **Step 4: Run tests to verify they pass**
Run: `just test ./opensearch/...`
Expected: PASS (integration test skipped without `FORGE_TEST_OPENSEARCH_ADDR`)
- [ ] **Step 5: Commit**
```bash
git add opensearch/ && git commit -m "feat(opensearch): add Close and Healthcheck lifecycle helpers"
```

### Task OS-4: `Setup` runner — `parseSetupFS` pure function + `NewSetup`/`Apply`/`WithUpdateMappings`

**Files:**
- Create: `opensearch/setup.go`
- Test: `opensearch/setup_test.go`

**Interfaces:**
- Produces: `Setup`; `SetupOption`; `NewSetup(fsys fs.FS, opts ...SetupOption) *Setup`; `WithUpdateMappings(enabled bool) SetupOption`; `(s *Setup) Apply(ctx, c *opensearch.Client) error`; internal pure `parseSetupFS(fsys fs.FS) ([]indexDef, []templateDef, error)`.

- [ ] **Step 1: Write the failing test**

`opensearch/setup_test.go`:
```go
package opensearch_test

import (
	"os"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgeos "github.com/dmitrymomot/forge/opensearch"
)

func TestSetup_ParseRejectsMalformed(t *testing.T) {
	// A malformed index JSON file must make Apply fail with ErrSetup before any
	// network call. parseSetupFS runs first inside Apply, so passing a nil client is
	// safe — the parse error short-circuits before the client is used.
	fsys := fstest.MapFS{
		"users.index.json": {Data: []byte("{ this is not json ")},
	}
	setup := forgeos.NewSetup(fsys)
	require.NotNil(t, setup)

	err := setup.Apply(t.Context(), nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, forgeos.ErrSetup)
}

func TestSetup_NilFSIsError(t *testing.T) {
	err := forgeos.NewSetup(nil).Apply(t.Context(), nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, forgeos.ErrSetup)
}

func TestSetup_Integration_Idempotent(t *testing.T) {
	addr := os.Getenv("FORGE_TEST_OPENSEARCH_ADDR")
	if addr == "" {
		t.Skip("set FORGE_TEST_OPENSEARCH_ADDR to run the opensearch integration test")
	}
	cfg := forgeos.DefaultConfig()
	cfg.Addresses = []string{addr}
	cfg.RetryAttempts = 5
	cfg.RetryInterval = 500 * time.Millisecond
	cfg.RequestTimeout = 5 * time.Second

	client, err := forgeos.Open(t.Context(), forgeos.WithConfig(cfg))
	require.NoError(t, err)
	defer forgeos.Close(client, nil)

	fsys := fstest.MapFS{
		"forge_setup_test.index.json": {Data: []byte(`{
			"settings": {"number_of_shards": 1, "number_of_replicas": 0},
			"mappings": {"properties": {"name": {"type": "keyword"}}}
		}`)},
		"forge_setup_test.template.json": {Data: []byte(`{
			"index_patterns": ["forge_setup_logs-*"],
			"template": {"settings": {"number_of_shards": 1}}
		}`)},
	}

	setup := forgeos.NewSetup(fsys, forgeos.WithUpdateMappings(true))

	// First Apply creates the index and upserts the template.
	require.NoError(t, setup.Apply(t.Context(), client))
	// Second Apply is a no-op (index already present; template re-upsert is idempotent).
	require.NoError(t, setup.Apply(t.Context(), client))
}
```
- [ ] **Step 2: Run test to verify it fails**
Run: `go test -race ./opensearch/ -run TestSetup_ParseRejectsMalformed -v`
Expected: FAIL (`NewSetup`/`Apply`/`WithUpdateMappings` undefined)
- [ ] **Step 3: Write the implementation**

`opensearch/setup.go`:
```go
package opensearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"sort"
	"strings"

	osgo "github.com/opensearch-project/opensearch-go/v4"
	osapi "github.com/opensearch-project/opensearch-go/v4/opensearchapi"
)

const (
	indexSuffix    = ".index.json"
	templateSuffix = ".template.json"
)

// indexDef is a parsed <name>.index.json setup file.
type indexDef struct {
	name string
	body []byte
}

// templateDef is a parsed <name>.template.json setup file.
type templateDef struct {
	name string
	body []byte
}

// Setup applies declarative OpenSearch index and template definitions embedded in
// an fs.FS at boot. It is forward-only: it creates absent indices, PUT-upserts
// templates, and (with WithUpdateMappings) PUTs additive mappings onto existing
// indices. It mirrors the migration package's up-only stance.
type Setup struct {
	fsys           fs.FS
	updateMappings bool
}

// SetupOption configures a Setup.
type SetupOption func(*Setup)

// WithUpdateMappings, when enabled, makes Apply additionally PUT the mappings block
// of each <name>.index.json onto an already-existing index (additive field changes
// only; OpenSearch rejects non-additive changes, which remain a consumer-driven
// reindex). Default false.
func WithUpdateMappings(enabled bool) SetupOption {
	return func(s *Setup) { s.updateMappings = enabled }
}

// NewSetup builds a Setup over fsys. Definition files live at the root of fsys and
// are matched by suffix: <name>.index.json and <name>.template.json. Parsing is
// deferred to Apply so construction never fails.
func NewSetup(fsys fs.FS, opts ...SetupOption) *Setup {
	s := &Setup{fsys: fsys}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Apply provisions the parsed definitions against c. It first parses the FS
// (returning ErrSetup on a malformed file before touching the network), then for
// each template PUT-upserts it, and for each index creates it when absent (and, if
// WithUpdateMappings is set, PUTs its mappings when already present). It is
// idempotent: a second Apply with no FS changes performs no mutating index create.
func (s *Setup) Apply(ctx context.Context, c *osgo.Client) error {
	indices, templates, err := parseSetupFS(s.fsys)
	if err != nil {
		return err
	}

	api := apiFor(c)

	// Templates first (an index may rely on a matching template at create time).
	for _, t := range templates {
		if _, err := api.IndexTemplate.Create(ctx, osapi.IndexTemplateCreateReq{
			IndexTemplate: t.name,
			Body:          bytes.NewReader(t.body),
		}); err != nil {
			return fmt.Errorf("%w: template %q: %v", ErrSetup, t.name, err)
		}
	}

	for _, idx := range indices {
		exists, err := indexExists(ctx, api, idx.name)
		if err != nil {
			return fmt.Errorf("%w: index %q exists check: %v", ErrSetup, idx.name, err)
		}
		if !exists {
			if _, err := api.Indices.Create(ctx, osapi.IndicesCreateReq{
				Index: idx.name,
				Body:  bytes.NewReader(idx.body),
			}); err != nil {
				return fmt.Errorf("%w: create index %q: %v", ErrSetup, idx.name, err)
			}
			continue
		}
		if s.updateMappings {
			mapping, err := extractMappings(idx.body)
			if err != nil {
				return fmt.Errorf("%w: index %q mappings: %v", ErrSetup, idx.name, err)
			}
			if mapping != nil {
				if _, err := api.Indices.Mapping.Put(ctx, osapi.MappingPutReq{
					Indices: []string{idx.name},
					Body:    bytes.NewReader(mapping),
				}); err != nil {
					return fmt.Errorf("%w: update mappings %q: %v", ErrSetup, idx.name, err)
				}
			}
		}
	}
	return nil
}

// indexExists reports whether an index is present. The HEAD-based Exists call
// returns a 404 *opensearch.Response (and a non-typed error) when absent; presence
// is determined from the status code, not from the error.
func indexExists(ctx context.Context, api *osapi.Client, name string) (bool, error) {
	resp, err := api.Indices.Exists(ctx, osapi.IndicesExistsReq{Indices: []string{name}})
	if resp != nil && resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if resp == nil {
		return false, nil
	}
	return resp.StatusCode >= 200 && resp.StatusCode < 300, nil
}

// extractMappings pulls the "mappings" object out of an index body for an additive
// mapping PUT. It returns nil (no error) when the index body has no mappings block.
func extractMappings(indexBody []byte) ([]byte, error) {
	var doc struct {
		Mappings json.RawMessage `json:"mappings"`
	}
	if err := json.Unmarshal(indexBody, &doc); err != nil {
		return nil, err
	}
	if len(doc.Mappings) == 0 {
		return nil, nil
	}
	return doc.Mappings, nil
}

// parseSetupFS reads every <name>.index.json and <name>.template.json at the root
// of fsys into sorted, validated definitions. It is a pure function (no network,
// no client) so it is unit-testable with fstest.MapFS. A nil fsys, an unreadable
// file, or malformed JSON is an ErrSetup-wrapped error.
func parseSetupFS(fsys fs.FS) ([]indexDef, []templateDef, error) {
	if fsys == nil {
		return nil, nil, fmt.Errorf("%w: nil fs.FS", ErrSetup)
	}
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, nil, fmt.Errorf("%w: read dir: %v", ErrSetup, err)
	}

	var (
		indices   []indexDef
		templates []templateDef
		errs      []error
	)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		switch {
		case strings.HasSuffix(name, indexSuffix):
			body, perr := readJSON(fsys, name)
			if perr != nil {
				errs = append(errs, perr)
				continue
			}
			indices = append(indices, indexDef{name: strings.TrimSuffix(name, indexSuffix), body: body})
		case strings.HasSuffix(name, templateSuffix):
			body, perr := readJSON(fsys, name)
			if perr != nil {
				errs = append(errs, perr)
				continue
			}
			templates = append(templates, templateDef{name: strings.TrimSuffix(name, templateSuffix), body: body})
		}
	}
	if len(errs) > 0 {
		return nil, nil, fmt.Errorf("%w: %v", ErrSetup, errors.Join(errs...))
	}

	// Deterministic order so Apply is reproducible.
	sort.Slice(indices, func(i, j int) bool { return indices[i].name < indices[j].name })
	sort.Slice(templates, func(i, j int) bool { return templates[i].name < templates[j].name })
	return indices, templates, nil
}

// readJSON reads a file and validates it parses as JSON, returning the raw bytes.
func readJSON(fsys fs.FS, name string) ([]byte, error) {
	body, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", name, err)
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("%q is not valid JSON", name)
	}
	return body, nil
}
```
- [ ] **Step 4: Run tests to verify they pass**
Run: `just test ./opensearch/...`
Expected: PASS (integration idempotency test skipped without `FORGE_TEST_OPENSEARCH_ADDR`)
- [ ] **Step 5: Commit**
```bash
git add opensearch/ && git commit -m "feat(opensearch): add declarative Setup runner with fs parsing"
```

### Task OS-5: `IsNotFound` (404 classification)

**Files:**
- Create: `opensearch/classify.go`
- Test: `opensearch/classify_test.go`

**Interfaces:**
- Produces: `IsNotFound(err error) bool`.

- [ ] **Step 1: Write the failing test**

`opensearch/classify_test.go`:
```go
package opensearch_test

import (
	"errors"
	"fmt"
	"testing"

	osgo "github.com/opensearch-project/opensearch-go/v4"
	"github.com/stretchr/testify/assert"

	forgeos "github.com/dmitrymomot/forge/opensearch"
)

func TestIsNotFound(t *testing.T) {
	// opensearch-go v4 parses api errors into *StructError / *StringError, each
	// carrying Status. A 404 in either shape must classify as not-found, including
	// when wrapped by fmt.Errorf.
	structErr := &osgo.StructError{Status: 404}
	stringErr := &osgo.StringError{Status: 404, Err: "no such index"}

	assert.True(t, forgeos.IsNotFound(structErr))
	assert.True(t, forgeos.IsNotFound(stringErr))
	assert.True(t, forgeos.IsNotFound(fmt.Errorf("setup: %w", structErr)))
	assert.True(t, forgeos.IsNotFound(fmt.Errorf("setup: %w", stringErr)))

	// Non-404 statuses and unrelated errors are not not-found.
	assert.False(t, forgeos.IsNotFound(&osgo.StructError{Status: 500}))
	assert.False(t, forgeos.IsNotFound(&osgo.StringError{Status: 403, Err: "forbidden"}))
	assert.False(t, forgeos.IsNotFound(errors.New("connection refused")))
	assert.False(t, forgeos.IsNotFound(nil))
}
```
- [ ] **Step 2: Run test to verify it fails**
Run: `go test -race ./opensearch/ -run TestIsNotFound -v`
Expected: FAIL (`IsNotFound` undefined)
- [ ] **Step 3: Write the implementation**

`opensearch/classify.go`:
```go
package opensearch

import (
	"errors"
	"net/http"

	osgo "github.com/opensearch-project/opensearch-go/v4"
)

// IsNotFound reports whether err is (or wraps) an OpenSearch 404 — an absent index
// or document. opensearch-go v4 parses api error responses into *opensearch.StructError
// or *opensearch.StringError, both of which carry an HTTP Status; IsNotFound matches
// either when that status is 404. It returns false for nil and for non-404 errors.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	var structErr *osgo.StructError
	if errors.As(err, &structErr) && structErr.Status == http.StatusNotFound {
		return true
	}
	var stringErr *osgo.StringError
	if errors.As(err, &stringErr) && stringErr.Status == http.StatusNotFound {
		return true
	}
	return false
}
```
- [ ] **Step 4: Run tests to verify they pass**
Run: `just test ./opensearch/...`
Expected: PASS
- [ ] **Step 5: Commit**
```bash
git add opensearch/ && git commit -m "feat(opensearch): add IsNotFound error classification"
```

### Task OS-6: Package doc + lint

**Files:**
- Create: `opensearch/doc.go`

**Interfaces:**
- Produces: package documentation (no new exported symbols).

- [ ] **Step 1: Write the implementation**

`opensearch/doc.go`:
```go
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
```
- [ ] **Step 2: Run the full package test suite**
Run: `just test ./opensearch/...`
Expected: PASS
- [ ] **Step 3: Run the linter**
Run: `just lint`
Expected: PASS (no findings in `opensearch/`)
- [ ] **Step 4: Commit**
```bash
git add opensearch/ && git commit -m "docs(opensearch): add package doc with env-var table and example"
```
---

## Build order & sequencing

The packages are independent except for the `postgres` ↔ `migration` seam, so build them in this order. Each package is a self-contained, testable deliverable; finish and verify one before starting the next.

1. **`postgres` (PG-1 → PG-7)** + **`migration` (MIG-1 → MIG-2)** — the reference implementation of the convention plus the migrator seam they share. Build `postgres` through PG-6 and `migration` together so the seam can be exercised end-to-end (PG-6's integration test and MIG-1's integration test both use `FORGE_TEST_POSTGRES_DSN`). This establishes the copied-convention shape every other package follows.
2. **`redis` (RD-1 → RD-5)** — `UniversalClient` topology selection, `IsNil`, typed `GetJSON`/`SetJSON`.
3. **`mongo` (MG-1 → MG-7)** — concern config, `Open`/lifecycle, `WithTransaction`, `EnsureIndexes`/sharding setup, error classifiers.
4. **`opensearch` (OS-1 → OS-6)** — `Open`/lifecycle, the declarative `Setup` runner, `IsNotFound`.

Within each package, the lifecycle trio (`Config` → `Open` → `Close`/`Healthcheck`) comes first; the boilerplate-reducers (transactions, setup, typed/error helpers) layer on top and land as their own commits.

## Final verification (after all packages land)

- [ ] **Whole-repo build + lint:** `just lint` (runs `go vet`, `go build -o /dev/null ./...`, `golangci-lint`, `nilaway`, `betteralign`, `modernize`). Expected: clean.
- [ ] **Whole-repo tests (no servers):** `just test`. Expected: PASS — every new package's unit tests run; all integration tests SKIP (no `FORGE_TEST_*` set). Confirm the skip count is non-zero for each backend.
- [ ] **Optional full integration run (local, with services):** export `FORGE_TEST_POSTGRES_DSN`, `FORGE_TEST_REDIS_URL`, `FORGE_TEST_MONGO_URI` (and the `_RS_`/`_SHARDED_` variants if available), `FORGE_TEST_OPENSEARCH_ADDR`, then `just test`. Expected: the previously-skipped integration tests now run and PASS. This is how CI exercises them via GitHub Actions **service containers**; it is never required for a green local default run.
- [ ] **Spec cross-check:** open [`docs/superpowers/specs/2026-06-27-database-packages-design.md`](../specs/2026-06-27-database-packages-design.md) and confirm every public symbol it lists exists with the documented signature (the per-package `doc.go` env tables are the quickest checklist).
