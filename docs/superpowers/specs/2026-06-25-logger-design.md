# Design: `logger` — slog factory, context extraction, file output + Sentry adapter

- **Date:** 2026-06-25
- **Status:** Draft for review (rev. 2 — Sentry is a sibling factory `sentry.New(opts…)`,
  not a `Wrap`; core gains a generic `WithHandler` destination hook; the context
  decorator is internal)
- **Scope:** A new `logger` package — an `slog.Logger` factory with context-attribute
  extraction, an optional local-dev file destination, and a discard logger — plus a
  separate `logger/sentry` package whose `New(opts…)` builds a logger that *also*
  reports to Sentry **in parallel, at its own level**, with the one unavoidable external
  dependency (the Sentry SDK) isolated to that subpackage.

## Overview

`logger` is a thin, composable layer over the standard library's `log/slog`. It does
four things and nothing more:

1. Builds a configured `*slog.Logger` from functional options (`New`).
2. Injects request-scoped attributes from `context.Context` on every log call (the
   `ContextExtractor` seam; the handler that applies them is internal).
3. Writes to a **single** primary destination — stdout by default, **or** a local file
   (not both): when a file path is configured, all records go to that file *instead of*
   stdout (no rotation — a plain append file for local development), creating the file
   and any missing parent directories.
4. Provides a no-op discard logger (`NewNope`) for defaults and tests.

It also exposes one general extension point — **`WithHandler(h slog.Handler)`** — that
adds an extra *parallel* destination alongside the primary one, beneath context
extraction. This is the seam adapters plug into.

The **`logger/sentry`** package is one such adapter, shaped like every other forge
package: it owns an env-loadable `Config`, options, and a constructor
`New(opts …Option) (*slog.Logger, Flush, error)`. Internally it builds a Sentry
`slog.Handler` and calls `logger.New(append(loggerOpts, logger.WithHandler(sentryH))…)`.
The dependency direction is clean: `sentry` imports `logger` and passes a built handler
*into* it; core `logger` never imports Sentry and stays 100% standard library. Sentry
is the only *parallel* destination — stdout and file remain mutually exclusive in core.

This spec follows the framework conventions established by `supervisor` and
`httpserver`: serializable settings live in an env-loadable `Config` (data only) with
`DefaultConfig()`/`Validate()`/`WithConfig(...)`; non-serializable settings (writers,
extractors, handlers) are functional options; option values are validated zero-trust
and errors accumulate; sentinel errors live in `errors.go`; diagnostics are slog
attributes and errors are single-line.

## Goals

- **`New(opts...)` returns a ready `*slog.Logger`.** With no options it is a complete,
  reasonable logger: text format, info level, stdout.
- **Context extraction** of request-scoped attributes (request id, user id, tenant
  id, …) injected fresh on every log call, landing at the record's **top level** even
  when the caller has opened groups — and reaching **every** destination, including any
  added via `WithHandler`, because the decorator wraps the whole fan-out.
- **File as an alternative primary destination** for local development: when a path is
  configured, all records go to that file *instead of* stdout (the two are mutually
  exclusive — never both at once), creating the file and parent directories if absent.
  No rotation, retention, or compression.
- **Sentry runs parallel to the primary destination at its own, separate level** —
  e.g. primary at `info`, Sentry at `warn` — with zero external dependency in core.
- **Graceful Sentry fallback:** an empty DSN makes `sentry.New` return a plain logger
  immediately (no-op flush, nil error); a Sentry init failure still returns a usable
  logger (plus `ErrSentryInit`).
- **Config = data, Option = code.** Serializable settings in env-loadable `Config`
  structs; writers, extractor funcs, and handlers as options.
- **No third-party dependency in `logger`.** The Sentry SDK is confined to
  `logger/sentry`; tests use only the already-permitted `testify`.

## Non-goals

- **Log rotation, retention, size limits, or compression.** The file destination is
  an append-only sink for local development. Production file management is the job of
  the platform (`logrotate`, container log drivers, Loki/Vector, etc.).
- A logging facade or custom `Logger` type. `New` returns the standard
  `*slog.Logger`; callers use slog directly. The framework passes `*slog.Logger`
  everywhere (`httpserver.WithLogger`, `supervisor`'s logger), so we never wrap it.
- Built-in extractors. `ContextExtractor` is the seam; callers supply the funcs
  (request-id middleware owns the key, not this package).
- **Logging to stdout and a file simultaneously.** The primary destination is exactly
  one writer (stdout XOR file). Teeing to multiple *local* sinks at once is out of
  scope; `WithHandler`/Sentry add *parallel* destinations, which is different.
- A config loader. `Config` is plain data with inert `env` struct tags.
- Per-destination level/format knobs in core. The single primary destination uses the
  global level and format; only handlers added via `WithHandler` (e.g. Sentry) carry
  their own independent level, baked in by whoever builds them.

## Package & module

- Import paths: `github.com/dmitrymomot/forge/logger` (package `logger`) and
  `github.com/dmitrymomot/forge/logger/sentry` (package `sentry`).
- Core is **stdlib only**: `log/slog`, `context`, `io`, `os`, `path/filepath`,
  `strings`, `errors`, `fmt`.
- The one nested folder (`logger/sentry`) is justified: a separate Go package is the
  only way to keep the Sentry SDK out of the core package's import graph. It depends
  on `github.com/getsentry/sentry-go` and `github.com/getsentry/sentry-go/slog`.
- File layout (mirrors `supervisor`/`httpserver`):

  ```
  logger/
    logger.go        # New, NewNope, internal config, handler assembly (primary + WithHandler fan-out)
    decorator.go     # ContextExtractor (exported); contextHandler (internal: Handle + group logic)
    file.go          # openFile: mkdir -p + create + append
    config.go        # Config (env-tagged), DefaultConfig, Validate, parseLevel/parseFormat
    options.go       # Option, WithConfig/WithLevel/WithFormat/WithFile/WithOutput/WithHandler/WithContextExtractors
    errors.go        # ErrInvalidConfig, ErrOpenFile
    doc.go
    logger_test.go
    decorator_test.go
    config_test.go
    options_test.go
    sentry/
      sentry.go               # Config (embeds logger.Config), DefaultConfig, Validate, New, Flush
      errors.go               # ErrInvalidConfig, ErrSentryInit, ErrSentryFlushTimeout
      doc.go
      sentry_test.go          # package sentry_test (external): empty-DSN passthrough, Validate, flush-timeout
      sentry_internal_test.go # package sentry (same pkg, not a subpackage): exercises the unexported
                              #   newWith seam with a fake handler builder → fan-out + extraction, no network
  ```

## Public API — core `logger`

### Config (serializable, env-loadable)

```go
// Config holds the serializable settings for New. The env struct tags are inert
// strings — this package imports no config loader.
type Config struct {
    // Level is the minimum level for the primary destination:
    // "debug", "info", "warn"/"warning", "error" (case-insensitive).
    Level string `env:"LEVEL"`
    // Format selects the handler: "text" or "json" (case-insensitive).
    Format string `env:"FORMAT"`
    // File, when non-empty, makes the primary destination this file INSTEAD of stdout
    // (mutually exclusive — never both). Parent directories and the file are created if
    // absent. Empty means stdout.
    File string `env:"FILE"`
    // AddSource includes the source file:line in records (slog AddSource).
    AddSource bool `env:"ADD_SOURCE"`
}

func DefaultConfig() Config // {Level:"info", Format:"text", File:"", AddSource:false}

// Validate reports whether the field values are usable, returning an
// ErrInvalidConfig-wrapped error otherwise.
func (c Config) Validate() error
```

`DefaultConfig()` is the single source of truth for defaults (no `envDefault` tags to
drift from it).

**`Validate` semantics (concrete):**

| Field | Accepted (case-insensitive) | Otherwise |
|---|---|---|
| `Level` | `"debug"`, `"info"`, `"warn"`/`"warning"`, `"error"` | `ErrInvalidConfig` |
| `Format` | `"text"`, `"json"` | `ErrInvalidConfig` |
| `File` | any string, including `""` (= stdout) | never rejected (created by `New`) |
| `AddSource` | any bool | never rejected |

`Level`/`Format` parse case-insensitively with surrounding whitespace trimmed; `Level`
accepts both `"warn"` and `"warning"` as `slog.LevelWarn`. There is **no silent
fallback** — an unknown value is an error, not a default. `parseLevel`/`parseFormat`
(internal to `logger/config.go`) are called by `New` only after `Validate` passes, so
they assume a member of the validated set.

### New, NewNope

```go
// New builds an *slog.Logger from the options. With no options it returns a
// text-format, info-level logger writing to os.Stdout. If Config.File is set (via
// WithConfig or WithFile), the primary destination becomes that file INSTEAD of stdout
// (the file and any missing parent directories are created). Any handlers added via
// WithHandler run as parallel destinations beneath context extraction. Returns
// ErrInvalidConfig for bad option/Config values and ErrOpenFile if the file cannot be
// opened.
func New(opts ...Option) (*slog.Logger, error)

// NewNope returns a logger that discards all records. Use as a safe default when
// logging is not configured, and in tests.
func NewNope() *slog.Logger
```

`New` returns `(*slog.Logger, error)` — **no closer**. The file destination needs no
explicit `Close`: slog's `TextHandler`/`JSONHandler` issue exactly one
`io.Writer.Write` per `Handle` (flushing their internal per-record buffer each call),
and `(*os.File).Write` is a direct, unbuffered syscall — so every record reaches the
kernel immediately. An append log file opened once for the process lifetime loses no
data when the fd is reclaimed at exit. This keeps the return signature clean and
matches the "local development" scope of the file feature. `New` is safe to call
concurrently (each call opens its own fd; `O_APPEND` writes are atomic per record).
(A `WithHandler` destination that *does* need flushing — e.g. Sentry — is owned by the
adapter that built it; that is why `sentry.New` returns its own `Flush`.)

### Options

```go
type Option func(*config)

// Format is the handler format. FormatText is the default.
type Format string

const (
    FormatText Format = "text"
    FormatJSON Format = "json"
)

// WithConfig sets the whole serializable data block at once. Build the argument from
// DefaultConfig() (or an env-parsed copy). Options apply in order — a later WithConfig
// replaces the whole block (last writer wins), so place WithConfig BEFORE any WithFile
// you want to keep.
func WithConfig(cfg Config) Option

// WithLevel sets an explicit code-level minimum for the primary destination. It is an
// override: it ALWAYS takes precedence over Config.Level regardless of option order.
// Accepts any slog.Level (incl. custom), so it never round-trips through string parsing.
func WithLevel(level slog.Level) Option

// WithFormat sets an explicit code format override. Like WithLevel, it ALWAYS takes
// precedence over Config.Format regardless of option order. Default FormatText.
func WithFormat(f Format) Option

// WithFile routes the primary destination to a file at path INSTEAD of stdout
// (convenience that writes Config.File). An empty path is rejected (ErrInvalidConfig).
// Because it writes the data block, a later WithConfig overrides it. The file and
// parent dirs are created at New.
func WithFile(path string) Option

// WithOutput sets the primary destination's writer directly (tests, custom sinks). As
// a code override it takes precedence over Config.File: if both are set, the file is
// NOT opened and w is used. A nil writer is rejected (ErrInvalidConfig).
func WithOutput(w io.Writer) Option

// WithHandler adds an extra parallel destination that runs ALONGSIDE the primary
// (stdout/file) destination, beneath context extraction. Multiple may be added; each
// filters at its own level via slog.Handler.Enabled. This is the generic seam adapters
// (e.g. logger/sentry) plug into — core stays unaware of them. A nil handler is
// rejected (ErrInvalidConfig).
func WithHandler(h slog.Handler) Option

// WithContextExtractors registers ContextExtractor funcs applied on every log call.
// Nil extractors are filtered. Order is preserved.
func WithContextExtractors(ex ...ContextExtractor) Option
```

**Precedence model (two tiers, stated explicitly to avoid ambiguity):**

- `Config` string fields (`Level`, `Format`, `File`) are the serializable tier. Among
  options that write them — `WithConfig` (whole block) and `WithFile` (one field) —
  **option order decides** (last writer wins); place `WithConfig` first.
- `WithLevel`/`WithFormat`/`WithOutput` set distinct **code-override fields**
  (`*slog.Level`, `*Format`, `io.Writer`) on the internal config. They are resolved last
  and **always win** over the parsed `Config` value, irrespective of where they sit in
  the option list. The split exists because a level has both a serializable string form
  (`Config.Level`) and a precise code form (`slog.Level`); the code form is explicit
  intent.

**Primary-writer resolution (single destination, stdout XOR file):** the one output
writer is resolved as `WithOutput`'s writer if set, else `openFile(Config.File)` if
`File != ""`, else `os.Stdout`. There is never more than one *primary* writer.
`WithHandler` destinations are *parallel*, not alternatives to the primary.

### Context extraction (decorator is internal)

```go
// ContextExtractor extracts a slog attribute from context. Return ok=false to skip.
// This is the ONLY exported extraction type: callers supply funcs; the package owns the
// handler that applies them.
type ContextExtractor func(ctx context.Context) (slog.Attr, bool)
```

Internally, an unexported `contextHandler` wraps the assembled handler and, on every
`Handle`, runs the extractors and injects their results at the record's **top level**
(ahead of any group opened with `WithGroup`). It is ported from the v1 implementation:
extracted attributes are attached to the root handler so they land at the top level
regardless of active groups, with a fast path when no group is open (add directly to the
record) and a rebuild path when a group is active (re-apply recorded ops on top of the
root + extracted attrs). All of its methods are pure — `WithAttrs`/`WithGroup` return a
new handler and never mutate the receiver (slices copied) — so it is safe for concurrent
use, like any `slog.Handler`.

> **Why no exported `Inner`/`WithInner` seam (changed from rev. 1):** composition now
> happens *before* decoration, inside `New`. `New` assembles
> `[primary, …WithHandler handlers]` into a `slog.NewMultiHandler` and then wraps the
> whole fan-out in `contextHandler`. So a Sentry handler added via `WithHandler` sits
> *beneath* the single decorator automatically and receives extracted attributes — no
> handler-peeling, no ops-replay. The decorator stays a private implementation detail.

## Option validation (zero-trust)

Options accumulate invalid values into `config.errs`; `New` joins them and returns
`ErrInvalidConfig` before performing any I/O (no file is created on a bad config).

| Input | Rejected when | Error |
|---|---|---|
| `WithFile(path)` | `path == ""` | `ErrInvalidConfig` |
| `WithOutput(w)` | `w == nil` | `ErrInvalidConfig` |
| `WithHandler(h)` | `h == nil` | `ErrInvalidConfig` |
| `Config.Level` | not a known level name | `ErrInvalidConfig` (from `Validate`) |
| `Config.Format` | not `"text"`/`"json"` | `ErrInvalidConfig` (from `Validate`) |
| `WithContextExtractors` | nil entries | filtered (not an error) |

`WithLevel` takes a `slog.Level` (any int is valid). `Validate` is called by `New`
defensively even if the caller already called it after env loading.

## New algorithm

```
1. c := defaultConfig()                     // {Config: DefaultConfig(), outputOverride: nil,
                                             //  extractors: nil, extraHandlers: nil, overrides: nil}
2. apply opts in order; each invalid value appends fmt.Errorf("%w: ...", ErrInvalidConfig)
   to c.errs. WithConfig replaces the embedded Config block; WithHandler appends to
   c.extraHandlers; WithContextExtractors appends non-nil funcs to c.extractors.
3. if len(c.errs) > 0: return nil, errors.Join(c.errs...)   // each err already wraps
                                                            // ErrInvalidConfig; no I/O
4. if err := c.Validate(); err != nil: return nil, err      // wraps ErrInvalidConfig
5. // Two-tier resolution (see Precedence model):
   level  := parseLevel(c.Level);  if c.levelOverride  != nil { level  = *c.levelOverride }
   format := parseFormat(c.Format); if c.formatOverride != nil { format = *c.formatOverride }
6. // Resolve the SINGLE primary writer (stdout XOR file), code override wins:
   var w io.Writer
   switch {
   case c.outputOverride != nil:            // WithOutput
       w = c.outputOverride
   case c.File != "":                        // file replaces stdout
       f, err := openFile(c.File)            // mkdir -p dir; O_CREATE|O_APPEND|O_WRONLY
       if err != nil { return nil, err }     // already wrapped with ErrOpenFile
       w = f
   default:
       w = os.Stdout
   }
7. // primary destination + any parallel WithHandler destinations:
   handlers := append([]slog.Handler{newHandler(format, w, level, c.AddSource)}, c.extraHandlers...)
   var base slog.Handler = handlers[0]
   if len(handlers) > 1 { base = slog.NewMultiHandler(handlers...) }
8. // c.extractors holds only non-nil funcs (filtered on apply), so an all-nil list skips:
   if len(c.extractors) > 0 { base = newContextHandler(base, c.extractors...) }
9. return slog.New(base), nil
```

The internal `config` embeds `Config` (serializable strings) and adds the
non-serializable fields: `outputOverride io.Writer` (nil unless `WithOutput`),
`extractors []ContextExtractor`, `extraHandlers []slog.Handler`, the code overrides
`levelOverride *slog.Level` and `formatOverride *Format`, and `errs []error` — mirroring
how `httpserver`'s internal `config` embeds its `Config`.

`newHandler` maps `FormatText → slog.NewTextHandler`, `FormatJSON → slog.NewJSONHandler`,
each with `&slog.HandlerOptions{Level: level, AddSource: addSource}`. The core builds
exactly one **primary** handler; `slog.NewMultiHandler` appears only when one or more
`WithHandler` destinations are present. Per-handler `Enabled(level)` filtering is what
gives each parallel destination (e.g. Sentry) its independent level: `MultiHandler.Handle`
calls a sub-handler's `Handle` only when its `Enabled` returns true for the record level.

`openFile` (in `file.go`): `dir := filepath.Dir(path)`; if `dir != "" && dir != "."`,
`os.MkdirAll(dir, 0o755)`; then `os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND,
0o644)`. Both failures are wrapped as `fmt.Errorf("%w: %v", ErrOpenFile, originalErr)` —
single `%w` so `ErrOpenFile` is the `errors.Is` sentinel, with the underlying
path/permission error preserved as message context.

## Public API — `logger/sentry`

Shaped like every other forge package (own `Config`/`DefaultConfig`/`Validate`/`Option`/
`WithConfig`/`New`). It configures the underlying primary logger *and* Sentry from one
place, and returns the combined logger plus a `Flush`.

### Config

```go
// Config carries both the primary-logger settings (embedded logger.Config) and the
// Sentry-specific settings, so the whole thing env-loads in one shot.
type Config struct {
    logger.Config          // embedded: Level, Format, File, AddSource (env LEVEL/FORMAT/FILE/ADD_SOURCE)

    DSN         string `env:"DSN"`
    Environment string `env:"ENVIRONMENT"`  // default "production"
    // MinLevel is Sentry's OWN minimum level — the lowest level forwarded to Sentry,
    // independent of the primary destination's level. Case-insensitive, one of
    // "debug", "info", "warn"/"warning", "error".
    MinLevel    string `env:"MIN_LEVEL"`     // default "warn"
    // EnableLogs forwards non-error log entries to Sentry as Logs (in addition to
    // errors, which always create Issues). Default false (opt-in).
    EnableLogs  bool   `env:"ENABLE_LOGS"`
}

func DefaultConfig() Config // {Config: logger.DefaultConfig(), Environment:"production", MinLevel:"warn"}
func (c Config) Validate() error
```

**`Validate` semantics:** validates the embedded `logger.Config` (Level/Format) **and**
rejects an unknown `MinLevel`, both case-insensitive over `{debug, info, warn, warning,
error}` with **no silent fallback**. It wraps with **double-`%w`** so the result matches
*both* `sentry.ErrInvalidConfig` and (for a bad Level/Format) `logger.ErrInvalidConfig`:
`fmt.Errorf("%w: %w", ErrInvalidConfig, c.Config.Validate())`. An empty `DSN` is valid
(triggers the no-op path in `New`). `parseLevel` (internal to `sentry`, mirroring v1's
`parseMinLevel`) is called only after `Validate` passes.

### Options, New, Flush

```go
type Option func(*config)

// WithConfig sets the whole serializable data block (primary-logger + Sentry settings).
func WithConfig(cfg Config) Option

// WithContextExtractors registers ContextExtractor funcs for the primary logger AND the
// Sentry destination (they sit beneath one decorator). Nil entries are filtered.
func WithContextExtractors(ex ...logger.ContextExtractor) Option

// WithOutput overrides the primary destination's writer (tests). A nil writer is
// rejected (ErrInvalidConfig).
func WithOutput(w io.Writer) Option

// Flush flushes buffered Sentry events; call it before the program exits. The timeout
// is derived from ctx.Deadline() (fallback defaultFlushTimeout = 2s). Returns
// ErrSentryFlushTimeout if not all events were delivered in time. A no-op when Sentry
// is not active (empty DSN or init failure).
type Flush func(ctx context.Context) error

// New builds a logger that writes to the primary destination (stdout/file from the
// embedded logger.Config) and, when DSN is non-empty, ALSO to Sentry in parallel at
// MinLevel. Returns the logger, a Flush closer, and an error.
//
//   - cfg.DSN == "": returns a plain logger (primary only), a no-op Flush, nil error.
//   - sentry.Init fails: returns a usable plain logger plus an ErrSentryInit-wrapped
//     error (the application keeps logging to its primary destination).
//   - otherwise: returns the combined logger, a real Flush, nil.
func New(opts ...Option) (*slog.Logger, Flush, error)
```

`sentry.New` deliberately mirrors `logger.New`'s ergonomics: it returns the standard
`*slog.Logger` (so it drops straight into `httpserver.WithLogger` etc.), plus the one
extra thing Sentry needs that a file does not — a `Flush`.

### sentry.New algorithm

```
1. c := defaultConfig()                       // sentry defaults incl. embedded logger defaults
2. apply opts; accumulate c.errs (WithConfig replaces block; WithContextExtractors filters nils)
3. if len(c.errs) > 0: return nil, nil, errors.Join(c.errs...)
4. if err := c.Validate(); err != nil: return nil, nil, err
5. // forward primary-logger settings to logger.New:
   loggerOpts := []logger.Option{ logger.WithConfig(c.Config) }              // embedded logger.Config
   if len(c.extractors) > 0 { loggerOpts = append(loggerOpts, logger.WithContextExtractors(c.extractors...)) }
   if c.output != nil       { loggerOpts = append(loggerOpts, logger.WithOutput(c.output)) }
6. if c.DSN == "":                            // Sentry disabled — plain logger, no-op flush
       l, err := logger.New(loggerOpts...)
       return l, noopFlush, err
7. sh, err := buildHandler(c)                 // sentry.Init(...) + build sentryslog handler
   if err != nil:                             // graceful: keep logging, surface the error
       l, lerr := logger.New(loggerOpts...)
       if lerr != nil { return nil, nil, lerr }
       return l, noopFlush, fmt.Errorf("%w: %v", ErrSentryInit, err) // single %w: ErrSentryInit
                                                                     // stays the only errors.Is sentinel
8. l, err := logger.New(append(loggerOpts, logger.WithHandler(sh))...)
   if err != nil { return nil, nil, err }
   return l, flush, nil
```

**Test seam (no mutable global):** the public `New` delegates to an unexported
`newWith(buildHandler func(Config) (slog.Handler, error), opts ...Option)` that runs the
algorithm above; `New` passes the real builder, the internal test passes a fake one:
```go
func New(opts ...Option) (*slog.Logger, Flush, error) { return newWith(realSentryHandler, opts...) }
```
`realSentryHandler(cfg)` calls `sentry.Init(ClientOptions{DSN, Environment, EnableLogs})`
then builds the handler:
```go
min := parseLevel(cfg.MinLevel)
sh  := sentryslog.Option{
           EventLevel: []slog.Level{slog.LevelError},   // errors → Issues  (see SDK note)
           LogLevel:   levelsFrom(min),                 // min..error → Logs
       }.NewSentryHandler(context.Background())
```

> **SDK / API note (re-confirm at implementation time).** The `sentryslog.Option` shape
> above is the v1 reference. The `getsentry/sentry-go/slog` API has churned —
> `EventLevel` is deprecated in newer releases. Before coding `logger/sentry`, pin exact
> versions in `go.mod` and re-confirm the current handler-construction API against them;
> drop/replace `EventLevel` if required. Versions and the outcome are recorded in the
> **Dependencies** section.

`flush(ctx)`:
```go
const defaultFlushTimeout = 2 * time.Second

timeout := defaultFlushTimeout
if dl, ok := ctx.Deadline(); ok {
    timeout = time.Until(dl)
}
if timeout <= 0 {                      // deadline already passed (ctx may not be Done yet)
    if err := ctx.Err(); err != nil {
        return err
    }
    return ErrSentryFlushTimeout
}
if !sentry.Flush(timeout) {
    return ErrSentryFlushTimeout
}
return nil
```

A ctx with **no deadline** falls back to the 2s default (not an infinite/zero wait).
`noopFlush` returns nil and is returned whenever Sentry is inactive, so callers can
always `defer flush(ctx)` unconditionally.

## Errors

```go
// logger
var (
    ErrInvalidConfig = errors.New("logger: invalid config")
    ErrOpenFile      = errors.New("logger: open log file")
)

// logger/sentry
var (
    ErrInvalidConfig      = errors.New("sentry: invalid config")
    ErrSentryInit         = errors.New("sentry: initialization failed")
    ErrSentryFlushTimeout = errors.New("sentry: flush timed out")
)
```

All errors are single-line and matchable with `errors.Is`. No stacks or blobs are
embedded; failure context is conveyed as wrapped messages. `sentry.Config.Validate`
wraps with double-`%w` so a bad primary-logger Level/Format matches both
`sentry.ErrInvalidConfig` and `logger.ErrInvalidConfig`.

## Logging conventions

Records are emitted by callers via slog; this package adds attributes, never strings.
The package never logs from `New` on success. On Sentry init failure it does not log on
your behalf — it returns `ErrSentryInit` for the caller to handle.

## Usage

```go
// Local development: text to a file INSTEAD of stdout, with a request-id extractor.
reqID := func(ctx context.Context) (slog.Attr, bool) {
    if id, ok := ctx.Value(ctxkeys.RequestID).(string); ok && id != "" {
        return slog.String("request_id", id), true
    }
    return slog.Attr{}, false
}

log, err := logger.New(
    logger.WithFormat(logger.FormatText),
    logger.WithLevel(slog.LevelDebug),
    logger.WithFile("./tmp/dev.log"),       // primary output is this file (not stdout)
    logger.WithContextExtractors(reqID),
)
if err != nil { /* handle ErrInvalidConfig / ErrOpenFile */ }

// Production: JSON to stdout, plus Sentry at warn+ (parallel, separate level) — one call.
log, flush, err := sentry.New(
    sentry.WithConfig(sentry.Config{
        Config:   logger.Config{Level: "info", Format: "json"}, // primary destination
        DSN:      os.Getenv("SENTRY_DSN"),                       // empty in dev → plain logger
        MinLevel: "warn",
    }),
    sentry.WithContextExtractors(reqID),
)
if err != nil {
    log.Warn("sentry disabled", slog.Any("err", err)) // log is still usable
}
defer flush(ctx) // ctx carries the shutdown deadline; no-op if Sentry inactive

// request_id is injected into the primary destination AND Sentry automatically.
log.ErrorContext(ctx, "payment failed", slog.String("user_id", "u-456"))
```

## Edge cases

- **No options:** text, info, stdout — a complete logger.
- **Empty `Config.File`:** primary destination is stdout; no file opened.
- **`Config.File` set:** primary destination is the file; **stdout receives nothing**.
- **File path with missing dirs:** `mkdir -p` creates them; failure → `ErrOpenFile`.
- **Bad level/format string:** `Validate` fails → `ErrInvalidConfig`, no I/O.
- **`WithOutput(buf)` + `WithFile`:** `WithOutput` wins (code override) — the file is
  **not opened** and records go to `buf` only.
- **`WithHandler` + extractors:** the extra handler sits beneath the decorator, so it
  receives extracted attributes; it still filters at its own level.
- **No extractors:** `New` skips the decorator entirely; the logger's handler is the
  primary handler (or a `MultiHandler` if `WithHandler` was used).
- **`sentry.New` with empty DSN:** returns a plain logger + no-op flush + nil error;
  `defer flush(ctx)` is a no-op.
- **`sentry.New` init failure:** returns a usable plain logger + `ErrSentryInit`; the
  app decides whether to treat it as fatal.
- **`flush` past deadline:** `ErrSentryFlushTimeout`. `flush` with a **no-deadline** ctx
  uses the 2s default.
- **Groups + extraction:** extracted attrs stay at the record's top level, not nested in
  a `WithGroup`, on every destination.
- **Calling `sentry.New` twice:** initializes the process-global Sentry hub twice —
  **call it once per process.** The doc comment on `New` states this.

## Testing

`logger`:
- `config_test.go` — `DefaultConfig` values; `Validate` table (good/bad level, format);
  level/format parsing case-insensitivity (incl. `"warning"`); an env-tag presence test
  (mirrors the supervisor `reflect.TypeFor` test).
- `options_test.go` — each option mutates `config` as expected; nil/empty rejections
  (`WithFile("")`, `WithOutput(nil)`, `WithHandler(nil)`) accumulate `ErrInvalidConfig`;
  `WithConfig`-then-`WithFile` order; `WithLevel`/`WithFormat`/`WithOutput` override-wins
  regardless of order.
- `logger_test.go` — `New()` default writes JSON/text to a captured `WithOutput` buffer
  at the right level; `WithFile` creates the file under `t.TempDir()` (incl. a missing
  nested dir) and **all records go to the file**; `WithOutput` + `WithFile` together →
  file is **not** created (XOR precedence); `WithHandler(fake)` → both the primary buffer
  and the fake handler receive a record, and the fake filters at its own level;
  `ErrOpenFile` on an un-creatable path; `NewNope` discards.
- `decorator_test.go` — extractor injects an attr; returns false → skipped; attr lands
  at top level even after `WithGroup`; nil extractors filtered; **immutability**
  (`WithAttrs`/`WithGroup` return new handlers, receiver unchanged; safe under `-race`);
  with `WithHandler`, the extracted attr reaches **both** the primary and the extra
  handler.

`logger/sentry`:
- `sentry_test.go` (external) — `Validate` table for `MinLevel` (incl. `"warning"` alias,
  rejection of unknown, and a bad embedded `Level` matching both sentinels via
  `errors.Is`); `New` with empty DSN returns a working primary logger (captured via
  `WithOutput`) and a no-op flush; `Flush` timeout derivation from a deadlined ctx and
  the no-deadline → 2s-default path. No network.
- `sentry_internal_test.go` (internal) — call the unexported `newWith` with a fake
  `buildHandler` returning an in-test handler, a non-empty DSN, and extractors; assert
  (a) a record reaches both the primary writer (captured via `WithOutput`) and the fake
  (parallel fan-out), (b) the fake carries the extracted attrs (extraction beneath the
  decorator), and (c) the returned `Flush` is the real one. `sentry.Init` / network is
  never invoked.

All tests use only `testify`. `just check` (fmt + vet + golangci-lint + nilaway +
betteralign + test with `-race`) must pass; field order satisfies `betteralign`, slog
usage satisfies `sloglint`.

## Dependencies

`CLAUDE.md` forbids external dependencies "without a strong reason." This design keeps
that rule intact:

- **`logger` (core): zero third-party dependencies.** Every file imports only the
  stdlib (`log/slog`, `context`, `io`, `os`, `path/filepath`, `strings`, `errors`,
  `fmt`). Enforceable: a test (or a `depguard` rule) can assert the core package's import
  graph contains nothing under `github.com/getsentry`.
- **`logger/sentry`: the one justified, isolated dependency.** It imports
  `github.com/getsentry/sentry-go` and `github.com/getsentry/sentry-go/slog`. Strong
  reason: reimplementing Sentry's ingestion protocol is infeasible and out of scope.
- **Why a subpackage (not merged into `logger`, not a separate module).** Go links per
  **package**, not per module, and Go 1.17+ prunes the module graph. So although forge's
  own `go.mod` lists `sentry-go` regardless (a single module can't make a dependency
  conditional), an app — or sibling package — that imports only `logger` and **not**
  `logger/sentry` never compiles, links, or downloads the SDK source; it stays out of
  that binary. This matters because `logger` is **foundational**: `supervisor`,
  `httpserver`, and essentially every app import it. Merging Sentry into `logger` would
  force `sentry-go` into **every** forge binary; the subpackage makes it strictly opt-in.
  A separate *module* would remove it from forge's `go.mod` too, but a multi-module repo
  (separate tagging/versioning, workspace wiring) is heavier than the one-line `go.mod`
  entry is worth, and cuts against the flat composable-monolith direction. Decision:
  **subpackage**.
- **Version pinning (action item for implementation):** pin exact versions of
  `sentry-go` and `sentry-go/slog` in `go.mod`, and re-confirm the `sentryslog`-handler
  construction API (`Option`/`EventLevel`/`LogLevel` vs. the current shape) against those
  versions before writing `realSentryHandler` — see the SDK note above. Record the chosen
  versions here once selected.
- **Tests:** `testify` only (already permitted framework-wide).

## Future fit

- Additional parallel destinations (OTLP, syslog, Loki) follow the same shape: a sibling
  adapter package with its own `Config`/`New` that builds a `slog.Handler` and calls
  `logger.New(append(loggerOpts, logger.WithHandler(h))...)`. Core never changes.
- If such a destination needs flushing/closing, its `New` returns its own closer, exactly
  as `sentry.New` returns `Flush` — core stays a clean `(*slog.Logger, error)`.
- Built-in extractor helpers could ship in a sibling package if a request-context
  convention is standardized framework-wide.

## Deferred

- Log rotation / retention / compression (explicitly out of scope — platform's job).
- Simultaneous stdout **and** file output (deliberately excluded — the primary
  destination is stdout XOR file; `WithHandler` covers *parallel* sinks if ever needed).
- Convenience options on `sentry` (`WithDSN`, `WithMinLevel`, …) — `WithConfig` covers
  configuration today; add sugar only if call sites get noisy.
- Sampling, rate limiting, async/buffered handlers.
