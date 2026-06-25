# httpserver + supervisor.Config Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a graceful, zero-trust-validated `httpserver` package that runs under the existing `supervisor`, and give both packages an env-loadable `Config` struct.

**Architecture:** `httpserver.Server` wraps a `*http.Server`, implements `supervisor.Service` (`Name()` + `Run(ctx) error`), serves on a listener it owns, and on ctx-cancel drains in-flight requests within its own deadline before force-closing. Both packages split serializable data into an exported `Config` (with `DefaultConfig`/`Validate`) while loggers/listeners/callbacks stay functional options. All option and `Config` input is validated; failures accumulate and surface at `Run`.

**Tech Stack:** Go 1.26, stdlib only (`net/http`, `crypto/tls`, `context`, `log/slog`), `testify` for tests, `just` task runner.

## Global Constraints

- **Go 1.26**, module `github.com/dmitrymomot/forge`.
- **Production code: stdlib only.** No new third-party dependencies.
- **Tests: `testify` only.** Do NOT import `github.com/caarlos0/env/v11` (or any loader) even in tests — verify the env-tag contract by reflection.
- **Flat layout** — no nested directories under each package.
- **Options, not builders.** `Option func(*config)`; no exported builder/runtime type.
- **Public methods must not return unexported types.**
- **Errors are single-line and `errors.Is`-matchable;** multi-line diagnostics (stacks, name lists) go through `slog` attributes, never embedded in an error string.
- **Work on the `main` branch.**
- **Verification recipes:** `just test` (race + cover), `just lint` (vet, build, golangci-lint, nilaway, betteralign, modernize), `just fmt` (gofmt + goimports + betteralign -apply), `just check` (fmt + lint + test). Run `just fmt` after adding structs so `betteralign` orders fields; struct field order in this plan is illustrative and may be reordered by the tool.
- **Spec:** `docs/superpowers/specs/2026-06-25-httpserver-design.md`.

---

## Task Overview

Supervisor (additive, do first — establishes the Config convention):
1. `supervisor.Config` + `DefaultConfig` + `Validate` + `ErrInvalidConfig`
2. Embed `Config` into the internal config; add `WithConfig`; remove duplicate const; update `supervisor.go` and existing tests
3. Zero-trust validation wiring in `supervisor` options + `Run`

httpserver (new package):
4. `httpserver` errors + `Config` + `DefaultConfig` + `Validate`
5. `httpserver` options (internal config + all `With*`)
6. `Server`, `New`, `Name`, `resolveLogger`
7. `Run` — serve + graceful drain + force-close (non-TLS)
8. `Run` — TLS serving + precedence
9. `doc.go` + final full verification

---

### Task 1: supervisor.Config + DefaultConfig + Validate

**Files:**
- Create: `supervisor/config.go`
- Modify: `supervisor/errors.go`
- Test: `supervisor/config_test.go`

**Interfaces:**
- Produces: `supervisor.Config{ ShutdownTimeout time.Duration; Recover bool }`; `supervisor.DefaultConfig() Config`; `func (c Config) Validate() error`; `supervisor.ErrInvalidConfig`.

- [ ] **Step 1: Add the sentinel error**

In `supervisor/errors.go`, add `ErrInvalidConfig` to the existing `var (...)` block (place it after `ErrUnnamedService`):

```go
	// ErrInvalidConfig is returned (joined) by Run when an option or Config field has an invalid value.
	ErrInvalidConfig = errors.New("supervisor: invalid config")
```

- [ ] **Step 2: Write the failing test**

Create `supervisor/config_test.go`:

```go
package supervisor

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExportedDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, 30*time.Second, cfg.ShutdownTimeout)
	assert.True(t, cfg.Recover)
}

func TestConfig_Validate(t *testing.T) {
	require.NoError(t, DefaultConfig().Validate())
	require.NoError(t, Config{ShutdownTimeout: 0, Recover: false}.Validate())

	err := Config{ShutdownTimeout: -1}.Validate()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestConfig_EnvTags(t *testing.T) {
	want := map[string]string{
		"ShutdownTimeout": "SHUTDOWN_TIMEOUT",
		"Recover":         "RECOVER",
	}
	typ := reflect.TypeFor[Config]()
	for name, tag := range want {
		f, ok := typ.FieldByName(name)
		require.Truef(t, ok, "field %s missing", name)
		assert.Equalf(t, tag, f.Tag.Get("env"), "field %s env tag", name)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./supervisor/ -run 'ExportedDefaultConfig|Config_Validate|Config_EnvTags' -v`
Expected: FAIL — `undefined: DefaultConfig` / `Config`.

- [ ] **Step 4: Create config.go**

Create `supervisor/config.go`:

```go
package supervisor

import (
	"fmt"
	"time"
)

// Config holds the serializable settings for Run. The env struct tags are inert
// strings — this package imports no config loader. Populate Config with any loader
// that reads env struct tags, typically by seeding from DefaultConfig and parsing
// the environment over it.
type Config struct {
	// ShutdownTimeout bounds how long Run waits for services to drain after
	// shutdown begins. 0 means wait indefinitely.
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT"`
	// Recover toggles panic recovery in each service's Run.
	Recover bool `env:"RECOVER"`
}

// DefaultConfig returns the optimal defaults and is the single source of truth for
// them (there are no envDefault tags to drift from it).
func DefaultConfig() Config {
	return Config{
		ShutdownTimeout: 30 * time.Second,
		Recover:         true,
	}
}

// Validate reports whether the field values are usable, returning an
// ErrInvalidConfig-wrapped error otherwise. Callers may call it after loading from
// env (zero-trust); Run also calls it defensively.
func (c Config) Validate() error {
	if c.ShutdownTimeout < 0 {
		return fmt.Errorf("%w: ShutdownTimeout must be >= 0", ErrInvalidConfig)
	}
	return nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./supervisor/ -run 'ExportedDefaultConfig|Config_Validate|Config_EnvTags' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
just fmt
git add supervisor/config.go supervisor/errors.go supervisor/config_test.go
git commit -m "feat(supervisor): add env-loadable Config, DefaultConfig, Validate"
```

---

### Task 2: Embed Config into internal config; add WithConfig; remove duplicate const

**Files:**
- Modify: `supervisor/options.go`
- Modify: `supervisor/supervisor.go:57,77,78,98`
- Modify: `supervisor/options_test.go:16,17,53,70,72`

**Interfaces:**
- Consumes: `Config`, `DefaultConfig` (Task 1).
- Produces: internal `config` embeds `Config` (fields `ShutdownTimeout`, `Recover` promoted); `supervisor.WithConfig(cfg Config) Option`.

- [ ] **Step 1: Update existing white-box tests to the embedded field names**

In `supervisor/options_test.go`, change the five references:
- line 16: `assert.Equal(t, 30*time.Second, cfg.shutdownTimeout)` → `cfg.ShutdownTimeout`
- line 17: `assert.True(t, cfg.recover)` → `cfg.Recover`
- line 53: `assert.Equal(t, 5*time.Second, cfg.shutdownTimeout)` → `cfg.ShutdownTimeout`
- line 70: `assert.False(t, cfg.recover)` → `cfg.Recover`
- line 72: `assert.True(t, cfg.recover)` → `cfg.Recover`

- [ ] **Step 2: Add a WithConfig test**

Append to `supervisor/options_test.go`:

```go
func TestWithConfig_SetsWholeBlock(t *testing.T) {
	cfg := defaultConfig()
	WithConfig(Config{ShutdownTimeout: 7 * time.Second, Recover: false})(&cfg)
	assert.Equal(t, 7*time.Second, cfg.ShutdownTimeout)
	assert.False(t, cfg.Recover)
}
```

- [ ] **Step 3: Run tests to verify they fail (compile error)**

Run: `go test ./supervisor/ -run TestWithConfig_SetsWholeBlock -v`
Expected: FAIL — `cfg.ShutdownTimeout undefined` / `undefined: WithConfig`.

- [ ] **Step 4: Rewrite options.go**

Replace the entire contents of `supervisor/options.go` with:

```go
package supervisor

import (
	"context"
	"log/slog"
	"time"
)

// config holds resolved settings for a single Run call.
type config struct {
	Config
	services []Service
	logger   *slog.Logger
}

func defaultConfig() config {
	return config{
		Config: DefaultConfig(),
		logger: slog.Default(),
	}
}

// Option configures a Run call: it registers services and tunes behavior.
type Option func(*config)

// WithService registers a Service to be supervised.
func WithService(svc Service) Option {
	return func(c *config) { c.services = append(c.services, svc) }
}

// WithServiceFunc registers a named function as a service. name must be non-empty.
func WithServiceFunc(name string, fn func(ctx context.Context) error) Option {
	return func(c *config) {
		c.services = append(c.services, serviceFunc{name: name, fn: fn})
	}
}

// WithConfig sets the whole serializable data block at once. Build the argument
// from DefaultConfig(); a bare Config{} sets ShutdownTimeout=0 (wait indefinitely)
// and Recover=false (disables panic recovery).
func WithConfig(cfg Config) Option {
	return func(c *config) { c.Config = cfg }
}

// WithShutdownTimeout bounds how long Run waits for services to drain after
// shutdown begins. Default 30s. A value of 0 means wait indefinitely.
func WithShutdownTimeout(d time.Duration) Option {
	return func(c *config) { c.ShutdownTimeout = d }
}

// WithLogger sets the slog.Logger used for lifecycle logging. Default
// slog.Default(); passing nil installs a discard handler at Run time.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) { c.logger = l }
}

// WithRecover toggles panic recovery in each service's Run. Default true: a panic
// is converted to an ErrPanic-wrapped error (which triggers shutdown so siblings
// still drain) instead of crashing the process.
func WithRecover(enabled bool) Option {
	return func(c *config) { c.Recover = enabled }
}
```

(The `const defaultShutdownTimeout` is removed; 30s now lives only in `DefaultConfig()`.)

- [ ] **Step 5: Update supervisor.go field references**

In `supervisor/supervisor.go`, update four references to the promoted fields:
- line 57: `runService(runCtx, svc, log, cfg.recover)` → `cfg.Recover`
- line 77: `if cfg.shutdownTimeout > 0 {` → `cfg.ShutdownTimeout`
- line 78: `graceCh = time.After(cfg.shutdownTimeout)` → `cfg.ShutdownTimeout`
- line 98: `ErrShutdownTimeout, len(remaining), cfg.shutdownTimeout))` → `cfg.ShutdownTimeout`

- [ ] **Step 6: Run the full supervisor suite**

Run: `go test ./supervisor/ -v`
Expected: PASS (all existing tests plus the new `TestWithConfig_SetsWholeBlock` and Task 1 tests).

- [ ] **Step 7: Commit**

```bash
just fmt
git add supervisor/options.go supervisor/supervisor.go supervisor/options_test.go
git commit -m "refactor(supervisor): embed Config, add WithConfig, drop duplicate const"
```

---

### Task 3: Zero-trust validation in supervisor options + Run

**Files:**
- Modify: `supervisor/options.go`
- Modify: `supervisor/supervisor.go` (Run entry)
- Test: `supervisor/options_test.go`, `supervisor/supervisor_test.go`

**Interfaces:**
- Consumes: `ErrInvalidConfig`, `Config.Validate` (Task 1); internal `config` (Task 2).
- Produces: internal `config` gains `errs []error`; `WithService(nil)` / `WithServiceFunc(_, nil)` append validation errors; `Run` returns joined `ErrInvalidConfig` before launching.

- [ ] **Step 1: Write the failing tests**

Append to `supervisor/options_test.go`:

```go
func TestWithService_NilAppendsError(t *testing.T) {
	cfg := defaultConfig()
	WithService(nil)(&cfg)
	require.Len(t, cfg.errs, 1)
	assert.ErrorIs(t, cfg.errs[0], ErrInvalidConfig)
	assert.Empty(t, cfg.services)
}

func TestWithServiceFunc_NilFuncAppendsError(t *testing.T) {
	cfg := defaultConfig()
	WithServiceFunc("w", nil)(&cfg)
	require.Len(t, cfg.errs, 1)
	assert.ErrorIs(t, cfg.errs[0], ErrInvalidConfig)
	assert.Empty(t, cfg.services)
}
```

Append to `supervisor/supervisor_test.go`:

```go
func TestRun_InvalidConfigReturnsError(t *testing.T) {
	ok := fakeService{name: "ok", run: func(ctx context.Context) error { return nil }}
	err := Run(context.Background(),
		WithService(ok),
		WithService(nil),
		WithShutdownTimeout(-1),
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./supervisor/ -run 'NilAppendsError|NilFuncAppendsError|InvalidConfigReturnsError' -v`
Expected: FAIL — `cfg.errs undefined` and `Run` does not return `ErrInvalidConfig`.

- [ ] **Step 3: Add errs field + nil-checks in options.go**

In `supervisor/options.go`, add the field to `config`:

```go
type config struct {
	Config
	services []Service
	logger   *slog.Logger
	errs     []error
}
```

Add `"fmt"` to the imports, and replace `WithService` / `WithServiceFunc` with nil-checking versions:

```go
// WithService registers a Service to be supervised. A nil Service is rejected and
// surfaced by Run as ErrInvalidConfig.
func WithService(svc Service) Option {
	return func(c *config) {
		if svc == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithService received a nil Service", ErrInvalidConfig))
			return
		}
		c.services = append(c.services, svc)
	}
}

// WithServiceFunc registers a named function as a service. name must be non-empty;
// a nil func is rejected and surfaced by Run as ErrInvalidConfig.
func WithServiceFunc(name string, fn func(ctx context.Context) error) Option {
	return func(c *config) {
		if fn == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithServiceFunc(%q) received a nil func", ErrInvalidConfig, name))
			return
		}
		c.services = append(c.services, serviceFunc{name: name, fn: fn})
	}
}
```

- [ ] **Step 4: Add validation to Run entry**

In `supervisor/supervisor.go`, immediately after `log := resolveLogger(cfg.logger)` and **before** the `if len(cfg.services) == 0` check, insert:

```go
	allErrs := cfg.errs
	if e := cfg.Validate(); e != nil {
		allErrs = append(allErrs, e)
	}
	if len(allErrs) > 0 {
		return errors.Join(allErrs...)
	}
```

(`errors` is already imported in `supervisor.go`.)

- [ ] **Step 5: Run the full supervisor suite**

Run: `go test ./supervisor/ -v`
Expected: PASS (new validation tests plus all prior tests; empty `Run()` still returns `ErrNoServices`).

- [ ] **Step 6: Lint and commit**

```bash
just fmt
just lint
git add supervisor/options.go supervisor/supervisor.go supervisor/options_test.go supervisor/supervisor_test.go
git commit -m "feat(supervisor): zero-trust validation for options and Config"
```

---

### Task 4: httpserver errors + Config + DefaultConfig + Validate

**Files:**
- Create: `httpserver/errors.go`
- Create: `httpserver/config.go`
- Test: `httpserver/config_test.go`

**Interfaces:**
- Produces: `httpserver.ErrNoHandler`, `httpserver.ErrInvalidConfig`, `httpserver.ErrShutdownTimeout`; `httpserver.Config` (10 fields below); `httpserver.DefaultConfig() Config`; `func (c Config) Validate() error`.

- [ ] **Step 1: Write the failing test**

Create `httpserver/config_test.go`:

```go
package httpserver

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, ":8080", cfg.Addr)
	assert.Equal(t, 15*time.Second, cfg.ShutdownTimeout)
	assert.Equal(t, 10*time.Second, cfg.ReadHeaderTimeout)
	assert.Equal(t, 30*time.Second, cfg.ReadTimeout)
	assert.Equal(t, 30*time.Second, cfg.WriteTimeout)
	assert.Equal(t, 120*time.Second, cfg.IdleTimeout)
	assert.Equal(t, 1<<20, cfg.MaxHeaderBytes)
	assert.Empty(t, cfg.Name, "Name defaults empty so Name() derives it")
	assert.Empty(t, cfg.TLSCertFile)
	assert.Empty(t, cfg.TLSKeyFile)
	require.NoError(t, cfg.Validate())
}

func TestConfig_Validate(t *testing.T) {
	tests := map[string]Config{
		"empty addr":        {Addr: ""},
		"neg shutdown":      {Addr: ":0", ShutdownTimeout: -1},
		"neg read header":   {Addr: ":0", ReadHeaderTimeout: -1},
		"neg read":          {Addr: ":0", ReadTimeout: -1},
		"neg write":         {Addr: ":0", WriteTimeout: -1},
		"neg idle":          {Addr: ":0", IdleTimeout: -1},
		"neg maxheader":     {Addr: ":0", MaxHeaderBytes: -1},
		"half tls (cert)":   {Addr: ":0", TLSCertFile: "c.pem"},
		"half tls (key)":    {Addr: ":0", TLSKeyFile: "k.pem"},
	}
	for name, cfg := range tests {
		t.Run(name, func(t *testing.T) {
			err := cfg.Validate()
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalidConfig)
		})
	}

	// Both TLS files set is valid; zero timeouts are valid (disabled/indefinite).
	require.NoError(t, Config{Addr: ":0", TLSCertFile: "c.pem", TLSKeyFile: "k.pem"}.Validate())
	require.NoError(t, Config{Addr: ":0", WriteTimeout: 0}.Validate())
}

func TestConfig_EnvTags(t *testing.T) {
	want := map[string]string{
		"Addr":              "ADDR",
		"Name":              "NAME",
		"ShutdownTimeout":   "SHUTDOWN_TIMEOUT",
		"ReadHeaderTimeout": "READ_HEADER_TIMEOUT",
		"ReadTimeout":       "READ_TIMEOUT",
		"WriteTimeout":      "WRITE_TIMEOUT",
		"IdleTimeout":       "IDLE_TIMEOUT",
		"MaxHeaderBytes":    "MAX_HEADER_BYTES",
		"TLSCertFile":       "TLS_CERT_FILE",
		"TLSKeyFile":        "TLS_KEY_FILE",
	}
	typ := reflect.TypeFor[Config]()
	for fname, tag := range want {
		f, ok := typ.FieldByName(fname)
		require.Truef(t, ok, "field %s missing", fname)
		assert.Equalf(t, tag, f.Tag.Get("env"), "field %s env tag", fname)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./httpserver/ -v`
Expected: FAIL — package does not compile (`undefined: Config`, etc.).

- [ ] **Step 3: Create errors.go**

Create `httpserver/errors.go`:

```go
package httpserver

import "errors"

// Sentinel errors returned (often joined) by Run. Match with errors.Is.
var (
	// ErrNoHandler is returned by Run when the Server was constructed with a nil handler.
	ErrNoHandler = errors.New("httpserver: nil handler")
	// ErrInvalidConfig is returned (joined) by Run when an option or Config field has an invalid value.
	ErrInvalidConfig = errors.New("httpserver: invalid config")
	// ErrShutdownTimeout is returned by Run when the drain deadline was exceeded and connections were force-closed.
	ErrShutdownTimeout = errors.New("httpserver: graceful shutdown timed out")
)
```

- [ ] **Step 4: Create config.go**

Create `httpserver/config.go`:

```go
package httpserver

import (
	"errors"
	"fmt"
	"time"
)

// Config holds the serializable settings for a Server. The env struct tags are
// inert strings — this package imports no config loader. Populate Config with any
// loader that reads env struct tags, typically by seeding from DefaultConfig and
// parsing the environment over it. Field order is subject to the repo's betteralign
// tooling.
type Config struct {
	Addr              string        `env:"ADDR"`                // listen address; ignored when WithListener is used
	Name              string        `env:"NAME"`                // empty -> Name() derives from listener/Addr
	ShutdownTimeout   time.Duration `env:"SHUTDOWN_TIMEOUT"`    // drain bound; 0 = wait indefinitely
	ReadHeaderTimeout time.Duration `env:"READ_HEADER_TIMEOUT"` // Slowloris guard
	ReadTimeout       time.Duration `env:"READ_TIMEOUT"`        // full request read
	WriteTimeout      time.Duration `env:"WRITE_TIMEOUT"`       // response write; set 0 for SSE/streaming
	IdleTimeout       time.Duration `env:"IDLE_TIMEOUT"`        // keep-alive idle
	MaxHeaderBytes    int           `env:"MAX_HEADER_BYTES"`    // 0 = net/http default
	TLSCertFile       string        `env:"TLS_CERT_FILE"`       // both cert+key set -> serve HTTPS
	TLSKeyFile        string        `env:"TLS_KEY_FILE"`
}

// DefaultConfig returns the optimal, secure-by-default settings and is the single
// source of truth for defaults (there are no envDefault tags to drift from it).
func DefaultConfig() Config {
	return Config{
		Addr:              ":8080",
		ShutdownTimeout:   15 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

// Validate reports whether the field values are usable, returning an
// ErrInvalidConfig-wrapped, single-line joined error otherwise. Callers may call
// it after loading from env (zero-trust); Run also calls it defensively.
func (c Config) Validate() error {
	var errs []error
	if c.Addr == "" {
		errs = append(errs, fmt.Errorf("%w: Addr must not be empty", ErrInvalidConfig))
	}
	for _, f := range []struct {
		name string
		d    time.Duration
	}{
		{"ShutdownTimeout", c.ShutdownTimeout},
		{"ReadHeaderTimeout", c.ReadHeaderTimeout},
		{"ReadTimeout", c.ReadTimeout},
		{"WriteTimeout", c.WriteTimeout},
		{"IdleTimeout", c.IdleTimeout},
	} {
		if f.d < 0 {
			errs = append(errs, fmt.Errorf("%w: %s must be >= 0", ErrInvalidConfig, f.name))
		}
	}
	if c.MaxHeaderBytes < 0 {
		errs = append(errs, fmt.Errorf("%w: MaxHeaderBytes must be >= 0", ErrInvalidConfig))
	}
	if (c.TLSCertFile == "") != (c.TLSKeyFile == "") {
		errs = append(errs, fmt.Errorf("%w: TLSCertFile and TLSKeyFile must both be set or both empty", ErrInvalidConfig))
	}
	return errors.Join(errs...)
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./httpserver/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
just fmt
git add httpserver/errors.go httpserver/config.go httpserver/config_test.go
git commit -m "feat(httpserver): add errors, env-loadable Config, DefaultConfig, Validate"
```

---

### Task 5: httpserver options (internal config + all With*)

**Files:**
- Create: `httpserver/options.go`
- Test: `httpserver/options_test.go`

**Interfaces:**
- Consumes: `Config` (Task 4).
- Produces: `httpserver.Option`; internal `config` struct (embeds `Config`; fields `handler http.Handler`, `logger *slog.Logger`, `listener net.Listener`, `tlsConfig *tls.Config`, `baseContext func() context.Context`, `connState func(net.Conn, http.ConnState)`, `errs []error`); options `WithConfig`, `WithAddr`, `WithName`, `WithLogger`, `WithListener`, `WithTLSConfig`, `WithBaseContext`, `WithConnState`.

- [ ] **Step 1: Write the failing test**

Create `httpserver/options_test.go`:

```go
package httpserver

import (
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func baseConfig() config {
	return config{Config: DefaultConfig(), logger: slog.Default()}
}

func TestDataOptions_SetFields(t *testing.T) {
	c := baseConfig()
	WithAddr(":9090")(&c)
	WithName("api")(&c)
	WithConfig(Config{Addr: ":1", ReadTimeout: 1})(&c)

	// WithConfig replaced the whole block (applied last), so Addr is ":1".
	assert.Equal(t, ":1", c.Addr)
}

func TestWithLogger_NilAllowed(t *testing.T) {
	c := baseConfig()
	l := slog.New(slog.NewTextHandler(io.Discard, nil))
	WithLogger(l)(&c)
	assert.Same(t, l, c.logger)

	WithLogger(nil)(&c)
	assert.Nil(t, c.logger)
	assert.Empty(t, c.errs, "nil logger is allowed, not a validation error")
}

func TestCodeOptions_NilAppendError(t *testing.T) {
	t.Run("listener", func(t *testing.T) {
		c := baseConfig()
		WithListener(nil)(&c)
		require.Len(t, c.errs, 1)
		assert.ErrorIs(t, c.errs[0], ErrInvalidConfig)
		assert.Nil(t, c.listener)
	})
	t.Run("tlsconfig", func(t *testing.T) {
		c := baseConfig()
		WithTLSConfig(nil)(&c)
		require.Len(t, c.errs, 1)
		assert.ErrorIs(t, c.errs[0], ErrInvalidConfig)
	})
	t.Run("basecontext", func(t *testing.T) {
		c := baseConfig()
		WithBaseContext(nil)(&c)
		require.Len(t, c.errs, 1)
		assert.ErrorIs(t, c.errs[0], ErrInvalidConfig)
	})
	t.Run("connstate", func(t *testing.T) {
		c := baseConfig()
		WithConnState(nil)(&c)
		require.Len(t, c.errs, 1)
		assert.ErrorIs(t, c.errs[0], ErrInvalidConfig)
	})
}

func TestCodeOptions_StoreNonNil(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	c := baseConfig()
	WithListener(ln)(&c)
	WithTLSConfig(&tls.Config{})(&c)
	WithBaseContext(func() context.Context { return context.Background() })(&c)
	WithConnState(func(net.Conn, http.ConnState) {})(&c)

	assert.Empty(t, c.errs)
	assert.Same(t, ln, c.listener)
	assert.NotNil(t, c.tlsConfig)
	assert.NotNil(t, c.baseContext)
	assert.NotNil(t, c.connState)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./httpserver/ -run 'Options' -v`
Expected: FAIL — `undefined: config` / `WithAddr` / etc.

- [ ] **Step 3: Create options.go**

Create `httpserver/options.go`:

```go
package httpserver

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
)

// config holds resolved settings for a single Server. The embedded Config carries
// serializable data; the remaining fields are non-serializable code values.
type config struct {
	Config
	handler     http.Handler
	logger      *slog.Logger
	listener    net.Listener
	tlsConfig   *tls.Config
	baseContext func() context.Context
	connState   func(net.Conn, http.ConnState)
	errs        []error
}

// Option configures a Server. Invalid values accumulate and are returned by Run.
type Option func(*config)

// WithConfig sets the whole serializable data block at once. Build the argument
// from DefaultConfig() (or an env-parsed copy); a bare Config{} zeroes the
// timeouts. Options apply in order — place WithConfig before any WithAddr/WithName
// you want to take precedence.
func WithConfig(cfg Config) Option {
	return func(c *config) { c.Config = cfg }
}

// WithAddr sets the listen address (convenience for Config.Addr).
func WithAddr(addr string) Option {
	return func(c *config) { c.Addr = addr }
}

// WithName sets the supervisor.Service name (convenience for Config.Name).
func WithName(name string) Option {
	return func(c *config) { c.Name = name }
}

// WithLogger sets the slog.Logger for lifecycle logging and the net/http ErrorLog
// bridge. Default slog.Default(); nil installs a discard handler at Run time.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) { c.logger = l }
}

// WithListener supplies a pre-bound listener, overriding Addr (for :0 tests, unix
// sockets, socket activation). A nil listener is rejected (ErrInvalidConfig).
func WithListener(ln net.Listener) Option {
	return func(c *config) {
		if ln == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithListener received a nil net.Listener", ErrInvalidConfig))
			return
		}
		c.listener = ln
	}
}

// WithTLSConfig sets an in-memory *tls.Config (mTLS, autocert). It takes precedence
// over Config.TLSCertFile/TLSKeyFile. A nil config is rejected (ErrInvalidConfig).
func WithTLSConfig(tc *tls.Config) Option {
	return func(c *config) {
		if tc == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithTLSConfig received a nil *tls.Config", ErrInvalidConfig))
			return
		}
		c.tlsConfig = tc
	}
}

// WithBaseContext sets the root context for every request. The server layers
// force-close cancellation on top. A nil func is rejected (ErrInvalidConfig).
func WithBaseContext(fn func() context.Context) Option {
	return func(c *config) {
		if fn == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithBaseContext received a nil func", ErrInvalidConfig))
			return
		}
		c.baseContext = fn
	}
}

// WithConnState registers an http.Server.ConnState callback (metrics). A nil func
// is rejected (ErrInvalidConfig).
func WithConnState(fn func(net.Conn, http.ConnState)) Option {
	return func(c *config) {
		if fn == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithConnState received a nil func", ErrInvalidConfig))
			return
		}
		c.connState = fn
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./httpserver/ -run 'Options|Logger' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
just fmt
git add httpserver/options.go httpserver/options_test.go
git commit -m "feat(httpserver): add functional options with zero-trust nil checks"
```

---

### Task 6: Server, New, Name, resolveLogger

**Files:**
- Create: `httpserver/server.go` (Server type, New, Name, resolveLogger — Run added in Task 7)
- Test: `httpserver/server_test.go`

**Interfaces:**
- Consumes: internal `config`, `DefaultConfig` (Tasks 4–5).
- Produces: `type Server struct{ cfg config }`; `func New(handler http.Handler, opts ...Option) *Server`; `func (s *Server) Name() string`; unexported `resolveLogger(*slog.Logger) *slog.Logger`.

- [ ] **Step 1: Write the failing test**

Create `httpserver/server_test.go`:

```go
package httpserver

import (
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func noopHandler() http.Handler {
	return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
}

func TestNew_SeedsDefaults(t *testing.T) {
	s := New(noopHandler())
	assert.Equal(t, ":8080", s.cfg.Addr)
	assert.Equal(t, 1<<20, s.cfg.MaxHeaderBytes)
	assert.NotNil(t, s.cfg.handler)
}

func TestName_Derivation(t *testing.T) {
	assert.Equal(t, "http :8080", New(noopHandler()).Name())
	assert.Equal(t, "api", New(noopHandler(), WithName("api")).Name())
	assert.Equal(t, "http :9090", New(noopHandler(), WithAddr(":9090")).Name())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	s := New(noopHandler(), WithListener(ln))
	assert.Equal(t, "http "+ln.Addr().String(), s.Name())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./httpserver/ -run 'New_SeedsDefaults|Name_Derivation' -v`
Expected: FAIL — `undefined: New`.

- [ ] **Step 3: Create server.go**

Create `httpserver/server.go`:

```go
package httpserver

import (
	"io"
	"log/slog"
	"net/http"
)

// Server is a single-use HTTP service that satisfies supervisor.Service. After Run
// returns it must not be reused; construct a fresh Server per run.
type Server struct {
	cfg config
}

// New builds a Server. The handler is required and is the only positional argument.
// New does no I/O: the internal config is seeded from DefaultConfig() and each
// option is applied in order, so New(handler) alone is a complete server on every
// default. Binding happens in Run. New never fails; invalid option/Config values
// accumulate and are returned by Run.
func New(handler http.Handler, opts ...Option) *Server {
	cfg := config{
		Config:  DefaultConfig(),
		handler: handler,
		logger:  slog.Default(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Server{cfg: cfg}
}

// Name returns the configured Name, else a name derived from the injected
// listener's address, else "http " + Addr. Satisfies supervisor.Service.
func (s *Server) Name() string {
	if s.cfg.Name != "" {
		return s.cfg.Name
	}
	if s.cfg.listener != nil {
		return "http " + s.cfg.listener.Addr().String()
	}
	return "http " + s.cfg.Addr
}

// resolveLogger returns l, or a discard logger when l is nil.
func resolveLogger(l *slog.Logger) *slog.Logger {
	if l == nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return l
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./httpserver/ -run 'New_SeedsDefaults|Name_Derivation' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
just fmt
git add httpserver/server.go httpserver/server_test.go
git commit -m "feat(httpserver): add Server, New, Name derivation"
```

---

### Task 7: Run — serve + graceful drain + force-close (non-TLS)

**Files:**
- Modify: `httpserver/server.go` (add `Run`)
- Test: `httpserver/server_test.go`

**Interfaces:**
- Consumes: `Server`, `config`, `resolveLogger` (Task 6); `ErrNoHandler`, `ErrInvalidConfig`, `ErrShutdownTimeout` (Task 4).
- Produces: `func (s *Server) Run(ctx context.Context) error` plus an unexported `(*Server).drain(...)` helper. No `var _ supervisor.Service` assert is added in production (it would create a package dependency); instead Task 9 adds an external-package integration test that statically requires `*Server` to satisfy `supervisor.Service`.

- [ ] **Step 1: Write the failing tests**

Append to `httpserver/server_test.go` (this adds `context`, `errors`, and `time` to the import list; run `just fmt` to let goimports reconcile imports):

```go
// startServed runs s.Run in a goroutine on a 127.0.0.1:0 listener and returns the
// bound base URL, the channel carrying Run's result, and a cancel func. cancel is
// also registered with t.Cleanup so a failing test never leaks the server goroutine.
func startServed(t *testing.T, h http.Handler, opts ...Option) (string, <-chan error, context.CancelFunc) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := New(h, append(opts, WithListener(ln))...)
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	return "http://" + ln.Addr().String(), done, cancel
}

func TestRun_RoundTripAndGracefulStop(t *testing.T) {
	url, done, cancel := startServed(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	// Retry until the server accepts; assert inside the guard so resp is non-nil
	// (keeps nilaway happy without a //nolint).
	var ok bool
	for range 50 {
		resp, err := http.Get(url)
		if err == nil && resp != nil {
			assert.Equal(t, http.StatusTeapot, resp.StatusCode)
			_ = resp.Body.Close()
			ok = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.True(t, ok, "server did not become ready")

	cancel()
	select {
	case runErr := <-done:
		require.NoError(t, runErr)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestRun_GracefulDrainCompletesInflight(t *testing.T) {
	started := make(chan struct{})
	url, done, cancel := startServed(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)                     // exactly one request is fired in this test
		time.Sleep(100 * time.Millisecond) // still serving when ctx is cancelled
		w.WriteHeader(http.StatusNoContent)
	}))

	codeCh := make(chan int, 1)
	go func() {
		resp, err := http.Get(url)
		if err != nil || resp == nil {
			codeCh <- -1
			return
		}
		_ = resp.Body.Close()
		codeCh <- resp.StatusCode
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never started")
	}
	cancel() // begin shutdown while the request is in-flight (default 15s drain)

	select {
	case code := <-codeCh:
		assert.Equal(t, http.StatusNoContent, code, "in-flight request must finish during graceful drain")
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight request did not complete")
	}
	require.NoError(t, <-done)
}

func TestRun_NilHandlerReturnsErrNoHandler(t *testing.T) {
	err := New(nil).Run(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoHandler)
}

func TestRun_InvalidConfigReturnsError(t *testing.T) {
	err := New(noopHandler(), WithConfig(Config{Addr: ""})).Run(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestRun_BindFailureReturnsError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	// Reuse the same address without WithListener so net.Listen fails (in use).
	s := New(noopHandler(), WithAddr(ln.Addr().String()))
	err = s.Run(context.Background())
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrShutdownTimeout)
}

func TestRun_ForceCloseOnSlowHandler(t *testing.T) {
	handlerCtxDone := make(chan struct{}, 1)
	cfg := DefaultConfig()
	cfg.ShutdownTimeout = 50 * time.Millisecond
	url, done, cancel := startServed(t,
		http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			<-r.Context().Done() // blocks until force-close cancels the base context
			handlerCtxDone <- struct{}{}
		}),
		WithConfig(cfg),
	)

	// Fire a request that will be in-flight when we cancel.
	go func() {
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		_, _ = http.DefaultClient.Do(req) // connection drops on force-close
	}()
	time.Sleep(100 * time.Millisecond) // let the request reach the handler

	cancel()
	select {
	case runErr := <-done:
		assert.ErrorIs(t, runErr, ErrShutdownTimeout)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after force-close")
	}
	select {
	case <-handlerCtxDone:
	case <-time.After(time.Second):
		t.Fatal("handler base context was not cancelled at force-close")
	}
}

// TestDrain_SurfacesBufferedServeError deterministically covers the lost-error
// race: a serve error already buffered when shutdown begins must be returned by
// drain, never masked as a clean nil. drain is called directly with a pre-seeded
// serveErr and a fresh *http.Server (whose Shutdown returns nil immediately).
func TestDrain_SurfacesBufferedServeError(t *testing.T) {
	s := New(noopHandler())
	boom := errors.New("boom")
	serveErr := make(chan error, 1)
	serveErr <- boom

	err := s.drain(&http.Server{}, serveErr, func() {}, resolveLogger(nil))
	require.ErrorIs(t, err, boom)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./httpserver/ -run 'Run_' -v`
Expected: FAIL — `s.Run undefined`.

- [ ] **Step 3: Add Run to server.go**

Add to `httpserver/server.go` the import block update and the `Run` method. New imports needed: `context`, `errors`, `net`. Replace the import block with:

```go
import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
)
```

Add the method:

```go
// Run serves until ctx is cancelled or serving fails, then drains gracefully.
// It validates first (joined ErrInvalidConfig / ErrNoHandler) and does no I/O on
// failure. On ctx cancel it calls Shutdown within ShutdownTimeout; if that deadline
// passes it cancels the request base context (best-effort "last call") and force-
// closes remaining connections, returning ErrShutdownTimeout. A serve error that
// races with cancellation is always surfaced, never masked as a clean stop.
// http.ErrServerClosed is treated as a clean stop (nil).
func (s *Server) Run(ctx context.Context) error {
	allErrs := s.cfg.errs
	if e := s.cfg.Validate(); e != nil {
		allErrs = append(allErrs, e)
	}
	if s.cfg.handler == nil {
		allErrs = append(allErrs, ErrNoHandler)
	}
	if len(allErrs) > 0 {
		return errors.Join(allErrs...)
	}

	log := resolveLogger(s.cfg.logger)

	srv := &http.Server{
		Handler:           s.cfg.handler,
		ReadHeaderTimeout: s.cfg.ReadHeaderTimeout,
		ReadTimeout:       s.cfg.ReadTimeout,
		WriteTimeout:      s.cfg.WriteTimeout,
		IdleTimeout:       s.cfg.IdleTimeout,
		MaxHeaderBytes:    s.cfg.MaxHeaderBytes,
		TLSConfig:         s.cfg.tlsConfig,
		ConnState:         s.cfg.connState,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelError),
	}

	// Base context for every request, rooted at the caller's base (or Background) —
	// NOT at ctx, so requests are not aborted when shutdown begins. Cancelled only
	// at the force-close step.
	base := context.Background()
	if s.cfg.baseContext != nil {
		base = s.cfg.baseContext()
	}
	baseCtx, baseCancel := context.WithCancel(base)
	defer baseCancel()
	srv.BaseContext = func(net.Listener) context.Context { return baseCtx }

	ln := s.cfg.listener
	if ln == nil {
		var err error
		ln, err = net.Listen("tcp", s.cfg.Addr)
		if err != nil {
			return err
		}
	}
	log.Info("http server listening", slog.String("addr", ln.Addr().String()))

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	return s.drain(srv, serveErr, baseCancel, log)
}

// drain performs graceful shutdown: it waits for in-flight requests up to
// ShutdownTimeout, then on timeout cancels the request base context (best-effort
// "last call") and force-closes remaining connections. It always reads the serve
// result so a real (non-ErrServerClosed) error that raced with cancellation is
// surfaced rather than masked as a clean stop.
func (s *Server) drain(srv *http.Server, serveErr <-chan error, baseCancel context.CancelFunc, log *slog.Logger) error {
	log.Info("http server shutting down")
	shutCtx := context.Background()
	if s.cfg.ShutdownTimeout > 0 {
		var cancel context.CancelFunc
		shutCtx, cancel = context.WithTimeout(shutCtx, s.cfg.ShutdownTimeout)
		defer cancel()
	}
	shutErr := srv.Shutdown(shutCtx)
	if shutErr != nil {
		log.Error("graceful shutdown timed out, forcing close")
		baseCancel()
		_ = srv.Close()
	}

	// Always read the serve result; a real (non-ErrServerClosed) error wins.
	serveResult := <-serveErr
	if serveResult != nil && !errors.Is(serveResult, http.ErrServerClosed) {
		return serveResult
	}
	if shutErr != nil {
		return ErrShutdownTimeout
	}
	log.Info("http server stopped")
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./httpserver/ -run 'Run_|Drain' -race -v`
Expected: PASS (round-trip, graceful-drain, nil-handler, invalid-config, bind-failure, force-close, and the deterministic drain lost-error test).

- [ ] **Step 5: Run the whole package with race**

Run: `go test ./httpserver/ -race`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
just fmt
git add httpserver/server.go httpserver/server_test.go
git commit -m "feat(httpserver): implement Run with graceful drain and force-close"
```

---

### Task 8: Run — TLS serving + precedence

**Files:**
- Modify: `httpserver/server.go` (serve goroutine)
- Create: `httpserver/tls_test.go`

**Interfaces:**
- Consumes: `Run` serve goroutine (Task 7).
- Produces: TLS serving — `WithTLSConfig` (in-memory) takes precedence; cert/key files used otherwise.

- [ ] **Step 1: Write the failing tests**

Create `httpserver/tls_test.go`:

```go
package httpserver

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// selfSigned returns an in-memory cert plus its PEM bytes for the loopback host.
func selfSigned(t *testing.T) (tls.Certificate, []byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)
	return pair, certPEM, keyPEM
}

// tlsClient trusts exactly the supplied CA/cert PEM — no InsecureSkipVerify.
func tlsClient(t *testing.T, caPEM []byte) *http.Client {
	t.Helper()
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(caPEM))
	return &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
	}}
}

func startTLS(t *testing.T, opts ...Option) (string, <-chan error, context.CancelFunc) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	s := New(h, append(opts, WithListener(ln))...)
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	return "https://" + ln.Addr().String(), done, cancel
}

func waitTLS200(t *testing.T, url string, caPEM []byte) {
	t.Helper()
	c := tlsClient(t, caPEM)
	for range 50 {
		resp, err := c.Get(url)
		if err == nil && resp != nil {
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			_ = resp.Body.Close()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("TLS server never served 200")
}

func TestRun_TLSWithConfig(t *testing.T) {
	cert, certPEM, _ := selfSigned(t)
	url, done, cancel := startTLS(t, WithTLSConfig(&tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}))
	waitTLS200(t, url, certPEM)
	cancel()
	require.NoError(t, <-done)
}

func TestRun_TLSWithCertFiles(t *testing.T) {
	_, certPEM, keyPEM := selfSigned(t)
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	require.NoError(t, os.WriteFile(certFile, certPEM, 0o600))
	require.NoError(t, os.WriteFile(keyFile, keyPEM, 0o600))

	cfg := DefaultConfig()
	cfg.TLSCertFile = certFile
	cfg.TLSKeyFile = keyFile
	url, done, cancel := startTLS(t, WithConfig(cfg))
	waitTLS200(t, url, certPEM)
	cancel()
	require.NoError(t, <-done)
}

func TestRun_TLSConfigTakesPrecedenceOverFiles(t *testing.T) {
	cert, certPEM, _ := selfSigned(t)
	// Bogus cert files that would fail to load IF they were used. WithTLSConfig must win.
	cfg := DefaultConfig()
	cfg.TLSCertFile = "/nonexistent/cert.pem"
	cfg.TLSKeyFile = "/nonexistent/key.pem"
	url, done, cancel := startTLS(t,
		WithConfig(cfg),
		WithTLSConfig(&tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}),
	)
	waitTLS200(t, url, certPEM) // would fail if the bogus files were loaded
	cancel()
	require.NoError(t, <-done)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./httpserver/ -run 'Run_TLS' -v`
Expected: FAIL — server speaks plaintext (`srv.Serve`), so the https client errors / `waitTLS200` fails.

- [ ] **Step 3: Replace the serve goroutine with the TLS switch**

In `httpserver/server.go`, replace:

```go
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
```

with:

```go
	serveErr := make(chan error, 1)
	go func() {
		switch {
		case s.cfg.tlsConfig != nil:
			// In-memory config wins; pass empty paths so ServeTLS keeps its certs.
			serveErr <- srv.ServeTLS(ln, "", "")
		case s.cfg.TLSCertFile != "" && s.cfg.TLSKeyFile != "":
			serveErr <- srv.ServeTLS(ln, s.cfg.TLSCertFile, s.cfg.TLSKeyFile)
		default:
			serveErr <- srv.Serve(ln)
		}
	}()
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./httpserver/ -run 'Run_TLS' -race -v`
Expected: PASS (all three TLS tests).

- [ ] **Step 5: Commit**

```bash
just fmt
git add httpserver/server.go httpserver/tls_test.go
git commit -m "feat(httpserver): serve TLS with WithTLSConfig precedence over cert files"
```

---

### Task 9: Package docs + final verification

**Files:**
- Create: `httpserver/doc.go`
- Create: `httpserver/integration_test.go` (package `httpserver_test`)
- Test: full `just check`

**Interfaces:**
- Consumes: `httpserver.New` / `WithListener` / `WithName` (Tasks 5–7); `supervisor.Run` / `WithService` (Tasks 1–3).

- [ ] **Step 1: Create doc.go**

Create `httpserver/doc.go`:

```go
// Package httpserver runs an HTTP server as a supervised, gracefully-stopping
// service.
//
// Server wraps net/http and satisfies the supervisor.Service interface (Name and
// Run). New takes the handler and functional options; serializable settings live
// in an env-loadable Config with secure defaults:
//
//	srv := httpserver.New(router,
//		httpserver.WithAddr(":8080"),
//		httpserver.WithName("api"),
//	)
//	if err := supervisor.Run(ctx, supervisor.WithService(srv)); err != nil {
//		// ...
//	}
//
// On context cancellation the server stops accepting, drains in-flight requests
// within ShutdownTimeout, then cancels the request base context and force-closes
// any stragglers (returning ErrShutdownTimeout). All option and Config values are
// validated; invalid input is reported by Run as a joined ErrInvalidConfig and no
// I/O is performed. Diagnostics are emitted as structured slog attributes; errors
// are single-line and matchable with errors.Is against ErrNoHandler,
// ErrInvalidConfig, and ErrShutdownTimeout.
package httpserver
```

- [ ] **Step 2: Write the supervisor integration test**

Create `httpserver/integration_test.go`:

```go
package httpserver_test

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/httpserver"
	"github.com/dmitrymomot/forge/supervisor"
	"github.com/stretchr/testify/require"
)

// TestServer_RunsUnderSupervisor proves *httpserver.Server satisfies
// supervisor.Service (the WithService call compiles only if it does) and that the
// supervisor drives its lifecycle: serve, then coordinated graceful stop on cancel.
func TestServer_RunsUnderSupervisor(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := httpserver.New(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
		httpserver.WithListener(ln),
		httpserver.WithName("api"),
	)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- supervisor.Run(ctx, supervisor.WithService(srv)) }()

	url := "http://" + ln.Addr().String()
	var ok bool
	for range 50 {
		resp, err := http.Get(url)
		if err == nil && resp != nil {
			_ = resp.Body.Close()
			ok = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.True(t, ok, "server did not become ready under supervisor")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("supervisor.Run did not return after cancel")
	}
}
```

- [ ] **Step 3: Run the integration test**

Run: `go test ./httpserver/ -run TestServer_RunsUnderSupervisor -race -v`
Expected: PASS. (It compiles only because `*httpserver.Server` satisfies `supervisor.Service`.)

- [ ] **Step 4: Run the full check**

Run: `just check`
Expected: `fmt`, `lint` (vet, build, golangci-lint, nilaway, betteralign, modernize), and `test` (race + cover) all pass for `./...`.

- [ ] **Step 5: Fix any lint findings**

If `betteralign` reordered struct fields, accept its changes. If `nilaway`/`golangci-lint` flag anything, fix it minimally without changing behavior. Re-run `just check` until clean.

- [ ] **Step 6: Commit**

```bash
git add httpserver/doc.go httpserver/integration_test.go
git commit -m "docs(httpserver): add package docs and supervisor integration test"
```

---

## Self-Review Notes (verification of this plan against the spec)

- **Config = data / Option = code:** Tasks 1–2 (supervisor) and 4–5 (httpserver) — data in `Config`; loggers/listeners/tls/callbacks as options. ✔
- **DefaultConfig applied automatically:** `New` seeds `DefaultConfig()` (Task 6); `defaultConfig()` seeds it (Task 2). ✔
- **WithConfig wholesale replace + footgun documented:** Tasks 2, 5 (option doc comments). ✔
- **Zero-trust validation, accumulate + surface at Run, exported Validate:** Tasks 1, 3, 4, 5, 7. ✔
- **Graceful drain then force-close; lost-error-race fix (deterministic `TestDrain_SurfacesBufferedServeError`); in-flight completion (`TestRun_GracefulDrainCompletesInflight`); best-effort base-ctx last call:** Task 7. ✔
- **TLS precedence (WithTLSConfig over files):** Task 8 (+ precedence test). ✔
- **Listener-aware Name:** Task 6. ✔
- **Supervisor integration / `*Server` satisfies `supervisor.Service`:** Task 9 external-package test. ✔
- **Supervisor: remove duplicate const, update existing white-box tests, config.go split:** Tasks 1–2. ✔
- **Secure default timeouts:** Task 4 `DefaultConfig`. ✔
- **No new deps; testify-only tests; env contract via reflection:** Tasks 1, 4 (`EnvTags` tests, no env/v11 import). ✔
- **Single-line errors + slog attributes:** errors.go + Run logging (Tasks 4, 7). ✔
