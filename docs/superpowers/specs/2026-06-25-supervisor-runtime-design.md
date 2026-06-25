# Design: `supervisor` — process runtime + main-context helper

- **Date:** 2026-06-25
- **Status:** Approved for planning
- **Scope:** The `supervisor` package only — the process-level runtime that runs services with coordinated graceful shutdown, plus the `NewContext` signal helper. The higher-level `service` base (http/grpc/websocket/sse ports, worker-only services) is a separate, later spec that builds on this one.

## Overview

`supervisor` is the foundation of a Forge application: the thing `main` calls to run one or more long-running **services** (an HTTP server, a background job loop, etc.) under a single coordinated lifecycle. It owns nothing about *what* a service does — only *that* it runs until told to stop, and that when one stops, they all stop, gracefully, within a bounded time.

It is intentionally tiny, dependency-free, and fully testable. Every other runnable thing in the framework ultimately plugs into it by satisfying one interface.

## Goals

- Run N services concurrently as peers under one shared, cancellable context.
- Coordinated shutdown: the **first** service to return (nil **or** error), or an OS signal, triggers shutdown of **all** services.
- **Graceful drain, then bounded abandon**: after the trigger, wait for every service to finish on its own; if a grace deadline passes, stop waiting, report which services are stuck, and return so the process can exit. (Go cannot forcibly kill a goroutine — "kill" means abandon + process exit.)
- One obvious setup path: a single `Run(ctx, opts...)` function. No builder, no constructor, no exported runtime type.
- A one-line helper to create the `main` context wired to SIGINT/SIGTERM.
- Mandatory, meaningful service names for log correlation.
- 100% testable with no real OS signals required for the supervision logic.

## Non-goals (out of scope for this spec)

- The `service` base package and any network **ports** (http/grpc/websocket/sse). Those are separate specs; they will simply implement `Service`.
- Ordered startup, dependency graphs, or readiness/health gating between services. All services here are peers, started together and stopped together.
- Restart/backoff/supervision-tree policies. First exit stops all; there is no per-service restart.
- Custom signal sets, a second-signal force-quit, or injectable clocks. Deliberately omitted as YAGNI (see "Deferred").

## Package & module

- Import path: `github.com/dmitrymomot/forge/supervisor`
- Package name: `supervisor` (chosen over `runtime` to avoid shadowing the standard library `runtime`).
- Flat layout, no nested directories, no third-party dependencies (stdlib only: `context`, `os`, `os/signal`, `syscall`, `log/slog`, `time`, `errors`, `fmt`, `runtime/debug`).

## Public API

### Service contract

```go
// Service is a long-running unit of work supervised by Run.
//
// Name returns a non-empty, human-readable identifier used in logs and
// shutdown diagnostics. It must be stable for the lifetime of the service.
//
// Run blocks until the work completes or ctx is cancelled. Implementations
// MUST observe ctx cancellation and shut themselves down gracefully, returning
// when drained. Returning context.Canceled in response to cancellation is
// treated as a clean stop (it is filtered from the aggregated error).
type Service interface {
    Name() string
    Run(ctx context.Context) error
}
```

`Name()` is part of the interface by design: there is no way to register an anonymous service.

### Run and Options

```go
// Option configures a Run call: it registers services and tunes behavior.
type Option func(*config)

// WithService registers a Service.
func WithService(svc Service) Option

// WithServiceFunc registers a named function as a service. name must be non-empty.
func WithServiceFunc(name string, fn func(ctx context.Context) error) Option

// WithShutdownTimeout bounds how long Run waits for services to drain after
// shutdown begins. Default 30s. A value of 0 means wait indefinitely.
func WithShutdownTimeout(d time.Duration) Option

// WithLogger sets the slog.Logger used for lifecycle logging.
// Default: slog.Default(). Passing nil installs a discard handler.
func WithLogger(l *slog.Logger) Option

// WithRecover toggles panic recovery in each service's Run. Default true:
// a panic is converted to an ErrPanic-wrapped error (which triggers shutdown,
// so siblings still drain) instead of crashing the process mid-flight.
func WithRecover(enabled bool) Option

// Run is the only entry point. It starts every registered service, supervises
// them, and blocks until shutdown completes. It returns the joined errors from
// services and/or ErrShutdownTimeout, or nil on a clean stop.
func Run(ctx context.Context, opts ...Option) error
```

`WithServiceFunc` wraps `name`+`fn` in an unexported adapter satisfying `Service`. Options accumulate into an unexported `config`; there is no exported `Runtime`/`New`.

```go
type config struct {
    services        []Service
    shutdownTimeout time.Duration
    logger          *slog.Logger
    recover         bool
}
```

### NewContext

```go
// NewContext returns a context that is cancelled on the first SIGINT or
// SIGTERM, implemented with signal.NotifyContext. It is single-shot: after the
// first signal the context is cancelled and further signals are not handled by
// this helper. Call stop (typically deferred in main) to release the handler.
func NewContext() (ctx context.Context, stop context.CancelFunc)
```

Equivalent to `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`. Zero-arg by design — it exists to make `main` a two-liner.

## Shutdown semantics (algorithm)

1. Build `config` from options; resolve the logger (default `slog.Default()`, `nil` → discard handler).
2. **Validate:**
   - No services → return `ErrNoServices`.
   - Any service with an empty `Name()` → return `ErrUnnamedService`.
   - Duplicate names are allowed but logged once at `WARN` (they only hurt log readability).
3. If the parent `ctx` is already cancelled, log and return `nil` without launching anything.
4. Derive `runCtx, cancel := context.WithCancel(ctx)`; `defer cancel()`.
5. Launch each service in its own goroutine; each sends `{name, err}` to a buffered results channel of capacity `len(services)`. Maintain a `remaining` set of running service names.
6. Select loop over three cases: a **result** arriving, `runCtx.Done()`, and a **grace timer** (nil until shutdown begins, and never armed when `shutdownTimeout == 0`). "Begin shutdown" is an idempotent step — the first time it runs it logs the reason, `cancel()`s the shared context (all siblings observe it and drain), stops the loop from selecting on `Done()` again, and arms `time.After(shutdownTimeout)` if the timeout is > 0; subsequent calls are no-ops.
   - **A result arrives** — remove the name from `remaining`; if `err != nil` and not `context.Canceled`, append `service %q: %w` to the error list (`context.DeadlineExceeded` is **not** filtered — that's a real failure); then **begin shutdown** (no-op if already shutting down). The loop continues until `remaining` is empty.
   - **`Done()` fires** (OS signal / parent cancel) — **begin shutdown**.
   - **Grace timer fires** — append `ErrShutdownTimeout` annotated with the still-`remaining` names, log them at `ERROR`, and return immediately. Stuck goroutines are abandoned; the process exits.
7. When all services have returned, log "shutdown complete" and return `errors.Join(errs...)` (nil if none).

Panic handling wrapper:

```go
func runService(ctx context.Context, svc Service, recoverPanics bool) (err error) {
    if recoverPanics {
        defer func() {
            if p := recover(); p != nil {
                err = fmt.Errorf("%w: %v\n%s", ErrPanic, p, debug.Stack())
            }
        }()
    }
    return svc.Run(ctx)
}
```

A recovered panic becomes a normal service error, which is itself a "first exit" trigger — so a single panicking worker brings the process down *gracefully* (siblings drain) rather than aborting the whole process immediately. Default: enabled.

**Scope caveat (to be stated in the godoc):** recovery only catches panics that propagate out of a service's own top-level `Run` goroutine. It does **not** catch panics in goroutines a service spawns internally — e.g. an HTTP handler panic runs in `net/http`'s per-connection goroutine and must be recovered by HTTP middleware, not here. `WithRecover` is a backstop for the service's main loop, not a process-wide panic shield.

## Options & defaults

| Option | Default | Notes |
|---|---|---|
| `WithService(svc)` | — | Append a `Service`. |
| `WithServiceFunc(name, fn)` | — | Append a named func service; `name` must be non-empty. |
| `WithShutdownTimeout(d)` | `30 * time.Second` | Grace bound on drain. `0` = wait indefinitely. |
| `WithLogger(l)` | `slog.Default()` | `nil` → discard handler (no nil deref). |
| `WithRecover(enabled)` | `true` | Recover panics in `Run`, convert to `ErrPanic`. |

## Errors

```go
var (
    ErrNoServices      = errors.New("supervisor: no services registered")
    ErrUnnamedService  = errors.New("supervisor: service has empty name")
    ErrShutdownTimeout = errors.New("supervisor: graceful shutdown timed out")
    ErrPanic           = errors.New("supervisor: service panicked")
)
```

`Run`'s return is `errors.Join` of: each service's non-`context.Canceled` error (wrapped with its name), plus `ErrShutdownTimeout` if the grace deadline was exceeded. Callers can match any of the above with `errors.Is`.

## Logging

Via the configured `*slog.Logger`. Events:

- `INFO` — service started (`service`), shutdown started (`reason`), service stopped (`service`, `err`), shutdown complete.
- `WARN` — duplicate service names detected.
- `ERROR` — graceful shutdown timed out (`stuck` = list of remaining service names).

No values are smuggled through `context`; the logger is passed explicitly and used only by the supervisor.

## Edge cases

- **Empty registration** → `ErrNoServices`.
- **Empty name** (including `WithServiceFunc("", fn)`) → `ErrUnnamedService`.
- **Single service** → works; its exit is the "first exit" that stops "all".
- **Instant clean exit** of any service → triggers full shutdown (by design; surfaces misconfiguration rather than silently running half the app).
- **Parent ctx already cancelled** at entry → log + return `nil`, nothing launched.
- **`context.Canceled` from a draining service** → filtered (expected). **`context.DeadlineExceeded`** → reported.
- **`shutdownTimeout == 0`** + a service that ignores `ctx` → `Run` blocks forever (documented "wait indefinitely").

## Testability

No hidden global state; the supervision logic is driven entirely by the passed-in `ctx` and the registered services.

- **Supervision** is tested with a plain `context.WithCancel` standing in for a signal — no real OS signals. Fake services cover every branch:
  - instant-return nil, instant-return error,
  - blocks-until-ctx-then-returns,
  - **ignores-ctx** (asserts `ErrShutdownTimeout` + abandon),
  - **panics** (asserts `WithRecover` on and off).
- **First-exit-stops-all** asserted by registering a short-lived service alongside a blocking one and checking the blocking one is cancelled.
- **Error aggregation / `context.Canceled` filtering** asserted via `errors.Is` on the joined result.
- **`NewContext`** tested by sending a real signal to the test process (`syscall.Kill(syscall.Getpid(), syscall.SIGTERM)`) and asserting cancellation.
- **Logging** asserted with a buffer-backed `slog.Handler`.
- Time-based tests use small real durations (tens of ms). No clock abstraction: the only timing primitive is the single grace timer, so injecting a clock would be over-engineering.

## File layout

```
supervisor/
  supervisor.go   # Run, config, supervision loop, runService
  service.go      # Service interface, WithServiceFunc adapter
  context.go      # NewContext
  options.go      # Option, WithService, WithServiceFunc, WithShutdownTimeout, WithLogger, WithRecover
  errors.go       # sentinel errors
  doc.go          # package documentation
  supervisor_test.go
  context_test.go
```

## Example usage

```go
func main() {
    ctx, stop := supervisor.NewContext()
    defer stop()

    err := supervisor.Run(ctx,
        supervisor.WithService(httpSvc),                 // implements Service
        supervisor.WithServiceFunc("cleanup", cleanup.Loop),
        supervisor.WithShutdownTimeout(20*time.Second),
        supervisor.WithLogger(logger),
    )
    if err != nil {
        logger.Error("runtime stopped", "err", err)
        os.Exit(1)
    }
}
```

## Future fit

The forthcoming `service` package (separate spec) will provide base types for HTTP/gRPC/WebSocket/SSE port services and worker-only services. Each will satisfy this `Service` interface (`Name()` + `Run(ctx) error`) and be registered with `WithService`, so the supervisor needs no changes to accommodate them. An HTTP port, for example, will run `srv.ListenAndServe()` in `Run`, and on `ctx` cancellation call `srv.Shutdown` with its own drain deadline — the supervisor's grace timeout is the outer bound across all services.

## Deferred (possible later, not now)

- Custom signal set for `NewContext` (e.g., `SIGHUP`).
- Second-signal force-quit in `NewContext`.
- Per-service "may complete" opt-out from first-exit-stops-all.
- Injectable clock for deterministic timeout tests.
