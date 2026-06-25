# Design: `logger` — slog factory, context extraction, file output + Sentry adapter

- **Date:** 2026-06-25
- **Status:** Draft for review
- **Scope:** A new `logger` package — an `slog.Logger` factory with context-attribute
  extraction, an optional local-dev file destination, and a discard logger — plus a
  separate `logger/sentry` adapter package that wraps a finished logger to fan logs
  out to Sentry **in parallel, at its own level**, with the one unavoidable external
  dependency (the Sentry SDK) isolated to that subpackage.

## Overview

`logger` is a thin, composable layer over the standard library's `log/slog`. It does
four things and nothing more:

1. Builds a configured `*slog.Logger` from functional options (`New`).
2. Injects request-scoped attributes from `context.Context` on every log call via an
   exported handler decorator (`ContextHandler` + `ContextExtractor`).
3. Optionally tees every record into a local file as a second destination (no
   rotation — a plain append file for local development), creating the file and any
   missing parent directories.
4. Provides a no-op discard logger (`NewNope`) for defaults and tests.

A separate `logger/sentry` package adds Sentry as an additional destination. The
dependency direction is **inverted** relative to a plugin model: core `logger` knows
nothing about Sentry; `sentry.Wrap(base, cfg)` takes the finished `*slog.Logger`,
fans it out to both its existing destinations and a Sentry handler via
`slog.NewMultiHandler`, and returns the combined logger plus a `Flush` closer. This
keeps the core package 100% standard library and makes Sentry a strictly additive,
opt-in import.

This spec follows the framework conventions established by `supervisor` and
`httpserver`: serializable settings live in an env-loadable `Config` (data only) with
`DefaultConfig()`/`Validate()`/`WithConfig(...)`; non-serializable settings (writers,
extractors) are functional options; option values are validated zero-trust and errors
accumulate; sentinel errors live in `errors.go`; diagnostics are slog attributes and
errors are single-line.

## Goals

- **`New(opts...)` returns a ready `*slog.Logger`.** With no options it is a complete,
  reasonable logger: text format, info level, stdout.
- **Context extraction** of request-scoped attributes (request id, user id, tenant
  id, …) injected fresh on every log call, landing at the record's **top level** even
  when the caller has opened groups.
- **File as a second destination** for local development: write all records to a
  caller-provided path, creating the file and parent directories if absent. No
  rotation, retention, or compression.
- **Sentry runs parallel to stdout at its own, separate level** — e.g. stdout at
  `info`, Sentry at `warn` — with zero external dependency in the core package.
- **Graceful Sentry fallback:** an empty DSN returns the base logger immediately
  (no-op flush, nil error); a Sentry init failure still returns a usable logger.
- **Config = data, Option = code.** Serializable settings in one env-loadable
  `Config`; writers and extractor funcs as options.
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
- Additional sinks beyond stdout/file/Sentry (Kafka, syslog, OTLP, …). New
  destinations are future adapter packages following the same `Wrap` shape.
- Importing or wrapping any config loader. `Config` is plain data with inert `env`
  struct tags.
- Per-destination level/format knobs in core. stdout and file share the global level
  and format; only Sentry has an independent level (it is a separate package).

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
    logger.go        # New, NewNope, internal config, handler assembly
    decorator.go     # ContextExtractor, ContextHandler (Inner/WithInner/Handle/…)
    file.go          # openFile: mkdir -p + create + append
    config.go        # Config (env-tagged), DefaultConfig, Validate, level/format parse
    options.go       # Option, WithConfig/WithLevel/WithFormat/WithFile/WithOutput/WithContextExtractors
    errors.go        # ErrInvalidConfig, ErrOpenFile
    doc.go           # package documentation
    logger_test.go
    decorator_test.go
    config_test.go
    options_test.go
    sentry/
      sentry.go               # Config, DefaultConfig, Validate, Wrap, Flush
      errors.go               # ErrInvalidConfig, ErrSentryInit, ErrSentryFlushTimeout
      doc.go
      sentry_test.go          # external: empty-DSN passthrough, Validate
      sentry_internal_test.go # internal: composition/extraction via injected fake handler
  ```

## Public API — core `logger`

### Config (serializable, env-loadable)

```go
// Config holds the serializable settings for New. The env struct tags are inert
// strings — this package imports no config loader.
type Config struct {
    // Level is the minimum level for stdout and file destinations:
    // "debug", "info", "warn"/"warning", "error" (case-insensitive).
    Level string `env:"LEVEL"`
    // Format selects the handler: "text" or "json" (case-insensitive).
    Format string `env:"FORMAT"`
    // File, when non-empty, adds a second destination writing to this path. Parent
    // directories and the file are created if absent. Empty means stdout only.
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
| `File` | any string, including `""` (= stdout only) | never rejected (created by `New`) |
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
// WithConfig or WithFile), records are also written to that file; the file and any
// missing parent directories are created. Returns ErrInvalidConfig for bad option/
// Config values and ErrOpenFile if the file cannot be opened.
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

// WithLevel sets an explicit code-level minimum for stdout/file. It is an override:
// it ALWAYS takes precedence over Config.Level regardless of option order. Accepts any
// slog.Level (including custom levels), so it never round-trips through string parsing.
func WithLevel(level slog.Level) Option

// WithFormat sets an explicit code format override. Like WithLevel, it ALWAYS takes
// precedence over Config.Format regardless of option order. Default FormatText.
func WithFormat(f Format) Option

// WithFile adds a file destination at path (convenience that writes Config.File). An
// empty path is rejected (ErrInvalidConfig). Because it writes the data block, a later
// WithConfig overrides it. The file and parent dirs are created at New.
func WithFile(path string) Option

// WithOutput overrides the default stdout destination's writer (tests, custom sinks).
// A nil writer is rejected (ErrInvalidConfig).
func WithOutput(w io.Writer) Option

// WithContextExtractors registers ContextExtractor funcs applied on every log call.
// Nil extractors are filtered. Order is preserved.
func WithContextExtractors(ex ...ContextExtractor) Option
```

**Precedence model (two tiers, stated explicitly to avoid ambiguity):**

- `Config` string fields (`Level`, `Format`, `File`) are the serializable tier. Among
  options that write them — `WithConfig` (whole block) and `WithFile` (one field) —
  **option order decides** (last writer wins); place `WithConfig` first.
- `WithLevel`/`WithFormat` set distinct **code-override fields** (`*slog.Level`,
  `*Format`) on the internal config. They are resolved last and **always win** over
  the parsed `Config` value, irrespective of where they sit in the option list. This
  two-representation split exists because a level has both a serializable string form
  (`Config.Level`) and a precise code form (`slog.Level`); the code form is treated as
  explicit intent.

`Format` is a tiny string enum so `WithFormat` is type-safe while `Config.Format`
stays an env-friendly string.

### Context extraction (exported decorator)

```go
// ContextExtractor extracts a slog attribute from context. Return ok=false to skip.
type ContextExtractor func(ctx context.Context) (slog.Attr, bool)

// ContextHandler wraps a slog.Handler and injects context-extracted attributes at the
// record's top level on every Handle call — ahead of any group opened with WithGroup.
//
// All methods are pure: WithAttrs/WithGroup/WithInner return a NEW *ContextHandler and
// never mutate the receiver (slices are copied), so a ContextHandler is safe for
// concurrent use by multiple goroutines like any slog.Handler.
type ContextHandler struct {
    // root is the underlying handler BEFORE any recorded WithAttrs/WithGroup ops.
    // Extracted attributes are attached here so they land at the top level, ahead of
    // any group. This is exactly what Inner() returns.
    root       slog.Handler
    // next is root with all recorded ops applied; it handles records on the fast path
    // (no group active) without rebuilding the chain per call.
    next       slog.Handler
    // ops records WithAttrs/WithGroup calls in order so the chain can be replayed onto
    // a fresh root (used by both Handle's slow path and WithInner).
    ops        []handlerOp
    extractors []ContextExtractor
}

// NewContextHandler wraps next with the given extractors. Nil extractors are filtered.
// With zero extractors it is a pass-through (callers may skip it entirely). Initializes
// root == next and ops == nil.
func NewContextHandler(next slog.Handler, ex ...ContextExtractor) *ContextHandler

// Inner returns the ORIGINAL unwrapped handler (h.root) — the destination fan-out
// BEFORE any recorded WithAttrs/WithGroup ops. Adapters compose a new destination onto
// this clean base; returning root (not next) is required so that WithInner can replay
// the ops without double-applying them.
func (h *ContextHandler) Inner() slog.Handler

// WithInner returns a copy of the decorator whose root is `next`, rebuilding the cached
// `next` by replaying h.ops onto `next` (same logic as Handle's slow path), and copying
// the ops and extractors slices. This preserves any static attrs/groups added via
// log.With(...) BEFORE the swap, so an adapter that does
// d.WithInner(MultiHandler(d.Inner(), extra)) keeps extraction AND prior With-attrs for
// every destination, old and new.
func (h *ContextHandler) WithInner(next slog.Handler) *ContextHandler

// Standard slog.Handler methods: Enabled, Handle, WithAttrs, WithGroup.
```

`Inner`/`WithInner` are the intentional **adapter seam** that makes the inverse Sentry
wrap (and any future destination adapter) work without re-passing extractors — and the
reason `ContextHandler` is exported. There is no import cycle: `logger/sentry` imports
`logger`, never the reverse.

The decorator's `Handle`/group internals are ported from the v1 implementation:
extracted attributes are attached to the root handler so they land at the top level
regardless of active groups, with a fast path when no group is open (add directly to
the record) and a rebuild path when a group is active (re-apply recorded ops on top of
the root + extracted attrs).

> **Net-new vs v1 — implementer beware:** `Inner`/`WithInner` do not exist in the v1
> decorator. The correctness hinge is that `Inner()` returns `root` and `WithInner`
> replays `ops` onto the new inner. If `WithInner` skipped the replay (or `Inner`
> returned `next`), static attributes added via `log.With(...)` before `sentry.Wrap`
> would silently vanish or be double-applied. This must be covered by a test (see
> Testing → `decorator_test.go`).

## Option validation (zero-trust)

Options accumulate invalid values into `config.errs`; `New` joins them and returns
`ErrInvalidConfig` before performing any I/O (no file is created on a bad config).

| Input | Rejected when | Error |
|---|---|---|
| `WithFile(path)` | `path == ""` | `ErrInvalidConfig` |
| `WithOutput(w)` | `w == nil` | `ErrInvalidConfig` |
| `Config.Level` | not a known level name | `ErrInvalidConfig` (from `Validate`) |
| `Config.Format` | not `"text"`/`"json"` | `ErrInvalidConfig` (from `Validate`) |
| `WithContextExtractors` | nil entries | filtered (not an error) |

`WithLevel` takes a `slog.Level` (any int is valid). `Validate` is called by `New`
defensively even if the caller already called it after env loading.

## New algorithm

```
1. c := defaultConfig()                     // {Config: DefaultConfig(), output: os.Stdout,
                                             //  extractors: nil, level/format override: nil}
2. apply opts in order; each invalid value appends fmt.Errorf("%w: ...", ErrInvalidConfig)
   to c.errs. WithConfig replaces the embedded Config block.
3. if len(c.errs) > 0: return nil, errors.Join(c.errs...)   // each err already wraps
                                                            // ErrInvalidConfig; no I/O
4. if err := c.Validate(); err != nil: return nil, err      // wraps ErrInvalidConfig
5. // Two-tier resolution (see Precedence model):
   level  := parseLevel(c.Level);  if c.levelOverride  != nil { level  = *c.levelOverride }
   format := parseFormat(c.Format); if c.formatOverride != nil { format = *c.formatOverride }
6. handlers := [ newHandler(format, c.output, level, c.AddSource) ]   // stdout/output
7. if c.File != "":
       f, err := openFile(c.File)           // mkdir -p dir; O_CREATE|O_APPEND|O_WRONLY
       if err != nil: return nil, err        // already wrapped with ErrOpenFile
       handlers = append(handlers, newHandler(format, f, level, c.AddSource))
8. var base slog.Handler = handlers[0]
   if len(handlers) > 1: base = slog.NewMultiHandler(handlers...)
9. // c.extractors holds only non-nil funcs (WithContextExtractors filtered them on apply),
   // so an all-nil extractor list correctly skips the decorator:
   if len(c.extractors) > 0: base = NewContextHandler(base, c.extractors...)
10. return slog.New(base), nil
```

The internal `config` embeds `Config` (serializable strings) and adds the
non-serializable fields: `output io.Writer`, `extractors []ContextExtractor`, the code
overrides `levelOverride *slog.Level` and `formatOverride *Format`, and `errs []error`
— mirroring how `httpserver`'s internal `config` embeds its `Config`.

`newHandler` maps `FormatText → slog.NewTextHandler`, `FormatJSON → slog.NewJSONHandler`,
each with `&slog.HandlerOptions{Level: level, AddSource: addSource}`. Per-destination
level filtering is handled by slog: `slog.NewMultiHandler` calls each sub-handler's
`Handle` only when its `Enabled(level)` is true. stdout and file share the global
level here; an independent level is only meaningful for Sentry, which is a separate
package.

`openFile` (in `file.go`): `dir := filepath.Dir(path)`; if `dir != "" && dir != "."`,
`os.MkdirAll(dir, 0o755)`; then `os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND,
0o644)`. Both errors are wrapped with `ErrOpenFile`.

## Public API — `logger/sentry` adapter

### Config

```go
type Config struct {
    DSN         string `env:"DSN"`
    Environment string `env:"ENVIRONMENT"` // default "production"
    // MinLevel is Sentry's OWN minimum level — the lowest level forwarded to Sentry,
    // independent of the base logger's stdout/file level. Case-insensitive, one of
    // "debug", "info", "warn"/"warning", "error".
    MinLevel    string `env:"MIN_LEVEL"`    // default "warn"
    // EnableLogs forwards non-error log entries to Sentry as Logs (in addition to
    // errors, which always create Issues). Default false (opt-in).
    EnableLogs  bool   `env:"ENABLE_LOGS"`
}

func DefaultConfig() Config // {DSN:"", Environment:"production", MinLevel:"warn", EnableLogs:false}
func (c Config) Validate() error
```

**`Validate` semantics:** rejects an unknown `MinLevel` with `ErrInvalidConfig` (same
case-insensitive `{debug, info, warn, warning, error}` set as `logger.Config.Level`,
**no silent fallback**). `DSN`/`Environment`/`EnableLogs` are not constrained — an
empty `DSN` is valid and triggers the no-op path in `Wrap`. The internal `parseLevel`
(in the `sentry` package, mirroring v1's `parseMinLevel`) is called by `Wrap` only
after `Validate` passes, so it assumes a validated value.

### Wrap, Flush

```go
// Flush flushes buffered Sentry events; call it before the program exits. The timeout
// is derived from ctx.Deadline() (fallback defaultFlushTimeout = 2s). Returns
// ErrSentryFlushTimeout if not all events were delivered in time. A no-op when Sentry
// is not active.
type Flush func(ctx context.Context) error

// Wrap fans the base logger out to Sentry in parallel at cfg.MinLevel, returning the
// combined logger and a Flush closer.
//
//   - cfg.DSN == "": returns base unchanged, a no-op Flush, and nil error
//     (graceful local-dev/CI path — Sentry is never initialized).
//   - sentry.Init fails: returns base (still usable) plus an ErrSentryInit-wrapped
//     error; the application keeps logging to its existing destinations.
//   - otherwise: returns slog.New(combined), flush, nil.
func Wrap(base *slog.Logger, cfg Config) (*slog.Logger, Flush, error)
```

### Wrap algorithm

```
1. if err := cfg.Validate(); err != nil: return base, noopFlush, err
2. if cfg.DSN == "": return base, noopFlush, nil          // immediate base return
3. if err := sentry.Init(ClientOptions{DSN, Environment, EnableLogs}); err != nil:
       return base, noopFlush, fmt.Errorf("%w: <err>", ErrSentryInit)
4. min := parseLevel(cfg.MinLevel)
   sh := sentryslog.Option{
            EventLevel: []slog.Level{slog.LevelError},   // errors → Issues  (see API note)
            LogLevel:   levelsFrom(min),                 // min..error → Logs
        }.NewSentryHandler(context.Background())
5. combined := composeBeneathExtraction(base.Handler(), sh)
6. return slog.New(combined), flush, nil
```

> **SDK / API note (re-confirm at implementation time).** The `sentryslog.Option`
> shape above is the v1 reference. The `getsentry/sentry-go/slog` API has churned —
> `EventLevel` is deprecated in newer releases. Before coding `logger/sentry`, pin an
> exact version in `go.mod` and re-confirm the current handler-construction API against
> that version's docs; drop/replace `EventLevel` if required. Targeted versions and the
> outcome of this check are recorded in the **Dependencies** section below.

`composeBeneathExtraction` is the inverse-wrap core and the reason core exports
`ContextHandler.Inner/WithInner`:

```go
func composeBeneathExtraction(bh, sh slog.Handler) slog.Handler {
    if d, ok := bh.(*logger.ContextHandler); ok {
        // Slot Sentry beneath the SAME extraction decorator so request-scoped attrs
        // reach Sentry too — extractors configured once in logger.New.
        return d.WithInner(slog.NewMultiHandler(d.Inner(), sh))
    }
    // base had no extractors: plain parallel fan-out.
    return slog.NewMultiHandler(bh, sh)
}
```

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
embedded; failure context is conveyed as wrapped messages.

## Logging conventions

Records are emitted by callers via slog; this package adds attributes, never strings.
Diagnostics produced by the package itself (none expected on the hot path) would use
slog attributes. The package never logs from `New` on success.

## Usage

```go
// Local development: text to stdout + a file, with a request-id extractor.
reqID := func(ctx context.Context) (slog.Attr, bool) {
    if id, ok := ctx.Value(ctxkeys.RequestID).(string); ok && id != "" {
        return slog.String("request_id", id), true
    }
    return slog.Attr{}, false
}

log, err := logger.New(
    logger.WithFormat(logger.FormatText),
    logger.WithLevel(slog.LevelDebug),
    logger.WithFile("./tmp/dev.log"),       // created with parent dirs
    logger.WithContextExtractors(reqID),
)
if err != nil { /* handle ErrInvalidConfig / ErrOpenFile */ }

// Production: JSON to stdout, plus Sentry at warn+ (parallel, separate level).
base, err := logger.New(
    logger.WithConfig(logger.DefaultConfig()), // info, text — or env-parsed
    logger.WithFormat(logger.FormatJSON),
    logger.WithContextExtractors(reqID),
)
if err != nil { /* ... */ }

log, flush, err := sentry.Wrap(base, sentry.Config{
    DSN:      os.Getenv("SENTRY_DSN"),  // empty in dev → base returned as-is
    MinLevel: "warn",
})
if err != nil {
    base.Warn("sentry disabled", slog.Any("err", err)) // base still usable
}
defer flush(ctx) // ctx carries the shutdown deadline; no-op if Sentry inactive

// request_id is injected into stdout AND Sentry automatically.
log.ErrorContext(ctx, "payment failed", slog.String("user_id", "u-456"))
```

## Edge cases

- **No options:** text, info, stdout — a complete logger.
- **Empty `Config.File`:** stdout only; no file opened.
- **File path with missing dirs:** `mkdir -p` creates them; failure → `ErrOpenFile`.
- **Bad level/format string:** `Validate` fails → `ErrInvalidConfig`, no I/O.
- **`WithOutput(buf)` + `WithFile`:** both destinations receive every record.
- **No extractors:** `New` skips the decorator entirely; `base.Handler()` is the
  (multi)handler, and `sentry.Wrap` takes the plain fan-out path.
- **`sentry.Wrap` with empty DSN:** returns `base` immediately; `defer flush(ctx)` is
  a no-op.
- **`sentry.Wrap` init failure:** returns usable `base` + `ErrSentryInit`; the app
  decides whether to treat it as fatal.
- **`flush` past deadline:** `ErrSentryFlushTimeout`. `flush` with a **no-deadline**
  ctx uses the 2s default.
- **Groups + extraction:** extracted attrs stay at the record's top level, not nested
  in a `WithGroup`.
- **`sentry.Wrap(NewNope(), cfg)`:** `NewNope`'s handler is a discard handler, not a
  `*ContextHandler`, so `Wrap` fans out as `MultiHandler(discard, sentryHandler)`. The
  discard branch drops records; the **Sentry branch still delivers** (each MultiHandler
  sub-handler gets its own clone). Net effect: **Sentry-only** logging with no stdout —
  a legitimate, if niche, pattern. (Note: there are no extractors to carry, since
  `NewNope` has none.)
- **Double-wrap `sentry.Wrap(sentry.Wrap(base))`:** **not supported — call `Wrap`
  once.** It nests MultiHandlers (functionally still routes, but redundantly) *and*
  calls `sentry.Init` a second time, reconfiguring the process-global Sentry hub. The
  doc comment on `Wrap` must state it is intended to be called exactly once per process.

## Testing

`logger`:
- `config_test.go` — `DefaultConfig` values; `Validate` table (good/bad level, format);
  level/format parsing case-insensitivity; an env-tag presence test (mirrors the
  supervisor `reflect.TypeFor` test).
- `options_test.go` — each option mutates `config` as expected; nil/empty rejections
  accumulate `ErrInvalidConfig`; option order/precedence (`WithConfig` then `WithLevel`).
- `logger_test.go` — `New()` default writes JSON/text to a captured `WithOutput` buffer
  at the right level; `WithFile` creates the file under `t.TempDir()` (incl. a missing
  nested dir) and appends; both buffer and file receive a record; `ErrOpenFile` on an
  un-creatable path; `NewNope` discards (buffer stays empty).
- `decorator_test.go` — extractor injects an attr; returns false → skipped; attr lands
  at top level even after `WithGroup`; nil extractors filtered; **immutability** (
  `WithAttrs`/`WithGroup`/`WithInner` return new handlers, receiver unchanged; safe
  under `-race`). The **blocker hinge**: build `d := NewContextHandler(h, ex)`, call
  `d2 := d.WithAttrs([]slog.Attr{slog.String("k","v")})`, then
  `d3 := d2.WithInner(slog.NewMultiHandler(d2.Inner(), fake))`; assert a record routed
  through `d3` still carries `k=v` on **both** the original `h` and `fake` branches (ops
  replayed onto the swapped inner; `Inner()` returned the clean root so `k=v` is not
  doubled).

`logger/sentry`:
- `sentry_test.go` (external) — `Wrap` with empty DSN returns the **same** `*slog.Logger`
  pointer and a no-op flush; `Validate` table for `MinLevel` (incl. `"warning"` alias and
  rejection of unknown); `Flush` timeout derivation from a deadlined ctx, and the
  no-deadline → 2s-default path (no network).
- `sentry_internal_test.go` (internal) — `composeBeneathExtraction` with a fake
  in-package handler asserts (a) a decorated base places the fake beneath the same
  extractor so extracted attrs reach it (and With-attrs added before the wrap survive),
  and (b) an undecorated base (e.g. `NewNope`'s handler) produces a plain `MultiHandler`
  whose Sentry branch still receives records. Network/`sentry.Init` is never invoked.

All tests use only `testify`. `just check` (fmt + vet + golangci-lint + nilaway +
betteralign + test with `-race`) must pass; field order satisfies `betteralign`,
slog usage satisfies `sloglint`.

## Dependencies

`CLAUDE.md` forbids external dependencies "without a strong reason." This design keeps
that rule intact:

- **`logger` (core): zero third-party dependencies.** Every file imports only the
  stdlib (`log/slog`, `context`, `io`, `os`, `path/filepath`, `strings`, `errors`,
  `fmt`). This is enforceable: a test (or a `depguard` rule) can assert the core
  package's import graph contains nothing under `github.com/getsentry`.
- **`logger/sentry`: the one justified, isolated dependency.** It imports
  `github.com/getsentry/sentry-go` and `github.com/getsentry/sentry-go/slog`. Strong
  reason: reimplementing Sentry's ingestion protocol is infeasible and out of scope.
  Because it is a **separate package**, only apps that import `logger/sentry` pull the
  SDK; the core logger and its consumers (`httpserver`, `supervisor`) never do.
- **Version pinning (action item for implementation):** pin exact versions of
  `sentry-go` and `sentry-go/slog` in `go.mod`, and re-confirm the
  `sentryslog`-handler construction API (`Option`/`EventLevel`/`LogLevel` vs. the
  current shape) against those versions before writing `Wrap` — see the SDK note in the
  Wrap algorithm. Record the chosen versions here once selected.
- **Tests:** `testify` only (already permitted framework-wide).

## Future fit

- Additional destinations (OTLP, syslog, Loki) follow the same inverse `Wrap(base,
  cfg)` shape in their own adapter packages, each composing beneath the extraction
  decorator via `ContextHandler.Inner/WithInner`.
- If a destination ever needs buffered I/O (and thus a real `Close`), it returns its
  own closer from its `Wrap`, exactly as `sentry.Wrap` returns `Flush` — core stays a
  clean `(*slog.Logger, error)`.
- Built-in extractor helpers could ship in a sibling package if a request-context
  convention is standardized framework-wide.

## Deferred

- Log rotation / retention / compression (explicitly out of scope — platform's job).
- Per-destination level/format for stdout vs file (YAGNI; single global pair today).
- A `WithFileLevel` to capture more in the file than stdout (add only if needed).
- Sampling, rate limiting, async/buffered handlers.
