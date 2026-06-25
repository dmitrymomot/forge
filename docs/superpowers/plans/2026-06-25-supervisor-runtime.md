# Supervisor Runtime Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `supervisor` package — a dependency-free process runtime that runs long-running services with coordinated, bounded graceful shutdown, plus a one-line `NewContext` signal helper for `main`.

**Architecture:** A single `Run(ctx, opts...)` function launches each registered `Service` (interface: `Name() string` + `Run(ctx) error`) in its own goroutine. The first service to return — or cancellation of `ctx` — cancels a shared child context so all services drain themselves; `Run` waits up to a grace timeout, then abandons stragglers and returns. Configuration and service registration both flow through functional options. No builder, no exported runtime type.

**Tech Stack:** Go 1.26 stdlib only (`context`, `os/signal`, `log/slog`, `time`, `errors`, `runtime/debug`). Tests use `github.com/stretchr/testify` (already in the module graph; test-only).

## Global Constraints

- Go version floor: **1.26** (`go.mod` already declares `go 1.26`).
- Module path: `github.com/dmitrymomot/forge`; this package lives at import path `github.com/dmitrymomot/forge/supervisor`, directory `supervisor/`.
- **Flat structure** — all files directly under `supervisor/`, no nested subdirectories.
- **No third-party runtime dependencies.** Production code is stdlib-only. `testify` is permitted in `_test.go` only.
- **No builder pattern. Options only** (project rule in `CLAUDE.md`).
- **Structured logging only:** every diagnostic is emitted as `slog` attributes. Error values are always single-line; never embed a stack trace, name list, or any multi-line/plain-text blob inside an error string.
- Work happens on the **`main` branch** (project rule in `CLAUDE.md`); each task commits directly to `main`.
- Authoritative gate is the project `just` recipes: `just test ./supervisor/` runs the package suite (race + cover); `just check` runs fmt + lint + test. Per-step `go test -run` calls are for the tight red→green loop; run `just test ./supervisor/` at each task boundary and `just check` in the final task.

## File Structure

```
supervisor/
  errors.go         # sentinel errors (ErrNoServices, ErrUnnamedService, ErrShutdownTimeout, ErrPanic)
  service.go        # Service interface + serviceFunc adapter (used by WithServiceFunc)
  options.go        # config, Option, defaultConfig, WithService/WithServiceFunc/WithShutdownTimeout/WithLogger/WithRecover
  context.go        # NewContext — signal.NotifyContext(SIGINT, SIGTERM)
  supervisor.go     # Run + supervision loop + runService + resolveLogger/warnDuplicateNames/remainingNames
  doc.go            # package documentation
  helpers_test.go   # fakeService test double (white-box)
  service_test.go   # serviceFunc tests
  options_test.go   # option/config tests
  context_test.go   # NewContext signal test
  supervisor_test.go# Run behavior tests + discardLogger helper
```

All test files use `package supervisor` (white-box) so they can exercise unexported helpers (`runService`, `resolveLogger`, `serviceFunc`, `config`).

---

## Task 1: Errors and Service interface

**Files:**
- Create: `supervisor/errors.go`
- Create: `supervisor/service.go`
- Test: `supervisor/service_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `Service interface { Name() string; Run(ctx context.Context) error }`
  - unexported `serviceFunc struct { name string; fn func(context.Context) error }` implementing `Service`
  - sentinels `ErrNoServices`, `ErrUnnamedService`, `ErrShutdownTimeout`, `ErrPanic` (all `error`)

- [ ] **Step 1: Write the failing tests**

Create `supervisor/service_test.go`:

```go
package supervisor

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceFunc_DelegatesNameAndRun(t *testing.T) {
	called := false
	var gotCtx context.Context
	s := serviceFunc{name: "worker", fn: func(ctx context.Context) error {
		called = true
		gotCtx = ctx
		return nil
	}}

	require.Equal(t, "worker", s.Name())

	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "v")
	require.NoError(t, s.Run(ctx))
	assert.True(t, called, "fn must be invoked by Run")
	assert.Equal(t, "v", gotCtx.Value(ctxKey{}), "Run must pass ctx straight through")
}

func TestServiceFunc_PropagatesError(t *testing.T) {
	sentinel := errors.New("boom")
	s := serviceFunc{name: "x", fn: func(ctx context.Context) error { return sentinel }}
	require.ErrorIs(t, s.Run(context.Background()), sentinel)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -run TestServiceFunc ./supervisor/`
Expected: FAIL — compile error `undefined: serviceFunc`.

- [ ] **Step 3: Implement errors.go**

Create `supervisor/errors.go`:

```go
package supervisor

import "errors"

// Sentinel errors returned (often joined) by Run. Match with errors.Is.
var (
	// ErrNoServices is returned by Run when no services were registered.
	ErrNoServices = errors.New("supervisor: no services registered")
	// ErrUnnamedService is returned by Run when a registered service has an empty Name.
	ErrUnnamedService = errors.New("supervisor: service has empty name")
	// ErrShutdownTimeout is returned by Run when services do not stop within the grace timeout.
	ErrShutdownTimeout = errors.New("supervisor: graceful shutdown timed out")
	// ErrPanic wraps a value recovered from a panicking service's Run.
	ErrPanic = errors.New("supervisor: service panicked")
)
```

- [ ] **Step 4: Implement service.go**

Create `supervisor/service.go`:

```go
package supervisor

import "context"

// Service is a long-running unit of work supervised by Run.
//
// Name returns a non-empty, stable identifier used in logs and shutdown
// diagnostics. Run blocks until the work completes or ctx is cancelled;
// implementations MUST observe ctx cancellation and shut down gracefully,
// returning when drained. Returning context.Canceled in response to
// cancellation is treated as a clean stop.
type Service interface {
	Name() string
	Run(ctx context.Context) error
}

// serviceFunc adapts a named function to Service. Created by WithServiceFunc.
type serviceFunc struct {
	name string
	fn   func(ctx context.Context) error
}

func (s serviceFunc) Name() string                  { return s.name }
func (s serviceFunc) Run(ctx context.Context) error { return s.fn(ctx) }
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test -run TestServiceFunc ./supervisor/`
Expected: PASS (`ok  github.com/dmitrymomot/forge/supervisor`).

- [ ] **Step 6: Tidy modules and commit**

`testify` is now imported from non-test... no — it is test-only, but `go mod tidy` relabels it from indirect to a direct test dependency.

Run: `go mod tidy`
Then commit:

```bash
git add supervisor/errors.go supervisor/service.go supervisor/service_test.go go.mod go.sum
git commit -m "feat(supervisor): add Service interface and sentinel errors"
```

---

## Task 2: Options and config

**Files:**
- Create: `supervisor/options.go`
- Create: `supervisor/helpers_test.go`
- Test: `supervisor/options_test.go`

**Interfaces:**
- Consumes: `Service`, `serviceFunc` (Task 1).
- Produces:
  - `Option func(*config)`
  - unexported `config struct { services []Service; shutdownTimeout time.Duration; logger *slog.Logger; recover bool }`
  - unexported `defaultConfig() config` (shutdownTimeout 30s, logger `slog.Default()`, recover `true`)
  - `WithService(svc Service) Option`
  - `WithServiceFunc(name string, fn func(ctx context.Context) error) Option`
  - `WithShutdownTimeout(d time.Duration) Option`
  - `WithLogger(l *slog.Logger) Option`
  - `WithRecover(enabled bool) Option`
  - test double `fakeService struct { name string; run func(context.Context) error }` (in `helpers_test.go`, used by all later test files)

- [ ] **Step 1: Create the shared test double**

Create `supervisor/helpers_test.go`:

```go
package supervisor

import "context"

// fakeService is a controllable Service test double shared across test files.
type fakeService struct {
	name string
	run  func(ctx context.Context) error
}

func (f fakeService) Name() string                  { return f.name }
func (f fakeService) Run(ctx context.Context) error { return f.run(ctx) }
```

- [ ] **Step 2: Write the failing tests**

Create `supervisor/options_test.go`:

```go
package supervisor

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()
	assert.Equal(t, 30*time.Second, cfg.shutdownTimeout)
	assert.True(t, cfg.recover)
	assert.NotNil(t, cfg.logger)
	assert.Empty(t, cfg.services)
}

func TestWithService_Appends(t *testing.T) {
	cfg := defaultConfig()
	a := fakeService{name: "a", run: func(ctx context.Context) error { return nil }}
	b := fakeService{name: "b", run: func(ctx context.Context) error { return nil }}

	WithService(a)(&cfg)
	WithService(b)(&cfg)

	require.Len(t, cfg.services, 2)
	assert.Equal(t, "a", cfg.services[0].Name())
	assert.Equal(t, "b", cfg.services[1].Name())
}

func TestWithServiceFunc_CreatesNamedService(t *testing.T) {
	cfg := defaultConfig()
	called := false

	WithServiceFunc("worker", func(ctx context.Context) error {
		called = true
		return nil
	})(&cfg)

	require.Len(t, cfg.services, 1)
	assert.Equal(t, "worker", cfg.services[0].Name())
	require.NoError(t, cfg.services[0].Run(context.Background()))
	assert.True(t, called)
}

func TestWithShutdownTimeout(t *testing.T) {
	cfg := defaultConfig()
	WithShutdownTimeout(5 * time.Second)(&cfg)
	assert.Equal(t, 5*time.Second, cfg.shutdownTimeout)
}

func TestWithLogger_StoresValueIncludingNil(t *testing.T) {
	cfg := defaultConfig()
	l := slog.New(slog.NewTextHandler(io.Discard, nil))

	WithLogger(l)(&cfg)
	assert.Same(t, l, cfg.logger)

	WithLogger(nil)(&cfg)
	assert.Nil(t, cfg.logger, "nil must be stored verbatim; Run resolves it to a discard logger")
}

func TestWithRecover_Toggles(t *testing.T) {
	cfg := defaultConfig()
	WithRecover(false)(&cfg)
	assert.False(t, cfg.recover)
	WithRecover(true)(&cfg)
	assert.True(t, cfg.recover)
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test -run 'TestDefaultConfig|TestWith' ./supervisor/`
Expected: FAIL — compile error `undefined: defaultConfig` / `undefined: config`.

- [ ] **Step 4: Implement options.go**

Create `supervisor/options.go`:

```go
package supervisor

import (
	"context"
	"log/slog"
	"time"
)

const defaultShutdownTimeout = 30 * time.Second

// config holds resolved settings for a single Run call.
type config struct {
	services        []Service
	shutdownTimeout time.Duration
	logger          *slog.Logger
	recover         bool
}

func defaultConfig() config {
	return config{
		shutdownTimeout: defaultShutdownTimeout,
		logger:          slog.Default(),
		recover:         true,
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

// WithShutdownTimeout bounds how long Run waits for services to drain after
// shutdown begins. Default 30s. A value of 0 means wait indefinitely.
func WithShutdownTimeout(d time.Duration) Option {
	return func(c *config) { c.shutdownTimeout = d }
}

// WithLogger sets the slog.Logger used for lifecycle logging. Default
// slog.Default(); passing nil installs a discard handler at Run time.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) { c.logger = l }
}

// WithRecover toggles panic recovery in each service's Run. Default true: a
// panic is converted to an ErrPanic-wrapped error (which triggers shutdown so
// siblings still drain) instead of crashing the process.
func WithRecover(enabled bool) Option {
	return func(c *config) { c.recover = enabled }
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test -run 'TestDefaultConfig|TestWith' ./supervisor/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add supervisor/options.go supervisor/helpers_test.go supervisor/options_test.go
git commit -m "feat(supervisor): add config and functional options"
```

---

## Task 3: NewContext signal helper

**Files:**
- Create: `supervisor/context.go`
- Test: `supervisor/context_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `NewContext() (context.Context, context.CancelFunc)` — context cancelled on first SIGINT/SIGTERM via `signal.NotifyContext`.

- [ ] **Step 1: Write the failing tests**

Create `supervisor/context_test.go`:

```go
package supervisor

import (
	"context"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewContext_CancelsOnSIGTERM(t *testing.T) {
	ctx, stop := NewContext()
	defer stop()

	require.NoError(t, ctx.Err(), "context must be live before any signal")

	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGTERM))

	select {
	case <-ctx.Done():
		assert.ErrorIs(t, ctx.Err(), context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("context was not cancelled after SIGTERM")
	}
}

func TestNewContext_StopIsSafe(t *testing.T) {
	_, stop := NewContext()
	stop() // releasing the handler must not panic
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -run TestNewContext ./supervisor/`
Expected: FAIL — compile error `undefined: NewContext`.

- [ ] **Step 3: Implement context.go**

Create `supervisor/context.go`:

```go
package supervisor

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// NewContext returns a context that is cancelled on the first SIGINT or
// SIGTERM, implemented with signal.NotifyContext. It is single-shot: after the
// first signal the context is cancelled and further signals are not handled by
// this helper. Call stop (typically deferred in main) to release the handler.
func NewContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -run TestNewContext ./supervisor/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add supervisor/context.go supervisor/context_test.go
git commit -m "feat(supervisor): add NewContext signal helper"
```

---

## Task 4: Run — core supervision loop

Implements validation, concurrent launch, first-exit-stops-all with sibling drain, error aggregation (filtering `context.Canceled`), the already-cancelled-context short-circuit, and the duplicate-name warning. **No grace timer yet** (added in Task 5) — every test here uses services that stop cooperatively, so the loop always terminates.

**Files:**
- Create: `supervisor/supervisor.go`
- Test: `supervisor/supervisor_test.go`

**Interfaces:**
- Consumes: `config`, `defaultConfig`, all `With*` options (Task 2); `Service` (Task 1); sentinels `ErrNoServices`, `ErrUnnamedService` (Task 1).
- Produces:
  - `Run(ctx context.Context, opts ...Option) error`
  - unexported `runService(ctx context.Context, svc Service) error` (passthrough; gains panic recovery in Task 6)
  - unexported `resolveLogger(l *slog.Logger) *slog.Logger` (nil → discard handler)
  - unexported `warnDuplicateNames(log *slog.Logger, services []Service)`
  - test helper `discardLogger() *slog.Logger` (in `supervisor_test.go`)

- [ ] **Step 1: Write the failing tests**

Create `supervisor/supervisor_test.go`:

```go
package supervisor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRun_NoServices_ReturnsErrNoServices(t *testing.T) {
	err := Run(context.Background())
	require.ErrorIs(t, err, ErrNoServices)
}

func TestRun_EmptyName_ReturnsErrUnnamedService(t *testing.T) {
	svc := fakeService{name: "", run: func(ctx context.Context) error { return nil }}
	err := Run(context.Background(), WithService(svc), WithLogger(discardLogger()))
	require.ErrorIs(t, err, ErrUnnamedService)
}

func TestRun_SingleService_ReturnsWrappedError(t *testing.T) {
	sentinel := errors.New("boom")
	svc := fakeService{name: "svc", run: func(ctx context.Context) error { return sentinel }}

	err := Run(context.Background(), WithService(svc), WithLogger(discardLogger()))

	require.ErrorIs(t, err, sentinel)
	assert.Contains(t, err.Error(), `service "svc"`)
}

func TestRun_FirstExitStopsAll(t *testing.T) {
	siblingCancelled := make(chan struct{})
	sibling := fakeService{name: "sibling", run: func(ctx context.Context) error {
		<-ctx.Done()
		close(siblingCancelled)
		return ctx.Err()
	}}
	quick := fakeService{name: "quick", run: func(ctx context.Context) error { return nil }}

	err := Run(context.Background(),
		WithService(sibling), WithService(quick), WithLogger(discardLogger()))

	require.NoError(t, err, "quick returns nil; sibling returns context.Canceled which is filtered")
	select {
	case <-siblingCancelled:
	default:
		t.Fatal("sibling was not cancelled when quick exited")
	}
}

func TestRun_ContextCancel_ShutsDown(t *testing.T) {
	svc := fakeService{name: "svc", run: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := Run(ctx, WithService(svc), WithLogger(discardLogger()))
	require.NoError(t, err)
}

func TestRun_AggregatesNonCanceledErrors(t *testing.T) {
	errA := errors.New("err-a")
	errB := errors.New("err-b")
	a := fakeService{name: "a", run: func(ctx context.Context) error { return errA }}
	b := fakeService{name: "b", run: func(ctx context.Context) error {
		<-ctx.Done()
		return errB // a real error during drain, NOT context.Canceled
	}}

	err := Run(context.Background(), WithService(a), WithService(b), WithLogger(discardLogger()))

	require.ErrorIs(t, err, errA)
	require.ErrorIs(t, err, errB)
}

func TestRun_AlreadyCancelledContext_DoesNotStartServices(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	started := false
	svc := fakeService{name: "svc", run: func(ctx context.Context) error {
		started = true
		return nil
	}}

	err := Run(ctx, WithService(svc), WithLogger(discardLogger()))

	require.NoError(t, err)
	assert.False(t, started, "no service may start when ctx is already cancelled")
}

func TestRun_DuplicateNames_Warns(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	a := fakeService{name: "dup", run: func(ctx context.Context) error { return nil }}
	b := fakeService{name: "dup", run: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}

	_ = Run(context.Background(), WithService(a), WithService(b), WithLogger(logger))

	assert.Contains(t, buf.String(), "duplicate service name")
}

func TestResolveLogger_NilReturnsUsableLogger(t *testing.T) {
	got := resolveLogger(nil)
	require.NotNil(t, got)
	got.Info("must not panic")
}

func TestResolveLogger_PassthroughWhenSet(t *testing.T) {
	l := slog.New(slog.NewTextHandler(io.Discard, nil))
	assert.Same(t, l, resolveLogger(l))
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -run 'TestRun_|TestResolveLogger' ./supervisor/`
Expected: FAIL — compile error `undefined: Run` / `undefined: resolveLogger`.

- [ ] **Step 3: Implement supervisor.go**

Create `supervisor/supervisor.go`:

```go
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
)

// Run starts every registered service, supervises them, and blocks until
// shutdown completes. The first service to return (nil or error), or
// cancellation of ctx, begins a coordinated shutdown: the shared context is
// cancelled so every service drains, and Run waits for them all to return.
// Run returns the joined non-context.Canceled service errors, or nil on a
// clean stop.
func Run(ctx context.Context, opts ...Option) error {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	log := resolveLogger(cfg.logger)

	if len(cfg.services) == 0 {
		return ErrNoServices
	}
	for _, svc := range cfg.services {
		if svc.Name() == "" {
			return ErrUnnamedService
		}
	}
	warnDuplicateNames(log, cfg.services)

	if err := ctx.Err(); err != nil {
		log.Info("context already cancelled, nothing to run")
		return nil
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		idx  int
		name string
		err  error
	}
	results := make(chan result, len(cfg.services))
	remaining := make(map[int]string, len(cfg.services))

	for i, svc := range cfg.services {
		remaining[i] = svc.Name()
		go func() {
			log.Info("service started", slog.String("service", svc.Name()))
			err := runService(runCtx, svc)
			results <- result{idx: i, name: svc.Name(), err: err}
		}()
	}

	var (
		errs         []error
		done         = runCtx.Done()
		shuttingDown bool
	)

	beginShutdown := func(reason string) {
		if shuttingDown {
			return
		}
		shuttingDown = true
		log.Info("shutdown started", slog.String("reason", reason))
		cancel()
		done = nil
	}

	for len(remaining) > 0 {
		select {
		case res := <-results:
			delete(remaining, res.idx)
			log.Info("service stopped",
				slog.String("service", res.name), slog.Any("err", res.err))
			if res.err != nil && !errors.Is(res.err, context.Canceled) {
				errs = append(errs, fmt.Errorf("service %q: %w", res.name, res.err))
			}
			beginShutdown(fmt.Sprintf("service %q exited", res.name))
		case <-done:
			beginShutdown("context cancelled")
		}
	}

	log.Info("shutdown complete")
	return errors.Join(errs...)
}

// runService runs a single service. Panic recovery is added in a later task.
func runService(ctx context.Context, svc Service) error {
	return svc.Run(ctx)
}

// resolveLogger returns l, or a discard logger when l is nil.
func resolveLogger(l *slog.Logger) *slog.Logger {
	if l == nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return l
}

// warnDuplicateNames logs a warning for each service name that appears more
// than once. Duplicates are permitted; they only hurt log readability.
func warnDuplicateNames(log *slog.Logger, services []Service) {
	seen := make(map[string]struct{}, len(services))
	for _, svc := range services {
		name := svc.Name()
		if _, dup := seen[name]; dup {
			log.Warn("duplicate service name", slog.String("service", name))
			continue
		}
		seen[name] = struct{}{}
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -race -run 'TestRun_|TestResolveLogger' ./supervisor/`
Expected: PASS, with no race warnings.

- [ ] **Step 5: Run the full package suite**

Run: `just test ./supervisor/`
Expected: `ok  github.com/dmitrymomot/forge/supervisor` (all tests from Tasks 1–4 pass under `-race`).

- [ ] **Step 6: Commit**

```bash
git add supervisor/supervisor.go supervisor/supervisor_test.go
git commit -m "feat(supervisor): add Run with coordinated first-exit shutdown"
```

---

## Task 5: Run — grace timeout and abandon

Adds the bounded wait: once shutdown begins, arm a grace timer; if it fires before all services return, append `ErrShutdownTimeout`, log the still-running names as a structured `stuck` attribute, and return immediately (abandoning the stragglers). A `shutdownTimeout` of `0` means wait indefinitely (timer never armed).

**Files:**
- Modify: `supervisor/supervisor.go` (rewrite `Run`; add `remainingNames`; add `sort` and `time` imports)
- Test: `supervisor/supervisor_test.go` (add timeout tests)

**Interfaces:**
- Consumes: everything from Task 4; `ErrShutdownTimeout` (Task 1); `cfg.shutdownTimeout` (Task 2).
- Produces: unexported `remainingNames(remaining map[int]string) []string` (sorted, for deterministic logs); `Run` now honors `WithShutdownTimeout`.

- [ ] **Step 1: Write the failing tests**

Append to `supervisor/supervisor_test.go`:

```go
func TestRun_GraceTimeout_AbandonsStuckService(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	stuck := fakeService{name: "stuck", run: func(ctx context.Context) error {
		<-release // deliberately ignores ctx
		return nil
	}}
	trigger := fakeService{name: "trigger", run: func(ctx context.Context) error {
		return nil // exits immediately -> begins shutdown
	}}

	start := time.Now()
	err := Run(context.Background(),
		WithService(stuck), WithService(trigger),
		WithShutdownTimeout(50*time.Millisecond),
		WithLogger(discardLogger()))

	require.ErrorIs(t, err, ErrShutdownTimeout)
	assert.Less(t, time.Since(start), 2*time.Second, "must return shortly after the grace timeout")
}

func TestRun_GraceTimeout_LogsStuckNamesStructured(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	stuck := fakeService{name: "stuck-svc", run: func(ctx context.Context) error { <-release; return nil }}
	trigger := fakeService{name: "trigger", run: func(ctx context.Context) error { return nil }}

	_ = Run(context.Background(),
		WithService(stuck), WithService(trigger),
		WithShutdownTimeout(30*time.Millisecond), WithLogger(logger))

	out := buf.String()
	assert.Contains(t, out, "graceful shutdown timed out")
	assert.Contains(t, out, "stuck-svc", "stuck service name must appear in the structured log")
}

func TestRun_ZeroTimeout_DrainsCooperativeService(t *testing.T) {
	// With timeout 0 there is no abandon; a cooperative service still drains.
	svc := fakeService{name: "coop", run: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := Run(ctx, WithService(svc), WithShutdownTimeout(0), WithLogger(discardLogger()))
	require.NoError(t, err)
}
```

- [ ] **Step 2: Run the new tests to verify they fail**

Run: `go test -run 'TestRun_GraceTimeout|TestRun_ZeroTimeout' ./supervisor/`
Expected: FAIL — `TestRun_GraceTimeout_AbandonsStuckService` hangs/fails because the current loop has no timer and waits forever for `stuck`. (Use Ctrl-C if it hangs; this proves the missing behavior.)

- [ ] **Step 3: Update Run to add the grace timer**

Replace the entire body of `supervisor/supervisor.go`'s `Run` function and add the `remainingNames` helper. The file's `import` block becomes:

```go
import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"time"
)
```

Replace `Run` with:

```go
func Run(ctx context.Context, opts ...Option) error {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	log := resolveLogger(cfg.logger)

	if len(cfg.services) == 0 {
		return ErrNoServices
	}
	for _, svc := range cfg.services {
		if svc.Name() == "" {
			return ErrUnnamedService
		}
	}
	warnDuplicateNames(log, cfg.services)

	if err := ctx.Err(); err != nil {
		log.Info("context already cancelled, nothing to run")
		return nil
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		idx  int
		name string
		err  error
	}
	results := make(chan result, len(cfg.services))
	remaining := make(map[int]string, len(cfg.services))

	for i, svc := range cfg.services {
		remaining[i] = svc.Name()
		go func() {
			log.Info("service started", slog.String("service", svc.Name()))
			err := runService(runCtx, svc)
			results <- result{idx: i, name: svc.Name(), err: err}
		}()
	}

	var (
		errs         []error
		done         = runCtx.Done()
		graceCh      <-chan time.Time // nil until shutdown begins; never armed when timeout == 0
		shuttingDown bool
	)

	beginShutdown := func(reason string) {
		if shuttingDown {
			return
		}
		shuttingDown = true
		log.Info("shutdown started", slog.String("reason", reason))
		cancel()
		done = nil
		if cfg.shutdownTimeout > 0 {
			graceCh = time.After(cfg.shutdownTimeout)
		}
	}

	for len(remaining) > 0 {
		select {
		case res := <-results:
			delete(remaining, res.idx)
			log.Info("service stopped",
				slog.String("service", res.name), slog.Any("err", res.err))
			if res.err != nil && !errors.Is(res.err, context.Canceled) {
				errs = append(errs, fmt.Errorf("service %q: %w", res.name, res.err))
			}
			beginShutdown(fmt.Sprintf("service %q exited", res.name))
		case <-done:
			beginShutdown("context cancelled")
		case <-graceCh:
			errs = append(errs, fmt.Errorf("%w: %d service(s) did not stop within %s",
				ErrShutdownTimeout, len(remaining), cfg.shutdownTimeout))
			log.Error("graceful shutdown timed out", slog.Any("stuck", remainingNames(remaining)))
			return errors.Join(errs...)
		}
	}

	log.Info("shutdown complete")
	return errors.Join(errs...)
}
```

Add this helper to the same file (e.g. after `warnDuplicateNames`):

```go
// remainingNames returns the names of services still running, sorted for
// deterministic logging.
func remainingNames(remaining map[int]string) []string {
	names := make([]string, 0, len(remaining))
	for _, name := range remaining {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
```

- [ ] **Step 4: Run the new tests to verify they pass**

Run: `go test -race -run 'TestRun_GraceTimeout|TestRun_ZeroTimeout' ./supervisor/`
Expected: PASS.

- [ ] **Step 5: Run the full package suite**

Run: `just test ./supervisor/`
Expected: `ok` — all prior tests still pass.

- [ ] **Step 6: Commit**

```bash
git add supervisor/supervisor.go supervisor/supervisor_test.go
git commit -m "feat(supervisor): bound graceful shutdown and abandon stragglers"
```

---

## Task 6: Run — panic recovery and structured panic logging

Gives `runService` its final signature and behavior: when recovery is enabled (default), a panic escaping a service's `Run` is logged with structured `service`/`panic`/`stack` attributes and converted to a single-line `ErrPanic`-wrapped error — which then drives a normal coordinated shutdown so siblings still drain. The stack is **never** embedded in the error string.

**Files:**
- Modify: `supervisor/supervisor.go` (rewrite `runService`; update its call site in `Run`; add `runtime/debug` import)
- Test: `supervisor/supervisor_test.go` (add recovery tests)

**Interfaces:**
- Consumes: `ErrPanic` (Task 1); `cfg.recover` (Task 2); `log` (the resolved logger inside `Run`).
- Produces: `runService(ctx context.Context, svc Service, log *slog.Logger, recoverPanics bool) error` (new signature — call site updated in `Run`).

- [ ] **Step 1: Write the failing tests**

Append to `supervisor/supervisor_test.go`:

```go
import "encoding/json" // add to the existing import block if not present

func TestRunService_RecoverEnabled_ReturnsSingleLineErrPanic(t *testing.T) {
	svc := fakeService{name: "boom", run: func(ctx context.Context) error { panic("kaboom") }}

	err := runService(context.Background(), svc, discardLogger(), true)

	require.ErrorIs(t, err, ErrPanic)
	assert.Contains(t, err.Error(), "kaboom")
	assert.NotContains(t, err.Error(), "\n", "error string must be single-line; no stack embedded")
}

func TestRunService_RecoverEnabled_LogsStackAsAttribute(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	svc := fakeService{name: "boom", run: func(ctx context.Context) error { panic("kaboom") }}

	_ = runService(context.Background(), svc, logger, true)

	var rec map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec))
	assert.Equal(t, "service panicked", rec["msg"])
	assert.Equal(t, "boom", rec["service"])
	stack, ok := rec["stack"].(string)
	require.True(t, ok, "stack must be a structured string attribute")
	assert.Contains(t, stack, "goroutine")
}

func TestRunService_RecoverDisabled_Propagates(t *testing.T) {
	svc := fakeService{name: "boom", run: func(ctx context.Context) error { panic("kaboom") }}
	require.Panics(t, func() {
		_ = runService(context.Background(), svc, discardLogger(), false)
	})
}

func TestRun_PanicTriggersGracefulShutdown(t *testing.T) {
	siblingDrained := make(chan struct{})
	sibling := fakeService{name: "sibling", run: func(ctx context.Context) error {
		<-ctx.Done()
		close(siblingDrained)
		return ctx.Err()
	}}
	panicky := fakeService{name: "panicky", run: func(ctx context.Context) error { panic("boom") }}

	err := Run(context.Background(),
		WithService(sibling), WithService(panicky), WithLogger(discardLogger()))

	require.ErrorIs(t, err, ErrPanic)
	select {
	case <-siblingDrained:
	default:
		t.Fatal("sibling did not drain when the other service panicked")
	}
}
```

Note: ensure `encoding/json` is in the test file's import block (add it if missing).

- [ ] **Step 2: Run the new tests to verify they fail**

Run: `go test -run 'TestRunService_|TestRun_Panic' ./supervisor/`
Expected: FAIL — compile error: too few arguments to `runService` (current signature is `runService(ctx, svc)`).

- [ ] **Step 3: Update runService and its call site**

In `supervisor/supervisor.go`, add `"runtime/debug"` to the import block:

```go
import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime/debug"
	"sort"
	"time"
)
```

Replace the `runService` function with:

```go
// runService runs a single service. When recoverPanics is true, a panic
// escaping svc.Run is logged with structured attributes (service, panic,
// stack) and converted to an ErrPanic-wrapped, single-line error. The stack is
// never embedded in the returned error string.
func runService(ctx context.Context, svc Service, log *slog.Logger, recoverPanics bool) (err error) {
	if !recoverPanics {
		return svc.Run(ctx)
	}
	defer func() {
		if p := recover(); p != nil {
			log.Error("service panicked",
				slog.String("service", svc.Name()),
				slog.Any("panic", p),
				slog.String("stack", string(debug.Stack())),
			)
			err = fmt.Errorf("%w: %v", ErrPanic, p)
		}
	}()
	return svc.Run(ctx)
}
```

Update the call site inside `Run`'s launch loop — change the goroutine body to pass the logger and recover flag:

```go
		go func() {
			log.Info("service started", slog.String("service", svc.Name()))
			err := runService(runCtx, svc, log, cfg.recover)
			results <- result{idx: i, name: svc.Name(), err: err}
		}()
```

- [ ] **Step 4: Run the new tests to verify they pass**

Run: `go test -race -run 'TestRunService_|TestRun_Panic' ./supervisor/`
Expected: PASS.

- [ ] **Step 5: Run the full package suite**

Run: `just test ./supervisor/`
Expected: `ok` — all tests pass under `-race`.

- [ ] **Step 6: Commit**

```bash
git add supervisor/supervisor.go supervisor/supervisor_test.go
git commit -m "feat(supervisor): recover service panics into structured logs and ErrPanic"
```

---

## Task 7: Package documentation and final gate

**Files:**
- Create: `supervisor/doc.go`

**Interfaces:**
- Consumes: the whole public API.
- Produces: package-level godoc.

- [ ] **Step 1: Write doc.go**

Create `supervisor/doc.go`:

```go
// Package supervisor runs a set of long-running services under a single
// coordinated lifecycle with graceful, bounded shutdown.
//
// A Service is any type with a Name and a blocking Run(ctx) method. Register
// services and tune behavior through options passed to Run, and use NewContext
// to wire main's context to SIGINT/SIGTERM:
//
//	func main() {
//		ctx, stop := supervisor.NewContext()
//		defer stop()
//
//		err := supervisor.Run(ctx,
//			supervisor.WithService(httpServer),
//			supervisor.WithServiceFunc("cleanup", cleanup.Loop),
//			supervisor.WithShutdownTimeout(20*time.Second),
//		)
//		if err != nil {
//			slog.Error("runtime stopped", "err", err)
//			os.Exit(1)
//		}
//	}
//
// The first service to return (nil or error), or cancellation of ctx, begins a
// coordinated shutdown: the shared context is cancelled so every service drains
// itself, and Run waits up to the shutdown timeout (default 30s; 0 means wait
// indefinitely) before abandoning any service that has not stopped. All
// diagnostics are emitted as structured slog attributes; the values returned by
// Run are single-line errors that can be matched with errors.Is against
// ErrNoServices, ErrUnnamedService, ErrShutdownTimeout, and ErrPanic.
package supervisor
```

- [ ] **Step 2: Verify the package builds and docs render**

Run: `go build ./supervisor/ && go doc ./supervisor/`
Expected: build succeeds; `go doc` prints the package synopsis and the exported symbols (`Run`, `NewContext`, `Service`, `Option`, `With*`, and the `Err*` sentinels).

- [ ] **Step 3: Run the full project gate**

Run: `just check`
Expected: `fmt`, `lint` (go vet, golangci-lint, nilaway, betteralign, modernize), and `test` all pass with no findings in `supervisor/`.

If `betteralign` or `golines`/`goimports` rewrites any file during `just fmt`, re-run `just check` and include the formatting changes in the commit below.

- [ ] **Step 4: Commit**

```bash
git add supervisor/doc.go
git commit -m "docs(supervisor): add package documentation"
```

---

## Self-Review

**1. Spec coverage** (against `docs/superpowers/specs/2026-06-25-supervisor-runtime-design.md`):

- Service contract (`Name()` + `Run(ctx) error`, mandatory name) → Task 1, validated in Task 4 (`ErrUnnamedService`).
- Single entry `Run(ctx, opts...)`, options-only, no builder/exported runtime type → Tasks 2 & 4.
- `WithService` / `WithServiceFunc(name, fn)` / `WithShutdownTimeout` / `WithLogger` / `WithRecover` with stated defaults → Task 2.
- First-exit-stops-all + sibling drain → Task 4 (`TestRun_FirstExitStopsAll`).
- Grace timeout + abandon, `0` = wait forever, stuck names structured-logged → Task 5.
- Panic recovery (default on), single-line `ErrPanic`, stack as structured attribute → Task 6.
- `context.Canceled` filtered, `DeadlineExceeded` not → Task 4 (`TestRun_AggregatesNonCanceledErrors` covers a non-canceled drain error; canceled filtering covered by `TestRun_FirstExitStopsAll`/`TestRun_ContextCancel_ShutsDown`).
- Empty list → `ErrNoServices`; empty name → `ErrUnnamedService`; already-cancelled ctx → nil, nothing launched; duplicate names warned → Task 4.
- `NewContext` via `signal.NotifyContext`, single-shot → Task 3.
- Errors `ErrNoServices`/`ErrUnnamedService`/`ErrShutdownTimeout`/`ErrPanic` → Task 1.
- Logging events (INFO started/shutdown-started/stopped/complete, WARN duplicate, ERROR panic/timeout), structured-only → Tasks 4–6.
- File layout (flat, listed files) → all tasks; `doc.go` → Task 7.
- Testability (no real signals for supervision; self-signal for NewContext; buffer-backed handler; small real durations) → Tasks 3–6.

No gaps found.

**2. Placeholder scan:** No `TBD`/`TODO`/"handle edge cases"/"similar to Task N". The only forward reference ("panic recovery is added in a later task") is a correct, complete passthrough at Task 4 and is fully implemented in Task 6 — not a placeholder. Every code step shows complete code.

**3. Type consistency:**
- `Service` (`Name`/`Run`) consistent across Tasks 1, 2, 4.
- `config` fields (`services`, `shutdownTimeout`, `logger`, `recover`) consistent Tasks 2, 4, 5, 6.
- `runService` signature evolves intentionally: `(ctx, svc)` in Task 4 → `(ctx, svc, log, recoverPanics)` in Task 6; call site updated in the same task (Task 6, Step 3). No stale callers.
- `remainingNames(map[int]string) []string`, `resolveLogger(*slog.Logger) *slog.Logger`, `warnDuplicateNames(*slog.Logger, []Service)` referenced consistently.
- `fakeService`/`discardLogger` test helpers defined once (Tasks 2 and 4 respectively) and reused.

No inconsistencies found.
