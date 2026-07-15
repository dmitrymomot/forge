# ops/logger async mode + sentry handler rework — design

Date: 2026-07-15. Status: approved design, pre-plan.

## Context

`ops/logger` builds `*slog.Logger` values that are fully synchronous: every log call runs formatting and the write syscall on the caller's goroutine, and the stdlib handlers serialize all goroutines on one mutex per handler. A slow sink (stalled pipe, slow docker log driver, network-backed extra handler) blocks the whole app. This design adds an opt-in async mode, reworks `logger/sentry` from a facade into a handler provider so it (and any future logger feature) composes with async for free, and adds per-destination level gating for arbitrary extra handlers.

Decisions locked during brainstorming: general-purpose opt-in async (not niche); drop-new-records on full buffer, never block; explicit closer wired into app teardown (crash loss accepted); code-only enablement via functional options, no env flag.

## Goals

- Log calls in async mode never block on the sink and never return errors, at any load.
- One async seam covers the primary destination and all extra handlers.
- `New` (sync) stays the default and is completely unchanged in behavior.
- `logger/sentry` stops mirroring the logger option surface; it exposes a `slog.Handler`.
- Arbitrary extra handlers can be attached with their own minimum level.

## Non-goals

- No env-loadable async config (`LOG_ASYNC` etc.) — async changes the call-site contract (a closer exists), so it is a code decision.
- No durability: crash/`os.Exit` loses buffered records; records logged after Close are silently dropped.
- No per-handler queues or worker pools — one bounded queue, one worker (design.md: bounded concurrency).
- No zap/zerolog bridges (design.md: forge is slog-only).

## Public API

### ops/logger

```go
// NewAsync is New with a buffered, single-worker async core beneath context extraction.
// The returned CloseFunc drains the buffer and must be called on shutdown.
func NewAsync(opts ...Option) (*slog.Logger, CloseFunc, error)

// CloseFunc drains and stops the async worker; ctx bounds the drain wait.
type CloseFunc func(ctx context.Context) error

// WithAsyncBufferSize sets the record buffer capacity (default 8192). n < 1 is rejected.
// Only valid with NewAsync; New returns ErrInvalidConfig if it is set.
func WithAsyncBufferSize(n int) Option

// WithLeveledHandler adds an extra parallel destination that only receives records at
// min and above. A nil handler is rejected. Valid for both New and NewAsync.
func WithLeveledHandler(min slog.Level, h slog.Handler) Option
```

`New` keeps its exact signature and behavior. `NewAsync` accepts every existing option (`WithConfig`, `WithLevel`, `WithFormat`, `WithFile`, `WithOutput`, `WithHandler`, `WithContextExtractors`).

### ops/logger/sentry (breaking rework)

```go
// NewHandler builds the Sentry slog.Handler. It ALWAYS returns a usable handler and a
// non-nil Flush: empty DSN or init failure yield a disabled handler (Enabled reports
// false) so the result is safe to pass to logger.WithHandler unconditionally. An init
// failure additionally returns an ErrSentryInit-wrapped error to surface (non-fatal).
func NewHandler(opts ...Option) (slog.Handler, Flush, error)
```

- `sentry.New` (the facade returning a full `*slog.Logger`) is deleted. Pre-v1, breaking is accepted.
- `Config` drops the embedded `logger.Config`; it keeps `DSN`, `Environment`, `MinLevel`, `EnableLogs`, and gains its own `AddSource` (`SENTRY_ADD_SOURCE`) — previously inherited from the embedded logger config and read by the handler builder. Env blocks split naturally into `LOG_*` (logger) and `SENTRY_*` (sentry).
- `WithContextExtractors` and `WithOutput` are deleted from the sentry package — they existed only to forward to `logger.New`. `WithConfig(cfg Config)` remains.
- `Flush` keeps its type `func(ctx context.Context) error`; it ships buffered Sentry events.
- The `newWith` test seam (fake handler builder) is preserved.

## Architecture

Handler chain in async mode: `contextHandler` → `asyncHandler` → primary handler, or `slog.NewMultiHandler(primary, extras...)` when extra handlers exist. The current `New` body is refactored into a shared `buildBase(c config) (slog.Handler, error)`; `New` wraps it directly, `NewAsync` inserts the async layer beneath context extraction (extraction must run on the caller's goroutine while the request ctx is live).

Caller's goroutine per log call: `Enabled` check (delegates to base, so records below every destination's level never enqueue), context extraction, `rec.Clone()` (mandatory before crossing goroutines — the record shares an attr backing array), `context.WithoutCancel(ctx)`, non-blocking channel send of `{ctx, rec, target}` where `target` is the queue item's destination handler.

Worker goroutine (exactly one): dequeues items, reports accumulated drops (see below), calls `target.Handle(ctx, rec)`. Downstream Handle errors are ignored — identical to sync-mode `slog.Logger` semantics.

`asyncHandler.WithAttrs`/`WithGroup` return a new `asyncHandler` sharing the same core (channel, worker, drop counter, closed flag, via pointer) but wrapping `base.WithAttrs(...)`/`WithGroup(...)`; the wrapped base rides along in each queue item. This keeps `contextHandler`'s op-replay logic working unchanged above the async layer.

`context.WithoutCancel` preserves ctx values (tenant, sentry hub, trace refs) for downstream handlers while making sink writes uncancellable by the finished request — intended behavior for logging.

### leveledHandler

~15-line wrapper: `Enabled(ctx, l) = l >= min && h.Enabled(ctx, l)`; `Handle`, `WithAttrs`, `WithGroup` delegate (rewrapping to keep the gate). Used by `WithLeveledHandler`; internal type, not exported.

## Backpressure and drop reporting

Buffer full → the new record is dropped and an `atomic.Int64` counter increments; `Handle` returns nil. Logging never blocks and never surfaces an error.

When the worker next dequeues an item (and once more before exiting at close), it swaps the counter to zero and, if it was positive, first emits a synthetic record — level `Warn`, message `"logger: dropped log records"`, attr `dropped=N` — to every destination directly, bypassing each destination's level gate. A dropped-records warning is a system-health signal, so it stays visible even when every destination (e.g. an error-level sink) sits above `Warn`. (Refined during implementation: the first cut routed the report through the composed base handler, where `slog.MultiHandler.Handle` re-gates each child by level and would suppress the warning whenever all destinations sat above `Warn`.)

## Shutdown

`CloseFunc` is idempotent via `sync.Once`; every call waits for the same completion. Sequence: set atomic closed flag (subsequent `Handle` calls silently drop — no count, no panic, no block; logging after Close is a caller bug that must stay harmless), then `close()` a dedicated stop channel. The worker selects between the data channel and the stop channel; once stop is closed it drains every remaining item non-blockingly, emits any final drop tally, signals done, exits. `Close` waits on done or `ctx.Done()`, returning nil or `ctx.Err()`. The data channel is never `close()`d — no send-on-closed-channel race and no mutex on the hot path. (A sentinel-item design was rejected: with a full buffer and a wedged sink, the blocking sentinel send would hang `Close` beyond its ctx.) Records enqueued by racing `Handle` calls after the drain's final pass are abandoned — covered by the post-Close silent-drop contract.

Loss contract: buffer-full → dropped and counted; after Close → silently dropped; crash/`os.Exit` → buffer lost.

Teardown ordering with sentry: `closeLog(ctx)` first (drains buffered records into the sentry handler), then `flushSentry(ctx)` (ships events). Documented in both packages.

## Error handling

- `NewAsync` construction errors: same paths as `New` (`ErrInvalidConfig` joined option errors, `ErrOpenFile`), plus `ErrInvalidConfig` for `WithAsyncBufferSize(n < 1)`. On error the returned logger and closer are nil.
- `New` returns `ErrInvalidConfig` if `WithAsyncBufferSize` was supplied (async-only option, fail loud).
- `sentry.NewHandler` init failure: disabled handler + no-op Flush + `ErrSentryInit`-wrapped error; the app keeps logging.
- Async `Handle`: always nil. Worker ignores downstream errors.
- `CloseFunc`: nil on full drain, `ctx.Err()` on timeout (worker keeps draining in the background; it is not killed).

## Usage (composition root)

```go
sentryHandler, flushSentry, err := sentry.NewHandler(sentry.WithConfig(cfg.Sentry))
if err != nil { /* non-fatal: handler is disabled, app still logs */ }

log, closeLog, err := logger.NewAsync(
    logger.WithConfig(cfg.Log),                                 // stdout, info+
    logger.WithContextExtractors(requestID, tenantID),
    logger.WithHandler(sentryHandler),                          // warn+ via sentry MinLevel
    logger.WithLeveledHandler(slog.LevelError, errFileHandler), // errors also to a file
)
// teardown (after servers stop): closeLog(ctx), then flushSentry(ctx)
```

Per-destination levels: the primary uses `Config.Level`; sentry gates itself via `Config.MinLevel`; arbitrary handlers gate via `WithLeveledHandler`. `slog.MultiHandler` dispatches per-handler on each handler's `Enabled`.

## Testing

Black-box in `package logger_test` / `sentry_test` (repo rule); `Recorder` as downstream sink; a gate-able blocking-writer/handler fake to force buffer-full.

- Async: ordering preserved under sequential logging; drop-on-full with correct `dropped=N` warn record; Close drains fully (all records observed); Close honors ctx deadline (`ctx.Err()`); Close idempotent and concurrent-safe; Handle after Close is a safe no-op; `WithAttrs`/`WithGroup`/groups produce identical output to sync mode; extractor values captured at log-call time, not handle time; `-race` concurrent-producer stress.
- `WithLeveledHandler`: gating below/at/above min; delegation of WithAttrs/WithGroup keeps the gate; works under both New and NewAsync.
- Option validation: `WithAsyncBufferSize` rejected by New; `n < 1` rejected; nil handler in `WithLeveledHandler` rejected.
- Sentry rework: disabled handler on empty DSN (Enabled false at every level); disabled handler + `ErrSentryInit` on init failure; Flush non-nil in every path; existing capture/level tests adapted to the handler-provider shape via the `newWith` seam.

## Benchmarks

Required (repo rule), in `perf_bench_test.go`: async vs sync hot path (records/op, allocs/op — expect ~1 extra alloc for `Record.Clone`); disabled-level call in async mode (must not enqueue or clone); drop path under a blocked sink. Post-benchmark optimization pass; before/after numbers in the PR.

## Docs

- `logger/doc.go`: async paragraph — sync default, drop-on-full semantics, closer contract, teardown ordering.
- `sentry/doc.go`: rewritten for the handler-provider shape.
- `docs/packages.md`: untouched (no new package).

## Anti-scope

No sampling, no rate limiting (that is `ops/logsample`), no per-handler queues, no dynamic buffer resizing, no runtime toggling between sync and async, no exported async/leveled handler types.
