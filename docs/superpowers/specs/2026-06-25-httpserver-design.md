# Design: `httpserver` — net/http port service + Config-struct support

- **Date:** 2026-06-25
- **Status:** Draft for review
- **Scope:** A new `httpserver` package — a graceful HTTP server that satisfies the
  `supervisor.Service` interface — plus an additive, env-loadable `Config` struct
  for **both** `httpserver` and the existing `supervisor` package.

## Overview

`httpserver` is the first network **port** foreshadowed in the supervisor spec's
"Future fit" section: a long-running HTTP server you register with
`supervisor.WithService(...)`. It wraps `net/http`, runs `Serve` until the shared
context is cancelled, then drains in-flight requests gracefully within its own
bounded deadline before force-closing stragglers. It is router-agnostic — it
accepts any `http.Handler`.

This spec also introduces a recurring convention across the framework: a package's
**serializable settings live in an exported `Config` struct** (data only,
`env`-tagged) with a `DefaultConfig()` constructor and a `WithConfig(Config)`
option, while **non-serializable settings (loggers, listeners, callbacks) remain
functional options**. This makes every package directly loadable by a struct
config loader (e.g. `github.com/caarlos0/env/v11`) without the framework taking
on any dependency.

## Goals

- Run an HTTP server as a `supervisor.Service` (`Name()` + `Run(ctx) error`).
- **Optimal, secure defaults** applied automatically — `New(handler)` with no
  options is a complete, production-reasonable server. net/http's "no timeouts"
  footgun is closed by default.
- **Graceful, bounded shutdown**: on ctx cancel, stop accepting, drain in-flight
  requests up to a per-server deadline, then force-close. The supervisor's grace
  timeout remains the outer bound across all services.
- **Config = data, Option = code.** All serializable settings in one env-loadable
  `Config`; loggers/listeners/TLS objects/callbacks as options.
- No new third-party dependencies in production code; tests use only the
  already-permitted `testify`.
- Apply the same `Config`/`DefaultConfig`/`WithConfig` treatment to `supervisor`.

## Non-goals

- gRPC / WebSocket / SSE port services (separate, later packages; each implements
  `supervisor.Service`).
- A router, middleware stack, or handler helpers. The caller brings the handler.
- HTTP/2 tuning knobs, `h2c`, connection-draining metrics, or per-route timeouts.
- Restart/backoff. First exit stops all — that is the supervisor's job.
- Importing or wrapping any specific config loader. `Config` is plain data with
  inert struct tags.

## Package & module

- Import path: `github.com/dmitrymomot/forge/httpserver`, package `httpserver`.
- Flat layout, stdlib only: `net`, `net/http`, `crypto/tls`, `context`,
  `log/slog`, `time`, `errors`, `fmt`.
- File layout (mirrors `supervisor`):

  ```
  httpserver/
    server.go       # Server type, New, Name, Run, shutdown coordination
    config.go       # Config (exported data) + DefaultConfig
    options.go      # Option, all With* options, internal config
    errors.go       # sentinel errors
    doc.go          # package documentation
    server_test.go
    config_test.go
    options_test.go
  ```

## Public API

### Config (serializable, env-loadable)

```go
// Config holds the serializable settings for a Server. The env struct tags are
// inert strings — this package imports no config loader. A consumer may populate
// Config with any loader that reads `env` struct tags, typically by seeding from
// DefaultConfig and parsing the environment over it. Field order is subject to the
// repo's betteralign tooling and may differ from the table below.
type Config struct {
    Addr              string        `env:"ADDR"`                // default ":8080"; ignored if WithListener is used
    Name              string        `env:"NAME"`                // empty -> Name() derives from listener/Addr
    ShutdownTimeout   time.Duration `env:"SHUTDOWN_TIMEOUT"`    // drain bound; 0 = wait indefinitely
    ReadHeaderTimeout time.Duration `env:"READ_HEADER_TIMEOUT"` // Slowloris guard
    ReadTimeout       time.Duration `env:"READ_TIMEOUT"`        // full request read
    WriteTimeout      time.Duration `env:"WRITE_TIMEOUT"`       // response write; set 0 for SSE/streaming
    IdleTimeout       time.Duration `env:"IDLE_TIMEOUT"`        // keep-alive idle
    MaxHeaderBytes    int           `env:"MAX_HEADER_BYTES"`    // header size cap
    TLSCertFile       string        `env:"TLS_CERT_FILE"`       // both cert+key set -> serve HTTPS
    TLSKeyFile        string        `env:"TLS_KEY_FILE"`
}

// DefaultConfig returns the optimal defaults. It is the single source of truth
// for defaults; there are no envDefault tags to drift from it.
func DefaultConfig() Config
```

`DefaultConfig()` values:

| Field | Default | Note |
|---|---|---|
| `Addr` | `":8080"` | |
| `Name` | `""` | empty → `Name()` derives `"http " + Addr` |
| `ShutdownTimeout` | `15 * time.Second` | drain, then force-close; `0` = wait forever |
| `ReadHeaderTimeout` | `10 * time.Second` | Slowloris guard |
| `ReadTimeout` | `30 * time.Second` | raise for large uploads |
| `WriteTimeout` | `30 * time.Second` | **set `0` for SSE/streaming** (long responses) |
| `IdleTimeout` | `120 * time.Second` | |
| `MaxHeaderBytes` | `1 << 20` (1 MiB) | |
| `TLSCertFile` / `TLSKeyFile` | `""` | plaintext unless both set (or `WithTLSConfig`) |

**Env-loading notes** (apply to any `env`-tag loader; verified against
`caarlos0/env/v11`):

- Seed from `DefaultConfig()` then parse over it — an absent env var leaves the
  seeded default intact (there are no `envDefault` tags to override it).
- A **present-but-empty** var (`HTTP_ADDR=`) is treated as *unset*: it does not
  blank a field. Provide a non-empty value to override a string field.
- **Duration** fields need a unit: `HTTP_READ_TIMEOUT=30s`, not `30`. To disable
  `WriteTimeout` for SSE via env, use the literal `HTTP_WRITE_TIMEOUT=0`.
- **Bool** fields accept only Go `strconv.ParseBool` tokens (`true/false/1/0`),
  not `yes/no/on/off`.
- A parse error can leave the struct **partially** populated, so callers must
  check the returned error (see Usage) and treat it as fatal — never discard it.

### Server, New, Service interface

```go
// Server is a single-use HTTP service. After Run returns, the Server must not be
// reused (the underlying http.Server cannot be restarted).
type Server struct { /* unexported */ }

// New builds a Server. The handler is required and is the only positional
// argument. New does no I/O: the internal config is seeded from DefaultConfig()
// and then each option is applied in order, so New(handler) alone is a complete
// server running on every default. Binding happens in Run.
func New(handler http.Handler, opts ...Option) *Server

func (s *Server) Name() string                  // cfg.Name; else "http "+listener.Addr() if WithListener; else "http "+cfg.Addr
func (s *Server) Run(ctx context.Context) error // supervisor.Service: serve until ctx cancel, then drain
```

### Options

Two **data conveniences** (server identity, the common inline case) and five
**code-only** options (values that cannot come from env):

```go
// WithConfig replaces the entire serializable data block at once. Build the
// argument from DefaultConfig() (or an env-parsed copy of it); a bare Config{}
// literal would zero the timeouts. Because options apply in order, place
// WithConfig before any WithAddr/WithName you want to take precedence.
func WithConfig(cfg Config) Option

func WithAddr(addr string) Option   // convenience: sets Config.Addr
func WithName(name string) Option   // convenience: sets Config.Name (multi-server)

func WithLogger(l *slog.Logger) Option                       // default slog.Default(); nil -> discard; bridged to srv.ErrorLog
func WithListener(ln net.Listener) Option                    // pre-bound listener; overrides Addr (:0 tests, unix sockets, socket activation)
func WithTLSConfig(cfg *tls.Config) Option                   // mTLS / autocert; cert files in Config still work
func WithBaseContext(fn func() context.Context) Option       // root context for every request; force-close cancel layered on top
func WithConnState(fn func(net.Conn, http.ConnState)) Option // passthrough connection-state hook (metrics)
```

All other tuning (timeouts, max header bytes, TLS file paths) is `Config`-only —
adjust via a `DefaultConfig()`-derived value or env. This keeps the option list
to identity + code, honoring "options only for the uncommon case."

### Internal config

```go
type config struct {
    Config                                          // embedded serializable data
    handler     http.Handler
    logger      *slog.Logger
    listener    net.Listener
    tlsConfig   *tls.Config
    baseContext func() context.Context
    connState   func(net.Conn, http.ConnState)
}
```

`New` builds `config{Config: DefaultConfig(), handler: handler, logger: slog.Default()}`,
applies options, and stores it on the `Server`.

## Run / shutdown algorithm

1. **Validate & resolve:** if `handler == nil`, return `ErrNoHandler`. Resolve the
   logger once into a local — `log := resolveLogger(cfg.logger)` (`nil` → discard
   handler), exactly as supervisor does — and use `log` for everything below,
   including the `ErrorLog` bridge (so `WithLogger(nil)` never nil-panics).
2. **Build a local `*http.Server`** from config (built inside `Run`, not stored on
   `Server` — a `Server` is single-use): `Handler`, `ReadHeaderTimeout`,
   `ReadTimeout`, `WriteTimeout`, `IdleTimeout`, `MaxHeaderBytes`, `ConnState`
   (if set), `TLSConfig` (if set), and
   `ErrorLog = slog.NewLogLogger(log.Handler(), slog.LevelError)` so net/http's
   internal connection errors flow through slog as structured records.
3. **Base context (force-close cancellation):**
   ```go
   base := context.Background()
   if cfg.baseContext != nil { base = cfg.baseContext() }
   baseCtx, baseCancel := context.WithCancel(base)
   defer baseCancel()
   srv.BaseContext = func(net.Listener) context.Context { return baseCtx }
   ```
   `baseCtx` is rooted at the caller's base (or `Background`) — **not** at the
   incoming `ctx` — so request contexts are *not* cancelled when shutdown begins
   (that would abort in-flight work and defeat graceful drain). `baseCtx` is
   cancelled only at the force-close step (step 6), giving long handlers a "last
   call."
4. **Bind:** if `cfg.listener != nil`, use it; otherwise
   `ln, err := net.Listen("tcp", cfg.Addr)` — a bind failure (port in use) returns
   `err` here and becomes the supervisor's "first exit" trigger. Log INFO
   `http server listening` with `addr=ln.Addr().String()` (resolves `:0`).
5. **Serve in a goroutine.** TLS precedence: `WithTLSConfig` wins over Config cert
   files. net/http's `ServeTLS` loads the cert/key files whenever *either* path is
   non-empty, so when an in-memory `tls.Config` is supplied we must pass empty paths
   or the files would clobber its certs:
   ```go
   serveErr := make(chan error, 1)
   go func() {
       switch {
       case cfg.tlsConfig != nil:
           // In-memory config wins; pass empty paths so ServeTLS keeps its certs.
           // Precondition: tlsConfig must set Certificates or GetCertificate
           // (or GetConfigForClient), else ServeTLS fails and Run returns that error.
           serveErr <- srv.ServeTLS(ln, "", "")
       case cfg.TLSCertFile != "" && cfg.TLSKeyFile != "":
           serveErr <- srv.ServeTLS(ln, cfg.TLSCertFile, cfg.TLSKeyFile)
       default:
           serveErr <- srv.Serve(ln)
       }
   }()
   ```
   Using `Serve`/`ServeTLS` on a listener we own (rather than `ListenAndServe*`)
   unifies the code path and lets us log the resolved address.
6. **Select** on the first of:
   - **`serveErr` returns first** — serving failed before any shutdown. If it is
     `http.ErrServerClosed`, return `nil` (clean); otherwise return the error.
   - **`ctx.Done()`** — begin graceful shutdown. Crucially, do **not** discard the
     value drained from `serveErr`: a real serve error can race with ctx
     cancellation, and silently dropping it would make `Run` report a clean stop for
     a genuine failure. Always read `serveErr` and surface any
     non-`ErrServerClosed` value:
     ```go
     log.Info("http server shutting down")
     shutCtx := context.Background()
     if cfg.ShutdownTimeout > 0 {
         var cancel context.CancelFunc
         shutCtx, cancel = context.WithTimeout(shutCtx, cfg.ShutdownTimeout)
         defer cancel()
     }
     shutErr := srv.Shutdown(shutCtx)
     if shutErr != nil { // deadline exceeded
         log.Error("graceful shutdown timed out, forcing close")
         baseCancel()    // best-effort "last call"; see note below
         _ = srv.Close() // drop remaining connections
     }
     serveResult := <-serveErr // ALWAYS read; never discard
     if serveResult != nil && !errors.Is(serveResult, http.ErrServerClosed) {
         return serveResult    // a real serve error wins over shutdown status
     }
     if shutErr != nil {
         return ErrShutdownTimeout
     }
     log.Info("http server stopped")
     return nil
     ```
   `ShutdownTimeout == 0` → `srv.Shutdown(context.Background())` waits indefinitely
   (documented). The force-close `baseCancel()` is **best-effort**: `srv.Close()`
   tears down connections concurrently, so a handler watching `r.Context().Done()`
   receives the cancellation signal but is not guaranteed a window to flush a final
   response before its connection drops. This `baseCancel()` is also covered by the
   `defer baseCancel()` from step 3 — calling it twice is intentionally idempotent.

## Errors

```go
var (
    ErrNoHandler       = errors.New("httpserver: nil handler")
    ErrShutdownTimeout = errors.New("httpserver: graceful shutdown timed out")
)
```

`Run` returns: `nil` on a clean stop (including `http.ErrServerClosed`); the
bind/serve error on startup failure; `ErrShutdownTimeout` when the drain deadline
was exceeded and connections were force-closed; `ErrNoHandler` if constructed
with a nil handler. All matchable with `errors.Is`. Error values are single-line;
diagnostics are slog attributes — per the framework's logging convention.

## Logging

Through the configured `*slog.Logger`, structured attributes only:

- `INFO` — `http server listening` (`addr`), `http server shutting down`,
  `http server stopped`.
- `ERROR` — `graceful shutdown timed out, forcing close`.
- net/http internal errors arrive via the bridged `srv.ErrorLog`.

The supervisor already logs `service started` / `service stopped`; httpserver adds
only the resolved-address and drain detail it uniquely knows.

## Supervisor: additive Config-struct support

Backward-compatible. No behavior change for existing callers.

```go
// Config holds the serializable settings for Run. Tags are inert; the package
// imports no loader.
type Config struct {
    ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT"` // default 30s; 0 = wait indefinitely
    Recover         bool          `env:"RECOVER"`          // default true
}

func DefaultConfig() Config           // { 30 * time.Second, true }
func WithConfig(cfg Config) Option    // sets the whole data block
```

The internal unexported `config` is refactored to embed the exported `Config`;
`services` and `logger` stay non-serializable:

```go
type config struct {
    Config
    services []Service
    logger   *slog.Logger
}

func defaultConfig() config {
    return config{Config: DefaultConfig(), logger: slog.Default()}
}
```

`WithShutdownTimeout` / `WithRecover` now set `c.ShutdownTimeout` / `c.Recover`;
all existing options and **API-level** behavior are unchanged. Required edits
beyond the new code:

- **Remove** the existing `const defaultShutdownTimeout = 30 * time.Second`
  (`supervisor/options.go:9`); `DefaultConfig()` becomes the single source of the
  30s / `true` defaults (no second place to drift).
- **Update existing white-box tests** that read the renamed internal fields:
  `supervisor/options_test.go` references `cfg.shutdownTimeout` / `cfg.recover`
  (and `supervisor_test.go` where applicable) must move to the embedded
  `cfg.ShutdownTimeout` / `cfg.Recover`. "No behavior change for callers" holds at
  the API surface, but these in-package tests must be edited to compile.
- Update `supervisor.go` references from `cfg.shutdownTimeout`/`cfg.recover` to the
  embedded fields.
- Place `Config` + `DefaultConfig` in a new `supervisor/config.go`, keeping
  `options.go` code-only — matching the httpserver layout.

Like httpserver, `supervisor.WithConfig` replaces the whole data block, so build
its argument from `DefaultConfig()`. A bare `supervisor.Config{}` sets
`ShutdownTimeout=0` (wait indefinitely) **and** `Recover=false` (disables panic
recovery) — a silent, dangerous change for this package; never pass a bare literal.

## Usage

```go
// env-driven: seed defaults, then parse the environment over it.
// Each package needs its OWN prefix — keys like SHUTDOWN_TIMEOUT repeat across
// httpserver and supervisor, so a shared/empty prefix would collide.
cfg := httpserver.DefaultConfig()
if err := env.ParseWithOptions(&cfg, env.Options{Prefix: "HTTP_"}); err != nil {
    log.Fatalf("http config: %v", err) // a parse error may leave cfg partially set — treat as fatal
}
api := httpserver.New(router,
    httpserver.WithConfig(cfg),
    httpserver.WithLogger(logger),
)

// simple / multi-server (distinct prefixes avoid env collisions; distinct names avoid the supervisor's dup warning)
admin := httpserver.New(adminMux,
    httpserver.WithAddr(":9090"),
    httpserver.WithName("admin"),
)

ctx, stop := supervisor.NewContext()
defer stop()
err := supervisor.Run(ctx,
    supervisor.WithService(api),
    supervisor.WithService(admin),
    supervisor.WithConfig(supervisor.DefaultConfig()), // optional; these are already the defaults
)
if err != nil {
    logger.Error("runtime stopped", "err", err)
    os.Exit(1)
}
```

## Edge cases

- **`New(handler)` with no options** → full `DefaultConfig()` server on `:8080`.
- **Nil handler** → `Run` returns `ErrNoHandler` (we do not silently fall back to
  `http.DefaultServeMux`).
- **Bare `Config{}` via `WithConfig`** → zeroed timeouts (documented footgun; build
  from `DefaultConfig()`).
- **Bind failure** (port in use) → `Run` returns the listen error; the supervisor
  treats it as a first-exit and drains siblings.
- **`WithListener`** → the listener wins; `Addr` is ignored for binding. For
  `Name()`, an empty `Name` derives from the **listener's** address
  (`"http " + ln.Addr()`), not the unused `Addr` default — so a listener-based
  server is not misnamed `http :8080`. Setting `WithName` is still recommended.
- **`WriteTimeout > 0` + SSE/streaming** → long responses are cut; set
  `WriteTimeout = 0` for streaming servers.
- **`ShutdownTimeout == 0`** + a handler that ignores `ctx` → `Run` blocks until the
  handler returns. The supervisor's outer grace timeout bounds the process **only
  if it is non-zero**; if both timeouts are `0`, a stuck handler hangs the process.
- **Reuse** → a `Server` is single-use: the `*http.Server` is built inside `Run`.
  Constructing a fresh `Server` per run is cheap; reusing one after `Run` has
  returned is unsupported.

## Testing

White-box (`package httpserver`) so tests can assert the built `*http.Server` and
the config without exporting internals. No third-party test dependencies
(testify is already permitted; **`env/v11` is not imported, even in tests**).

- **Defaults & options:** `DefaultConfig()` values; `New` seeds them; option
  ordering; `WithConfig` wholesale replace; `Name()` derivation (`""` → `"http "+Addr`).
- **env-tag contract:** a reflection test asserts each `Config` field carries the
  expected `env:"..."` tag (locks the loader contract without importing a loader).
  Same test added for `supervisor.Config`.
- **Round-trip:** `WithListener` on `127.0.0.1:0`; start `Run` in a goroutine,
  issue a request, cancel ctx, assert clean `nil` return.
- **Graceful drain:** a handler that sleeps; cancel ctx mid-request; assert the
  in-flight request completes within the deadline and `Run` returns `nil`.
- **Force-close:** a handler that blocks past `ShutdownTimeout`; assert `Run`
  returns `ErrShutdownTimeout`; a streaming handler observes `r.Context().Done()`
  from the force-close `baseCancel()`.
- **Bind failure:** occupy a port, start a second server, assert `Run` returns the
  listen error.
- **TLS:** generated self-signed cert — both the `WithTLSConfig` (in-memory) path
  and the cert/key-file path; assert `WithTLSConfig` takes precedence when files
  are also set.
- **Lost-error race:** a serve error arriving as ctx is cancelled must be returned
  by `Run`, not masked as a clean stop (e.g. close the injected listener out from
  under `Serve` at shutdown and assert the error surfaces).
- **Nil handler:** `New(nil).Run(ctx)` returns `ErrNoHandler`.
- **Supervisor integration:** register a server with `supervisor.Run`; assert
  coordinated shutdown on ctx cancel.
- **Supervisor Config:** `WithConfig` sets `ShutdownTimeout`/`Recover`; defaults
  unchanged for existing callers. **Update the existing `supervisor/options_test.go`
  (and `supervisor_test.go`) references** from `cfg.shutdownTimeout`/`cfg.recover`
  to the embedded `cfg.ShutdownTimeout` / `cfg.Recover` so the package compiles
  after the refactor.

Time-based tests use small real durations (tens of ms); no clock abstraction.

## Future fit

gRPC, WebSocket, and SSE ports follow the same shape: a `New(...)` returning a
type that implements `supervisor.Service`, a serializable `Config` + `DefaultConfig`
+ `WithConfig`, and code-only functional options. The `Config = data / Option =
code` split becomes the framework-wide convention for env-loadable packages.

Because each package's `Config` uses prefix-less keys, an app composing several of
them must give each its own namespace: call `ParseWithOptions` once per `Config`
with a distinct `Prefix`, or, when nesting them in an app-level struct, tag each
**named** nested field with `envPrefix:"..."`. (Anonymous embedding shares the
parent's namespace — which is exactly why the internal `config` can embed `Config`
without a prefix.)

## Deferred

- HTTP/2 (`h2c`), connection-count metrics beyond the `WithConnState` passthrough.
- A shared `service` base package (only worthwhile once 2–3 ports exist and real
  duplication appears).
- Per-route or per-handler timeouts (belongs in middleware, not the port).
- Hot-reload of `Config` at runtime.
