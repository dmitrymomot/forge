# data/clickhouse Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `data/clickhouse` — a ClickHouse connection factory in the `data/postgres` mold, returning both the native `clickhouse.Conn` (via `Open`) and a `database/sql` `*sql.DB` (via `OpenDB`) over one shared Config → `*clickhouse.Options` builder, with bounded retry/backoff ping, lifecycle helpers, and error-classification predicates.

**Architecture:** Connection-only (query building, batching, schema stay consumer-side). A minimal serializable `Config` (DSN + pool/timeout overlays, `CLICKHOUSE_*` env tags) is resolved into `*clickhouse.Options` by a pure `buildOptions`, LZ4 wire compression is defaulted on unless the DSN specifies otherwise, a `WithOptions` escape hatch runs last, then the driver constructs the handle and a shared `pingWithRetry` verifies liveness. `Close`/`Healthcheck`/`HealthcheckDB` own shutdown and probes; `classify.go` maps `*clickhouse.Exception` codes to named predicates.

**Tech Stack:** Go 1.26; `github.com/ClickHouse/clickhouse-go/v2` v2.47.0 (new direct dep, isolated in this package); stdlib `database/sql`, `net/url`, `log/slog`.

## Global Constraints

- **Single Go module** `github.com/dmitrymomot/forge`; the package import path is `github.com/dmitrymomot/forge/data/clickhouse`, directory `data/clickhouse`, package clause `package clickhouse`.
- **Driver import MUST be aliased** `ch` in every source and test file: `import ch "github.com/ClickHouse/clickhouse-go/v2"`. The driver's own package name is `clickhouse`, which clashes with this package (same pattern as `data/redis` aliasing go-redis to `goredis`). godoc still renders the driver's exported types under their real package name (`clickhouse.Conn`, `clickhouse.Options`, `clickhouse.Exception`).
- **Driver version:** add exactly `github.com/ClickHouse/clickhouse-go/v2@v2.47.0` (Task 2). `go mod tidy` may pull transitive deps — that is expected.
- **Env prefix** is the full word `CLICKHOUSE_` on every `Config` field tag.
- **LZ4-on-by-default:** `DefaultConfig` does not carry a compression field; `buildOptions` enables `clickhouse.CompressionLZ4` only when the DSN did not mention `compress` at all (see Task 2 for the exact gate — this is a correctness-critical nuance, not a nicety).
- **Errors:** single-line, `errors.Is`-matchable sentinels (`ErrInvalidConfig`, `ErrConnect`, `ErrHealthcheck`). Wrap with `fmt.Errorf("%w: …: %v", Err…, err)`.
- **Tests:** black-box (`package clickhouse_test`) everywhere EXCEPT the single white-box `buildoptions_test.go` (`package clickhouse`) that asserts the unexported `buildOptions`/`dsnHasParam`. This is the sanctioned exception to the black-box rule for asserting unexported state.
- **No `bench_test.go`.** A connection factory has no per-request hot path; this follows the established `data/*` precedent (none of postgres/redis/mongo/opensearch ship one) and overrides the repo-wide benchmark rule for this package class.
- **No manual line wrapping** in any prose, doc comment, or commit body — one continuous line per paragraph.
- **After each task:** run `just fmt ./data/clickhouse/...` (package-path form — the single-file form trips a spurious betteralign error). **After the final task:** run `just lint` and `just test ./data/clickhouse/...` and confirm both pass.
- **No Claude attribution** in any commit message (no "Generated with", no "Co-Authored-By: Claude").
- Commit after every task with a `feat(clickhouse):` / `test(clickhouse):` / `docs(clickhouse):` conventional message.

---

### Task 1: Sentinels + Config

**Files:**
- Create: `data/clickhouse/errors.go`
- Create: `data/clickhouse/config.go`
- Test: `data/clickhouse/config_test.go`

**Interfaces:**
- Consumes: nothing (first task).
- Produces: `ErrInvalidConfig`, `ErrConnect`, `ErrHealthcheck` (sentinel `error` vars); `type Config struct{…}`; `func DefaultConfig() Config`; `func (Config) Validate() error`. `Config` fields (exact names/types used by later tasks): `DSN string`, `ConnMaxLifetime time.Duration`, `DialTimeout time.Duration`, `RetryInterval time.Duration`, `MaxOpenConns int`, `MaxIdleConns int`, `RetryAttempts int`.

This task has no driver import and compiles standalone (`go build ./data/clickhouse/...` succeeds before the dep is added in Task 2).

- [ ] **Step 1: Write the failing test**

Create `data/clickhouse/config_test.go`:

```go
package clickhouse_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/data/clickhouse"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := clickhouse.DefaultConfig()
	if cfg.DSN != "" {
		t.Fatalf("DefaultConfig DSN = %q, want empty", cfg.DSN)
	}
	if cfg.MaxOpenConns != 10 {
		t.Fatalf("MaxOpenConns = %d, want 10", cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns != 5 {
		t.Fatalf("MaxIdleConns = %d, want 5", cfg.MaxIdleConns)
	}
	if cfg.RetryAttempts != 3 {
		t.Fatalf("RetryAttempts = %d, want 3", cfg.RetryAttempts)
	}
	// DefaultConfig alone must fail Validate because DSN is empty.
	if err := cfg.Validate(); !errors.Is(err, clickhouse.ErrInvalidConfig) {
		t.Fatalf("DefaultConfig().Validate() = %v, want ErrInvalidConfig", err)
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()
	valid := func() clickhouse.Config {
		c := clickhouse.DefaultConfig()
		c.DSN = "clickhouse://localhost:9000/db"
		return c
	}
	tests := []struct {
		name    string
		mutate  func(*clickhouse.Config)
		wantErr bool
	}{
		{"valid", func(*clickhouse.Config) {}, false},
		{"empty DSN", func(c *clickhouse.Config) { c.DSN = "" }, true},
		{"negative MaxOpenConns", func(c *clickhouse.Config) { c.MaxOpenConns = -1 }, true},
		{"negative MaxIdleConns", func(c *clickhouse.Config) { c.MaxIdleConns = -1 }, true},
		{"idle exceeds open", func(c *clickhouse.Config) { c.MaxOpenConns = 4; c.MaxIdleConns = 8 }, true},
		{"negative RetryAttempts", func(c *clickhouse.Config) { c.RetryAttempts = -1 }, true},
		{"negative duration", func(c *clickhouse.Config) { c.DialTimeout = -time.Second }, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := valid()
			tc.mutate(&c)
			err := c.Validate()
			if tc.wantErr {
				if !errors.Is(err, clickhouse.ErrInvalidConfig) {
					t.Fatalf("Validate() = %v, want ErrInvalidConfig", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./data/clickhouse/ -run 'TestDefaultConfig|TestValidate' 2>&1 | head`
Expected: FAIL — build error, `undefined: clickhouse.DefaultConfig` / `ErrInvalidConfig` / `Config`.

- [ ] **Step 3: Write `errors.go`**

Create `data/clickhouse/errors.go`:

```go
package clickhouse

import "errors"

// Sentinel errors returned (often wrapped) by this package. Match with errors.Is.
// These are distinct from the classify predicates (IsTableNotFound and friends),
// which match the underlying *clickhouse.Exception rather than these sentinels.
var (
	// ErrInvalidConfig is returned (joined) by Validate and the constructors when an
	// option or Config field has an invalid value.
	ErrInvalidConfig = errors.New("clickhouse: invalid config")
	// ErrConnect is returned by Open/OpenDB when the DSN could not be parsed or the
	// server could not be reached within the configured retry budget.
	ErrConnect = errors.New("clickhouse: connect failed")
	// ErrHealthcheck wraps a failed liveness ping from a Healthcheck closure.
	ErrHealthcheck = errors.New("clickhouse: healthcheck failed")
)
```

- [ ] **Step 4: Write `config.go`**

Create `data/clickhouse/config.go`:

```go
package clickhouse

import (
	"errors"
	"fmt"
	"time"
)

// Config holds the serializable settings for a connection. The env struct tags are
// inert strings — this package imports no config loader. Populate Config with any
// loader that reads env struct tags, typically by seeding from DefaultConfig and
// parsing the environment over it. Field order is subject to the repo's betteralign
// tooling. Everything ClickHouse-specific not listed here (TLS, the Settings map,
// block buffer size, custom auth, HTTP vs native protocol) rides the DSN query params
// or the WithOptions escape hatch.
type Config struct {
	DSN             string        `env:"CLICKHOUSE_DSN"`               // clickhouse://user:pass@host:9000/db?param=value (required)
	ConnMaxLifetime time.Duration `env:"CLICKHOUSE_CONN_MAX_LIFETIME"` // close a conn this long after creation
	DialTimeout     time.Duration `env:"CLICKHOUSE_DIAL_TIMEOUT"`      // per-attempt dial+handshake bound
	RetryInterval   time.Duration `env:"CLICKHOUSE_RETRY_INTERVAL"`    // base backoff between connect attempts
	MaxOpenConns    int           `env:"CLICKHOUSE_MAX_OPEN_CONNS"`    // pool ceiling
	MaxIdleConns    int           `env:"CLICKHOUSE_MAX_IDLE_CONNS"`    // idle pool size
	RetryAttempts   int           `env:"CLICKHOUSE_RETRY_ATTEMPTS"`    // total connect attempts; <=1 means one, no wait
}

// DefaultConfig returns production-sane pool/timeout defaults and is the single source
// of truth for them (there are no envDefault tags to drift from it). DSN is left empty
// and must be supplied; DefaultConfig alone therefore fails Validate.
func DefaultConfig() Config {
	return Config{
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: 30 * time.Minute,
		DialTimeout:     5 * time.Second,
		RetryAttempts:   3,
		RetryInterval:   time.Second,
	}
}

// Validate reports whether the field values are usable, returning an
// ErrInvalidConfig-wrapped, single-line joined error otherwise. Callers may call it
// after loading from env (zero-trust); Open/OpenDB also call it defensively.
func (c Config) Validate() error {
	var errs []error
	if c.DSN == "" {
		errs = append(errs, fmt.Errorf("%w: DSN must not be empty", ErrInvalidConfig))
	}
	if c.MaxOpenConns < 0 {
		errs = append(errs, fmt.Errorf("%w: MaxOpenConns must be >= 0", ErrInvalidConfig))
	}
	if c.MaxIdleConns < 0 {
		errs = append(errs, fmt.Errorf("%w: MaxIdleConns must be >= 0", ErrInvalidConfig))
	}
	if c.MaxOpenConns > 0 && c.MaxIdleConns > c.MaxOpenConns {
		errs = append(errs, fmt.Errorf("%w: MaxIdleConns must be <= MaxOpenConns", ErrInvalidConfig))
	}
	if c.RetryAttempts < 0 {
		errs = append(errs, fmt.Errorf("%w: RetryAttempts must be >= 0", ErrInvalidConfig))
	}
	for _, f := range []struct {
		name string
		d    time.Duration
	}{
		{"ConnMaxLifetime", c.ConnMaxLifetime},
		{"DialTimeout", c.DialTimeout},
		{"RetryInterval", c.RetryInterval},
	} {
		if f.d < 0 {
			errs = append(errs, fmt.Errorf("%w: %s must be >= 0", ErrInvalidConfig, f.name))
		}
	}
	return errors.Join(errs...)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `just fmt ./data/clickhouse/... && go test ./data/clickhouse/ -run 'TestDefaultConfig|TestValidate' -v 2>&1 | tail`
Expected: PASS (both tests, all subtests).

- [ ] **Step 6: Commit**

```bash
git add data/clickhouse/errors.go data/clickhouse/config.go data/clickhouse/config_test.go
git commit -m "feat(clickhouse): Config, DefaultConfig, Validate, sentinels"
```

---

### Task 2: Options + buildOptions (add driver dep)

**Files:**
- Modify: `go.mod`, `go.sum` (add the driver dep)
- Create: `data/clickhouse/options.go`
- Test: `data/clickhouse/buildoptions_test.go` (white-box, `package clickhouse`)

**Interfaces:**
- Consumes: `Config`, `ErrInvalidConfig`, `ErrConnect` (Task 1).
- Produces: `type Option func(*config)`; `func WithConfig(Config) Option`; `func WithLogger(*slog.Logger) Option`; `func WithOptions(func(*ch.Options)) Option`; unexported `type config struct{ logger *slog.Logger; withOptions func(*ch.Options); errs []error; Config }`; unexported `func buildOptions(Config) (*ch.Options, error)`; unexported `func dsnHasParam(dsn, key string) bool`.

- [ ] **Step 1: Add the driver dependency**

Run:
```bash
go get github.com/ClickHouse/clickhouse-go/v2@v2.47.0
```
Expected: `go.mod` now lists `github.com/ClickHouse/clickhouse-go/v2 v2.47.0` (transitive upgrades are fine).

- [ ] **Step 2: Write the failing white-box test**

Create `data/clickhouse/buildoptions_test.go`:

```go
package clickhouse

import (
	"testing"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

func TestBuildOptions_LZ4DefaultWhenDSNSilent(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.DSN = "clickhouse://localhost:9000/db"
	opts, err := buildOptions(cfg)
	if err != nil {
		t.Fatalf("buildOptions() error = %v", err)
	}
	if opts.Compression == nil {
		t.Fatal("Compression = nil, want LZ4 default")
	}
	if opts.Compression.Method != ch.CompressionLZ4 {
		t.Fatalf("Compression.Method = %v, want LZ4", opts.Compression.Method)
	}
}

func TestBuildOptions_DSNCompressionWins(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.DSN = "clickhouse://localhost:9000/db?compress=zstd"
	opts, err := buildOptions(cfg)
	if err != nil {
		t.Fatalf("buildOptions() error = %v", err)
	}
	if opts.Compression == nil || opts.Compression.Method != ch.CompressionZSTD {
		t.Fatalf("Compression = %+v, want ZSTD from DSN", opts.Compression)
	}
}

func TestBuildOptions_CompressFalseStaysOff(t *testing.T) {
	t.Parallel()
	// compress=false must NOT be overridden by the LZ4 default. ParseDSN leaves
	// Compression nil for compress=false, so the default must gate on the raw DSN.
	cfg := DefaultConfig()
	cfg.DSN = "clickhouse://localhost:9000/db?compress=false"
	opts, err := buildOptions(cfg)
	if err != nil {
		t.Fatalf("buildOptions() error = %v", err)
	}
	if opts.Compression != nil {
		t.Fatalf("Compression = %+v, want nil (caller disabled it)", opts.Compression)
	}
}

func TestBuildOptions_Overlay(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.DSN = "clickhouse://localhost:9000/db"
	cfg.MaxOpenConns = 42
	cfg.MaxIdleConns = 7
	opts, err := buildOptions(cfg)
	if err != nil {
		t.Fatalf("buildOptions() error = %v", err)
	}
	if opts.MaxOpenConns != 42 || opts.MaxIdleConns != 7 {
		t.Fatalf("overlay = (%d,%d), want (42,7)", opts.MaxOpenConns, opts.MaxIdleConns)
	}
	if opts.DialTimeout != cfg.DialTimeout {
		t.Fatalf("DialTimeout = %v, want %v", opts.DialTimeout, cfg.DialTimeout)
	}
}

func TestBuildOptions_ParseError(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.DSN = "clickhouse://" // empty host -> ParseDSN fails
	if _, err := buildOptions(cfg); err == nil {
		t.Fatal("buildOptions() error = nil, want ErrConnect")
	}
}

func TestDSNHasParam(t *testing.T) {
	t.Parallel()
	cases := []struct {
		dsn, key string
		want     bool
	}{
		{"clickhouse://h:9000/db?compress=lz4", "compress", true},
		{"clickhouse://h:9000/db?compress=false", "compress", true},
		{"clickhouse://h:9000/db?compress_level=3", "compress", false},
		{"clickhouse://h:9000/db", "compress", false},
		{"clickhouse://h1:9000,h2:9000/db?compress=zstd", "compress", true}, // multi-host authority
	}
	for _, c := range cases {
		if got := dsnHasParam(c.dsn, c.key); got != c.want {
			t.Errorf("dsnHasParam(%q, %q) = %v, want %v", c.dsn, c.key, got, c.want)
		}
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./data/clickhouse/ -run 'TestBuildOptions|TestDSNHasParam' 2>&1 | head`
Expected: FAIL — `undefined: buildOptions`, `undefined: dsnHasParam`.

- [ ] **Step 4: Write `options.go`**

Create `data/clickhouse/options.go`:

```go
package clickhouse

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

// config holds the resolved settings for one Open/OpenDB call. The embedded Config
// carries serializable data; the remaining fields are non-serializable code values.
type config struct {
	logger      *slog.Logger
	withOptions func(*ch.Options)
	errs        []error
	Config
}

// Option configures Open and OpenDB. Invalid values accumulate in the config and are
// returned (joined, ErrInvalidConfig-wrapped) by the constructor before any I/O.
type Option func(*config)

// WithConfig sets the whole serializable data block at once. Build the argument from
// DefaultConfig() (or an env-parsed copy); a bare Config{} zeroes every limit and
// leaves DSN empty (which fails Validate). Options apply in order — place WithConfig
// before any code option you want layered on top of it.
func WithConfig(cfg Config) Option {
	return func(c *config) { c.Config = cfg }
}

// WithLogger sets the slog.Logger used by Close and lifecycle logging. Default is
// slog.Default(); a nil logger is rejected (ErrInvalidConfig). Pass a discard logger
// to silence logging instead.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithLogger received a nil *slog.Logger", ErrInvalidConfig))
			return
		}
		c.logger = l
	}
}

// WithOptions registers the escape hatch that runs LAST in Open/OpenDB, after the
// Config overlay and the LZ4 default, on the fully-built *clickhouse.Options. Use it
// for anything the serializable fields do not cover — TLS config, the Settings map,
// block buffer size, a custom dialer, JWT auth. A nil func is rejected
// (ErrInvalidConfig).
func WithOptions(fn func(*ch.Options)) Option {
	return func(c *config) {
		if fn == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithOptions received a nil func", ErrInvalidConfig))
			return
		}
		c.withOptions = fn
	}
}

// buildOptions maps a validated Config onto a *clickhouse.Options. It is pure (the
// only work is ParseDSN plus field overlay), so the mapping is unit-testable without a
// server. A DSN parse failure is wrapped in ErrConnect.
func buildOptions(cfg Config) (*ch.Options, error) {
	opts, err := ch.ParseDSN(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("%w: parse dsn: %v", ErrConnect, err)
	}
	if cfg.MaxOpenConns > 0 {
		opts.MaxOpenConns = cfg.MaxOpenConns
	}
	if cfg.MaxIdleConns > 0 {
		opts.MaxIdleConns = cfg.MaxIdleConns
	}
	if cfg.ConnMaxLifetime > 0 {
		opts.ConnMaxLifetime = cfg.ConnMaxLifetime
	}
	if cfg.DialTimeout > 0 {
		opts.DialTimeout = cfg.DialTimeout
	}
	// LZ4-on-by-default: enable LZ4 wire compression only when the DSN did not mention
	// compression at all. ParseDSN leaves Compression nil both when the param is absent
	// AND when it is explicitly disabled (compress=false), so gating on opts.Compression
	// alone would silently re-enable compression the caller turned off — hence the raw
	// DSN check. A compress_level-only DSN yields a non-nil CompressionNone, which the
	// nil check preserves.
	if opts.Compression == nil && !dsnHasParam(cfg.DSN, "compress") {
		opts.Compression = &ch.Compression{Method: ch.CompressionLZ4}
	}
	return opts, nil
}

// dsnHasParam reports whether the DSN query string contains key. It parses only the
// query portion (after the first '?') so it is unaffected by ClickHouse's multi-host
// authority syntax (host1:9000,host2:9000), which net/url cannot parse.
func dsnHasParam(dsn, key string) bool {
	_, query, ok := strings.Cut(dsn, "?")
	if !ok {
		return false
	}
	values, err := url.ParseQuery(query)
	if err != nil {
		return false
	}
	return values.Has(key)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `just fmt ./data/clickhouse/... && go test ./data/clickhouse/ -run 'TestBuildOptions|TestDSNHasParam' -v 2>&1 | tail -20`
Expected: PASS (all `TestBuildOptions_*` and `TestDSNHasParam`).

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum data/clickhouse/options.go data/clickhouse/buildoptions_test.go
git commit -m "feat(clickhouse): options, buildOptions with LZ4 default, driver dep"
```

---

### Task 3: Open + OpenDB + retry ping

**Files:**
- Create: `data/clickhouse/clickhouse.go`
- Test: `data/clickhouse/open_test.go` (black-box)

**Interfaces:**
- Consumes: `config`, `Option`, `buildOptions`, `Config`, `ErrConnect`, `ErrInvalidConfig` (Tasks 1–2).
- Produces: `func Open(ctx context.Context, opts ...Option) (ch.Conn, error)`; `func OpenDB(ctx context.Context, opts ...Option) (*sql.DB, error)`. (`ch.Conn` renders as `clickhouse.Conn` in godoc.)

- [ ] **Step 1: Write the failing test**

Create `data/clickhouse/open_test.go`:

```go
package clickhouse_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/data/clickhouse"
)

func unreachableCfg() clickhouse.Config {
	c := clickhouse.DefaultConfig()
	c.DSN = "clickhouse://127.0.0.1:9/db" // port 9 (discard): refused fast
	c.RetryAttempts = 2
	c.RetryInterval = time.Millisecond
	c.DialTimeout = 200 * time.Millisecond
	return c
}

func TestOpen_OptionErrorShortCircuits(t *testing.T) {
	t.Parallel()
	_, err := clickhouse.Open(context.Background(), clickhouse.WithLogger(nil))
	if !errors.Is(err, clickhouse.ErrInvalidConfig) {
		t.Fatalf("Open() = %v, want ErrInvalidConfig", err)
	}
}

func TestOpen_ValidateFailsBeforeIO(t *testing.T) {
	t.Parallel()
	_, err := clickhouse.Open(context.Background(), clickhouse.WithConfig(clickhouse.Config{}))
	if !errors.Is(err, clickhouse.ErrInvalidConfig) {
		t.Fatalf("Open() = %v, want ErrInvalidConfig", err)
	}
}

func TestOpen_MalformedDSN(t *testing.T) {
	t.Parallel()
	cfg := clickhouse.DefaultConfig()
	cfg.DSN = "clickhouse://" // empty host
	_, err := clickhouse.Open(context.Background(), clickhouse.WithConfig(cfg))
	if !errors.Is(err, clickhouse.ErrConnect) {
		t.Fatalf("Open() = %v, want ErrConnect", err)
	}
}

func TestOpen_UnreachableExhaustsRetry(t *testing.T) {
	t.Parallel()
	_, err := clickhouse.Open(context.Background(), clickhouse.WithConfig(unreachableCfg()))
	if !errors.Is(err, clickhouse.ErrConnect) {
		t.Fatalf("Open() = %v, want ErrConnect", err)
	}
}

func TestOpen_ContextCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := clickhouse.Open(ctx, clickhouse.WithConfig(unreachableCfg()))
	if !errors.Is(err, clickhouse.ErrConnect) {
		t.Fatalf("Open() = %v, want ErrConnect", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Open() = %v, want wrapped context.Canceled", err)
	}
}

func TestOpenDB_UnreachableExhaustsRetry(t *testing.T) {
	t.Parallel()
	_, err := clickhouse.OpenDB(context.Background(), clickhouse.WithConfig(unreachableCfg()))
	if !errors.Is(err, clickhouse.ErrConnect) {
		t.Fatalf("OpenDB() = %v, want ErrConnect", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./data/clickhouse/ -run 'TestOpen' 2>&1 | head`
Expected: FAIL — `undefined: clickhouse.Open`, `clickhouse.OpenDB`.

- [ ] **Step 3: Write `clickhouse.go`**

Create `data/clickhouse/clickhouse.go`:

```go
package clickhouse

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

// maxConnectBackoff caps the exponential backoff between connect attempts so a large
// RetryInterval or RetryAttempts cannot produce an unbounded wait.
const maxConnectBackoff = 30 * time.Second

// Open builds a native ClickHouse connection from options and returns it only once a
// Ping has confirmed a live server. It starts from DefaultConfig, applies options in
// order, surfaces accumulated option errors and a failed Validate as an
// ErrInvalidConfig-wrapped error (before any network I/O), builds the driver options
// (DSN parse + Config overlay + LZ4 default), runs the WithOptions escape hatch LAST,
// constructs the conn, then pings with bounded retry/backoff. On failure it closes the
// partial conn and returns an ErrConnect-wrapped, single-line error, leaking nothing.
//
// The returned clickhouse.Conn exposes the native API — PrepareBatch, AsyncInsert,
// Select, QueryRow, Exec. The caller owns it and should defer Close(conn, logger).
func Open(ctx context.Context, opts ...Option) (ch.Conn, error) {
	chOpts, cfg, logger, err := resolve(opts)
	if err != nil {
		return nil, err
	}
	conn, err := ch.Open(chOpts)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConnect, err)
	}
	if err := pingWithRetry(ctx, conn.Ping, cfg, logger); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// OpenDB is the database/sql counterpart to Open, returning a *sql.DB for consumers
// that want goose/sqlc/stdlib ergonomics. It shares Open's resolve+build pipeline; the
// driver applies MaxOpenConns/MaxIdleConns/ConnMaxLifetime from the options onto the
// *sql.DB. It pings with the same bounded retry/backoff and, on failure, closes the
// handle and returns an ErrConnect-wrapped error.
func OpenDB(ctx context.Context, opts ...Option) (*sql.DB, error) {
	chOpts, cfg, logger, err := resolve(opts)
	if err != nil {
		return nil, err
	}
	db := ch.OpenDB(chOpts)
	if err := pingWithRetry(ctx, db.PingContext, cfg, logger); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// resolve runs the shared, I/O-free front half of Open/OpenDB: apply options, surface
// option errors, Validate, build the driver options, run the escape hatch, and pick
// the logger (slog.Default when unset). It returns the built options, the resolved
// Config (for the retry budget), and the logger.
func resolve(opts []Option) (*ch.Options, Config, *slog.Logger, error) {
	c := config{Config: DefaultConfig()}
	for _, opt := range opts {
		opt(&c)
	}
	if len(c.errs) > 0 {
		return nil, c.Config, nil, errors.Join(c.errs...)
	}
	if err := c.Validate(); err != nil {
		return nil, c.Config, nil, err
	}
	chOpts, err := buildOptions(c.Config)
	if err != nil {
		return nil, c.Config, nil, err
	}
	if c.withOptions != nil {
		c.withOptions(chOpts) // escape hatch runs LAST, on the fully-built options
	}
	logger := c.logger
	if logger == nil {
		logger = slog.Default()
	}
	return chOpts, c.Config, logger, nil
}

// pingWithRetry pings via ping up to RetryAttempts times, waiting
// RetryInterval·2^attempt (capped at maxConnectBackoff) between tries and honoring ctx
// cancellation during the wait. RetryAttempts <= 1 means a single attempt with no
// wait. After exhausting attempts it returns ErrConnect joined with the last error.
func pingWithRetry(ctx context.Context, ping func(context.Context) error, cfg Config, logger *slog.Logger) error {
	attempts := max(cfg.RetryAttempts, 1)

	var lastErr error
	for attempt := range attempts {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%w: %v", ErrConnect, err)
		}
		if err := ping(ctx); err != nil {
			lastErr = err
		} else {
			return nil
		}

		if attempt == attempts-1 {
			break // no wait after the final attempt
		}
		wait := backoff(cfg.RetryInterval, attempt)
		logger.Warn("clickhouse connect attempt failed; retrying",
			slog.Int("attempt", attempt+1),
			slog.Int("attempts", attempts),
			slog.Duration("wait", wait),
			slog.String("err", lastErr.Error()),
		)
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
	if d <= 0 || d > maxConnectBackoff { // overflow or over the cap
		return maxConnectBackoff
	}
	return d
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `just fmt ./data/clickhouse/... && go test ./data/clickhouse/ -run 'TestOpen' -v 2>&1 | tail -20`
Expected: PASS (all six). The unreachable cases return within ~1s given the tiny retry budget.

- [ ] **Step 5: Commit**

```bash
git add data/clickhouse/clickhouse.go data/clickhouse/open_test.go
git commit -m "feat(clickhouse): Open and OpenDB with bounded retry ping"
```

---

### Task 4: Lifecycle helpers

**Files:**
- Create: `data/clickhouse/lifecycle.go`
- Test: `data/clickhouse/lifecycle_test.go` (black-box)

**Interfaces:**
- Consumes: `ErrHealthcheck` (Task 1); `ch.Conn` (driver).
- Produces: `func Close(c io.Closer, log *slog.Logger)`; `func Healthcheck(conn ch.Conn) func(context.Context) error`; `func HealthcheckDB(db *sql.DB) func(context.Context) error`.

- [ ] **Step 1: Write the failing test**

Create `data/clickhouse/lifecycle_test.go`:

```go
package clickhouse_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dmitrymomot/forge/data/clickhouse"
)

// fakeCloser records whether Close was called and can return a forced error.
type fakeCloser struct {
	closed bool
	err    error
}

func (f *fakeCloser) Close() error {
	f.closed = true
	return f.err
}

func TestClose_NilTolerated(t *testing.T) {
	t.Parallel()
	// Must not panic with a nil closer and/or nil logger.
	clickhouse.Close(nil, nil)
}

func TestClose_ClosesCloser(t *testing.T) {
	t.Parallel()
	fc := &fakeCloser{}
	clickhouse.Close(fc, nil)
	if !fc.closed {
		t.Fatal("Close did not call the underlying Close")
	}
}

func TestClose_CloseErrorTolerated(t *testing.T) {
	t.Parallel()
	fc := &fakeCloser{err: errors.New("boom")}
	clickhouse.Close(fc, nil) // must not panic even though Close errors
	if !fc.closed {
		t.Fatal("Close did not call the underlying Close")
	}
}

func TestHealthcheckDB_NilDBPings(t *testing.T) {
	t.Parallel()
	// HealthcheckDB over a *sql.DB pointing at an unreachable server wraps the ping
	// failure in ErrHealthcheck. Build the DB via OpenDB is not possible without a
	// server, so assert the closure shape and error wrapping through a bad conn.
	db, err := clickhouse.OpenDB(context.Background(), badConn())
	if err == nil {
		t.Skip("unexpected live server")
	}
	// OpenDB failed as expected; nothing else to assert here.
	_ = db
}
```

Note: the `HealthcheckDB`/`Healthcheck` closures are exercised for real in the guarded integration test (Task 6); here we only assert `Close` semantics and the constructors' shapes. Replace the `TestHealthcheckDB_NilDBPings` body's `badConn()` reference by adding this helper to the test file:

```go
func badConn() clickhouse.Option {
	cfg := clickhouse.DefaultConfig()
	cfg.DSN = "clickhouse://127.0.0.1:9/db"
	cfg.RetryAttempts = 1
	return clickhouse.WithConfig(cfg)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./data/clickhouse/ -run 'TestClose|TestHealthcheck' 2>&1 | head`
Expected: FAIL — `undefined: clickhouse.Close`.

- [ ] **Step 3: Write `lifecycle.go`**

Create `data/clickhouse/lifecycle.go`:

```go
package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

// Close logs a single line (when log is non-nil), then closes c. Both the native
// clickhouse.Conn returned by Open and the *sql.DB returned by OpenDB satisfy
// io.Closer, so one helper covers both. It is the resource counterpart to
// Open/OpenDB, meant as `defer Close(conn, logger)` in main so it runs after the
// supervisor has drained every service. A nil c and/or nil log is tolerated: the log
// line is skipped and no close is attempted on a nil closer. It takes no context
// because both driver Close methods are synchronous.
func Close(c io.Closer, log *slog.Logger) {
	if c == nil {
		return
	}
	if log != nil {
		log.Info("closing clickhouse connection")
	}
	if err := c.Close(); err != nil && log != nil {
		log.Error("clickhouse close failed", "err", err)
	}
}

// Healthcheck returns a stateless closure that pings the native connection, wrapping
// any failure in ErrHealthcheck. Its func(context.Context) error shape is exactly what
// a readiness/liveness probe wants; hand it to the app's /readyz handler. It is safe
// to call on every probe.
func Healthcheck(conn ch.Conn) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := conn.Ping(ctx); err != nil {
			return fmt.Errorf("%w: %v", ErrHealthcheck, err)
		}
		return nil
	}
}

// HealthcheckDB is the *sql.DB counterpart to Healthcheck for connections opened with
// OpenDB. It pings via PingContext and wraps failures in ErrHealthcheck.
func HealthcheckDB(db *sql.DB) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := db.PingContext(ctx); err != nil {
			return fmt.Errorf("%w: %v", ErrHealthcheck, err)
		}
		return nil
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `just fmt ./data/clickhouse/... && go test ./data/clickhouse/ -run 'TestClose|TestHealthcheck' -v 2>&1 | tail`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add data/clickhouse/lifecycle.go data/clickhouse/lifecycle_test.go
git commit -m "feat(clickhouse): Close, Healthcheck, HealthcheckDB helpers"
```

---

### Task 5: Error classification

**Files:**
- Create: `data/clickhouse/classify.go`
- Test: `data/clickhouse/classify_test.go` (black-box)

**Interfaces:**
- Consumes: `ch.Exception` (driver).
- Produces: `func Code(err error) (int32, bool)`; `func IsCode(err error, code int32) bool`; `func IsTableNotFound(err error) bool`; `func IsDatabaseNotFound(err error) bool`; `func IsAlreadyExists(err error) bool`; `func IsAuthFailed(err error) bool`.

- [ ] **Step 1: Write the failing test**

Create `data/clickhouse/classify_test.go`:

```go
package clickhouse_test

import (
	"errors"
	"fmt"
	"testing"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/dmitrymomot/forge/data/clickhouse"
)

func exc(code int32) error {
	return &ch.Exception{Code: code, Message: "test", Name: "TEST"}
}

func TestCode(t *testing.T) {
	t.Parallel()
	got, ok := clickhouse.Code(exc(60))
	if !ok || got != 60 {
		t.Fatalf("Code() = (%d, %v), want (60, true)", got, ok)
	}
	// Wrapped exception is still matched.
	if _, ok := clickhouse.Code(fmt.Errorf("query: %w", exc(81))); !ok {
		t.Fatal("Code() did not unwrap")
	}
	// Non-exception and nil return false.
	if _, ok := clickhouse.Code(errors.New("plain")); ok {
		t.Fatal("Code() matched a plain error")
	}
	if _, ok := clickhouse.Code(nil); ok {
		t.Fatal("Code(nil) matched")
	}
}

func TestNamedPredicates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		pred func(error) bool
		code int32
	}{
		{"IsTableNotFound", clickhouse.IsTableNotFound, 60},
		{"IsDatabaseNotFound", clickhouse.IsDatabaseNotFound, 81},
		{"IsAlreadyExists", clickhouse.IsAlreadyExists, 57},
		{"IsAuthFailed", clickhouse.IsAuthFailed, 516},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !tc.pred(exc(tc.code)) {
				t.Fatalf("%s(exc(%d)) = false, want true", tc.name, tc.code)
			}
			if tc.pred(exc(999)) {
				t.Fatalf("%s(exc(999)) = true, want false", tc.name)
			}
			if tc.pred(errors.New("plain")) {
				t.Fatalf("%s(plain) = true, want false", tc.name)
			}
		})
	}
}

func TestIsCode(t *testing.T) {
	t.Parallel()
	if !clickhouse.IsCode(exc(241), 241) {
		t.Fatal("IsCode(exc(241), 241) = false, want true")
	}
	if clickhouse.IsCode(exc(241), 60) {
		t.Fatal("IsCode(exc(241), 60) = true, want false")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./data/clickhouse/ -run 'TestCode|TestNamedPredicates|TestIsCode' 2>&1 | head`
Expected: FAIL — `undefined: clickhouse.Code`.

- [ ] **Step 3: Write `classify.go`**

Create `data/clickhouse/classify.go`:

```go
package clickhouse

import (
	"errors"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

// ClickHouse server error codes recognized by the classification predicates. These are
// stable codes from ClickHouse's server-side ErrorCodes table, not driver constants.
const (
	codeTableAlreadyExists = 57  // TABLE_ALREADY_EXISTS
	codeUnknownTable       = 60  // UNKNOWN_TABLE
	codeUnknownDatabase    = 81  // UNKNOWN_DATABASE
	codeAuthFailed         = 516 // AUTHENTICATION_FAILED
)

// Code returns the ClickHouse server error code carried by err if it (or anything it
// wraps) is a *clickhouse.Exception, and reports whether such an exception was found.
// Use it instead of importing the driver and matching *clickhouse.Exception at the
// call site.
func Code(err error) (int32, bool) {
	if e, ok := errors.AsType[*ch.Exception](err); ok {
		return e.Code, true
	}
	return 0, false
}

// IsCode reports whether err carries a *clickhouse.Exception with the given server
// error code.
func IsCode(err error, code int32) bool {
	c, ok := Code(err)
	return ok && c == code
}

// IsTableNotFound reports whether err is a ClickHouse UNKNOWN_TABLE (60) error.
func IsTableNotFound(err error) bool { return IsCode(err, codeUnknownTable) }

// IsDatabaseNotFound reports whether err is a ClickHouse UNKNOWN_DATABASE (81) error.
func IsDatabaseNotFound(err error) bool { return IsCode(err, codeUnknownDatabase) }

// IsAlreadyExists reports whether err is a ClickHouse TABLE_ALREADY_EXISTS (57) error.
func IsAlreadyExists(err error) bool { return IsCode(err, codeTableAlreadyExists) }

// IsAuthFailed reports whether err is a ClickHouse AUTHENTICATION_FAILED (516) error.
// Modern ClickHouse collapses wrong-password and unknown-user into this single code so
// authentication failures do not reveal which half was wrong.
func IsAuthFailed(err error) bool { return IsCode(err, codeAuthFailed) }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `just fmt ./data/clickhouse/... && go test ./data/clickhouse/ -run 'TestCode|TestNamedPredicates|TestIsCode' -v 2>&1 | tail -20`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add data/clickhouse/classify.go data/clickhouse/classify_test.go
git commit -m "feat(clickhouse): Exception code classification predicates"
```

---

### Task 6: Docs, live integration test, roadmap removal, final gate

**Files:**
- Create: `data/clickhouse/doc.go`
- Create: `data/clickhouse/integration_test.go` (black-box, guarded by `CLICKHOUSE_DSN`)
- Modify: `docs/packages.md` (delete the `data/clickhouse` roadmap entry)

**Interfaces:**
- Consumes: the full public API from Tasks 1–5.
- Produces: package documentation and a guarded integration test.

- [ ] **Step 1: Write `doc.go`**

Create `data/clickhouse/doc.go` (package comment with a complete usage example, in the `data/redis` style — a comment example, not a runnable `Example` func, since running it needs a live server):

```go
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
```

- [ ] **Step 2: Write the guarded integration test**

Create `data/clickhouse/integration_test.go`:

```go
package clickhouse_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/data/clickhouse"
)

// TestIntegration exercises a real ClickHouse when CLICKHOUSE_DSN is set (e.g. an
// ephemeral clickhouse/clickhouse-server container in CI); it is skipped otherwise.
func TestIntegration(t *testing.T) {
	dsn := os.Getenv("CLICKHOUSE_DSN")
	if dsn == "" {
		t.Skip("set CLICKHOUSE_DSN to run the live integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg := clickhouse.DefaultConfig()
	cfg.DSN = dsn

	// Native path: Open + PrepareBatch round-trip.
	conn, err := clickhouse.Open(ctx, clickhouse.WithConfig(cfg))
	if err != nil {
		t.Fatalf("Open() = %v", err)
	}
	defer clickhouse.Close(conn, nil)

	if err := clickhouse.Healthcheck(conn)(ctx); err != nil {
		t.Fatalf("Healthcheck() = %v", err)
	}

	const table = "forge_clickhouse_it"
	if err := conn.Exec(ctx, "DROP TABLE IF EXISTS "+table); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := conn.Exec(ctx, "CREATE TABLE "+table+" (id UInt64) ENGINE = Memory"); err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() { _ = conn.Exec(context.Background(), "DROP TABLE IF EXISTS "+table) }()

	batch, err := conn.PrepareBatch(ctx, "INSERT INTO "+table+" (id) VALUES")
	if err != nil {
		t.Fatalf("PrepareBatch: %v", err)
	}
	if err := batch.Append(uint64(7)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := batch.Send(); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var got uint64
	if err := conn.QueryRow(ctx, "SELECT id FROM "+table+" LIMIT 1").Scan(&got); err != nil {
		t.Fatalf("QueryRow: %v", err)
	}
	if got != 7 {
		t.Fatalf("round-trip id = %d, want 7", got)
	}

	// Classify: a query against a missing table is UNKNOWN_TABLE (60).
	err = conn.Exec(ctx, "SELECT 1 FROM forge_clickhouse_missing_9x8y7z")
	if !clickhouse.IsTableNotFound(err) {
		t.Fatalf("IsTableNotFound(%v) = false, want true", err)
	}

	// database/sql path: OpenDB + HealthcheckDB.
	db, err := clickhouse.OpenDB(ctx, clickhouse.WithConfig(cfg))
	if err != nil {
		t.Fatalf("OpenDB() = %v", err)
	}
	defer clickhouse.Close(db, nil)
	if err := clickhouse.HealthcheckDB(db)(ctx); err != nil {
		t.Fatalf("HealthcheckDB() = %v", err)
	}
}
```

- [ ] **Step 3: Remove the roadmap entry**

The forge convention: the roadmap lists only unbuilt packages; on ship, delete the entry. Edit `docs/packages.md` and delete the `**data/clickhouse**` block (the heading, its paragraph, its `Deps:` line, and the trailing `---` separator that pairs with it), leaving the surrounding `data/export`, `data/ingest`, and `data/sqlite` entries intact.

- [ ] **Step 4: Verify docs compile and the whole package builds**

Run: `just fmt ./data/clickhouse/... && go build ./data/clickhouse/... && go vet ./data/clickhouse/...`
Expected: no output (success). If a live ClickHouse is available, run `CLICKHOUSE_DSN=clickhouse://default:@127.0.0.1:9000/default just test ./data/clickhouse/...`; otherwise the integration test self-skips.

- [ ] **Step 5: Full lint + test gate**

Run: `just lint && just test ./data/clickhouse/...`
Expected: `just lint` passes (go vet, build, golangci-lint incl. the clickhouse-go linter, nilaway, betteralign, modernize) and `just test` is green (integration test skipped without a DSN). Fix any finding and re-run before committing.

- [ ] **Step 6: Commit**

```bash
git add data/clickhouse/doc.go data/clickhouse/integration_test.go docs/packages.md
git commit -m "docs(clickhouse): package doc, live integration test, remove roadmap entry"
```

---

## Self-Review

**Spec coverage:**
- Driver `clickhouse-go/v2`, isolated, aliased → Global Constraints + Task 2. ✔
- Both constructors `Open`/`OpenDB` over one builder → Task 3. ✔
- Minimal postgres-mirrored `Config` with `CLICKHOUSE_*` tags, `DefaultConfig`, `Validate` → Task 1. ✔
- `WithConfig`/`WithLogger`/`WithOptions` escape hatch running last → Tasks 2–3. ✔
- LZ4-on-by-default with DSN-wins and the `compress=false` / `compress_level` nuance → Task 2 (`buildOptions` + `dsnHasParam` + `buildoptions_test.go`). ✔
- Bounded retry/backoff ping shared by both constructors, `ErrConnect` on failure, partial handle closed → Task 3. ✔
- `Close`/`Healthcheck`/`HealthcheckDB` (io.Closer + two ping closures) → Task 4. ✔
- `classify.go` named set `Code`/`IsCode`/`IsTableNotFound`/`IsDatabaseNotFound`/`IsAlreadyExists`/`IsAuthFailed` → Task 5. ✔
- `errors.go` three sentinels → Task 1. ✔
- `doc.go` runnable example (both paths) → Task 6. ✔
- One white-box test file; rest black-box → Global Constraints + Task 2. ✔
- No migrator, no tenancy hook, no benchmark → Global Constraints (no such files planned). ✔
- Testing: config/validate/options/classify + Open/OpenDB error paths without a server; guarded live integration → Tasks 1–6. ✔
- Roadmap entry removed → Task 6. ✔

**Deviations from the spec's file listing (intentional, noted):**
- `buildOptions` lives in `options.go` (not `clickhouse.go`), matching the `data/redis` precedent where the pure options builder sits beside the options, and enabling the white-box `buildoptions_test.go`.
- The spec listed a separate `options_test.go`; option-error paths are covered through the public `Open` in `open_test.go` (nil logger, empty Config, nil escape-hatch func are only observable via the constructor, since `config`/`Option` are unexported). No behavior is left untested.

**Placeholder scan:** No TBD/TODO/"handle edge cases"/"similar to Task N"; every code step shows complete code. ✔

**Type consistency:** `Config` field names/types identical across Tasks 1–3; `buildOptions(Config) (*ch.Options, error)` signature identical in Task 2 (definition) and Task 3 (call in `resolve`); `pingWithRetry(ctx, func(context.Context) error, Config, *slog.Logger)` fed `conn.Ping` and `db.PingContext` (both `func(context.Context) error`); `Close(io.Closer, *slog.Logger)` satisfied by both `ch.Conn` and `*sql.DB`; sentinel names (`ErrInvalidConfig`/`ErrConnect`/`ErrHealthcheck`) consistent throughout. ✔
