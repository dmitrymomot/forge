# Design: black-box test rework for `httpserver` & `supervisor`

- **Date:** 2026-06-26
- **Status:** Draft for review
- **Scope:** Convert every `*_test.go` file in `httpserver/` and `supervisor/` to
  black-box tests (`package httpserver_test` / `package supervisor_test`) that assert
  only through the exported API and externally-observable behavior. **Zero white-box
  test files. No production-code changes. No new dependencies.** Consolidate the
  redundancy that the conversion exposes.

## Overview

Today most tests in these two packages are white-box (`package httpserver` /
`package supervisor`): they apply unexported `Option` funcs to the unexported
`config` struct and read internal fields (`cfg.errs`, `cfg.logger`, `cfg.services`,
`s.cfg.Addr`), or call unexported functions directly (`drain`, `runService`,
`resolveLogger`, `serviceFunc`). That reliance is **convenience, not necessity** —
the behaviors being asserted are observable through the exported surface
(`New`/`Run`/`Name`, the option constructors, the sentinel errors, and structured
log output).

This rework brings both packages in line with the project standard
(`CLAUDE.md`: "black-box testing ONLY!", and the `black-box-tests-external-package`
memory). The payoff is **refactor-resilience** (tests stop naming internals, so
internal restructuring no longer breaks them) and **contract-as-documentation**, at
**no cost to the shipped binary** — `*_test.go` files are never compiled into the
production artifact regardless of their package clause.

The plan below was produced and then **adversarially verified**: a per-file analysis
pass proposed each conversion, and an independent pass tried to refute that the
proposed observable signal genuinely proves the behavior through the public API
alone, empirically running prototypes under `-race` and the full lint chain. Two
tests originally believed to require a white-box exception were proven convertible
with deterministic black-box techniques (a fake `net.Listener` and a subprocess
re-exec); both are adopted here, so **no white-box files remain**.

## Goals & non-goals

**Goals**

- Every `*_test.go` in `httpserver/` and `supervisor/` is `package <pkg>_test`.
- Tests assert only via exported identifiers and observable behavior (return values,
  HTTP round-trips, sentinel-error identity via `errors.Is`, structured log records,
  process exit status).
- Drop assertions that only inspected internal plumbing with no observable
  consequence; merge tests that become duplicates.
- `just check` passes: `fmt`, then `vet` + `golangci-lint` + `nilaway` +
  `betteralign`, then `go test -race -cover ./...`.

**Non-goals**

- No changes to any production (`.go` non-test) file in either package.
- No new third-party dependencies (testify is already in use; keep it).
- No attempt to raise coverage beyond what the current tests assert; equivalence of
  *observable* coverage is the target, not a coverage-number increase.

## Final file layout

### `httpserver/` — all files become `package httpserver_test`

| File | Action |
|---|---|
| `config_test.go` | Trivial swap — 3 tests unchanged in logic |
| `tls_test.go` | Trivial swap — 4 helpers + 3 tests unchanged in logic |
| `integration_test.go` | **No change** (already `package httpserver_test`) |
| `options_test.go` | Behavioral reframe (see below) |
| `server_test.go` | Trivial swaps + drain test replaced + `ConnState` test added |

### `supervisor/` — all files become `package supervisor_test`

| File | Action |
|---|---|
| `config_test.go` | Trivial swap (3 tests) |
| `context_test.go` | Trivial swap (2 tests) + optional strengthening |
| `helpers_test.go` | `fakeService` moves into the `_test` package |
| `service_test.go` | Reframe `serviceFunc` tests via `WithServiceFunc` + `Run` |
| `supervisor_test.go` | Trivial swaps + reframes + new subprocess recover test |
| `options_test.go` | **Deleted** — assertions drop or fold into `supervisor_test.go` |

"Trivial swap" = change the `package` clause to `<pkg>_test`, add the package import,
qualify identifiers with the package selector, and re-group imports with `just fmt`.
No assertion logic changes.

## Detailed conversion plan

### httpserver

#### `config_test.go` — trivial swap

`TestDefaultConfig`, `TestConfig_Validate`, `TestConfig_EnvTags` already touch only
exported identifiers (`DefaultConfig`, the exported `Config` fields, `Validate`,
`ErrInvalidConfig`, and reflection over the exported `Config` type). Swap package +
qualify. `TestConfig_EnvTags` stays a reflective tag assertion — it is a legitimate
black-box check of a documented public contract and cannot be reframed as a `Run`
test because the package imports no env loader (the tags are inert strings). It keys
fields by name, so betteralign field-reordering will not break it.

#### `tls_test.go` — trivial swap

`selfSigned`, `tlsClient`, `startTLS`, `waitTLS200` and the three TLS tests use only
`New`/`WithListener`/`WithTLSConfig`/`WithConfig`/`Run`. The CA-pinned `200`
round-trips (no `InsecureSkipVerify`) remain the observable proof that TLS was
actually negotiated. Keep all three tests distinct — in-memory config, on-disk cert
files, and config-takes-precedence-over-files are three different code paths.

#### `integration_test.go` — no change

Already `package httpserver_test`; proves `*httpserver.Server` satisfies
`supervisor.Service` (compile-time) and runs under `supervisor.Run`.

#### `options_test.go` — behavioral reframe

| Current test | Converts to |
|---|---|
| `TestDataOptions_SetFields` | `New(...).Name()` order-precedence, both directions: `WithConfig` applied last ⇒ `"http :1"`; `WithAddr` applied last ⇒ `"http :9090"`. Proves whole-block replacement + last-option-wins via the exported `Name()`. |
| `TestWithLogger_NilAllowed` | `Run` with `WithListener(ln)` + `WithLogger(nil)` returns `nil` and `NotErrorIs(ErrInvalidConfig)`. Drop the unobservable "logger stored verbatim" assertion. |
| `TestCodeOptions_NilAppendError` (listener/tlsconfig/basecontext/connstate) | One **table-driven** test over `map[string]httpserver.Option`; each option run through `New(noopHandler(), opt).Run(context.Background())` asserting `errors.Is(err, ErrInvalidConfig)`. Pass a real handler so the failure is unambiguously the option's rejection, not `ErrNoHandler`. Validation runs before any `net.Listen`, so there is no I/O on this path. |
| `TestCodeOptions_StoreNonNil` | **Dropped.** Store-only checks have no observable consequence except `WithConnState`, whose wiring is preserved by the new test below. `WithBaseContext` wiring is already covered by `TestRun_ForceCloseOnSlowHandler`; `WithListener`/`WithTLSConfig` by served tests. |
| — (new) `TestRun_ConnStateCallbackFires` | `WithConnState` callback records `http.StateNew` into a **buffered** channel via a non-blocking send; one round-trip (with the standard retry-until-ready loop) proves the callback fires within a timeout. This is the only non-error observable of `WithConnState` and is **mandatory** to preserve the coverage dropped from `TestCodeOptions_StoreNonNil` — treat the drop and this addition as an atomic pair. |

`noopHandler` moves into the `_test` package unchanged.

#### `server_test.go` — swaps + drain replacement + merge

| Current test | Converts to |
|---|---|
| `TestNew_SeedsDefaults` | **Dropped/merged.** `s.cfg.Addr`/`MaxHeaderBytes`/`handler` are unexported; the one observable assertion (`New(noop).Name() == "http :8080"`) already exists in `TestName_Derivation`. `MaxHeaderBytes` default stays covered by `config_test.go`. |
| `TestName_Derivation` | Trivial swap; remains the home of the default-Addr `"http :8080"` assertion. |
| `TestRun_RoundTripAndGracefulStop` | Trivial swap; keep the `resp != nil` guard inside the retry loop (nilaway). `startServed` helper moves verbatim. |
| `TestRun_GracefulDrainCompletesInflight` | Trivial swap; preserve the `started`-channel sync and the 100ms-vs-15s margin. |
| `TestRun_NilHandlerReturnsErrNoHandler` | Trivial swap (`ErrNoHandler` exported). |
| `TestRun_InvalidConfigReturnsError` | Trivial swap (`WithConfig(Config{Addr:""})` ⇒ `ErrInvalidConfig`). |
| `TestRun_BindFailureReturnsError` | Trivial swap; keep the original listener open (deferred close after `Run` returns) so the address stays bound. Negative assertion `NotErrorIs(ErrShutdownTimeout)` is the strongest available black-box check. |
| `TestRun_ForceCloseOnSlowHandler` | Trivial swap; build `cfg` via `DefaultConfig()` + `WithConfig`, `ShutdownTimeout = 50ms`. Handler observes base-context cancellation through its own `r.Context().Done()`. Keep generous 5s/1s outer waits. |
| `TestDrain_SurfacesBufferedServeError` | **Replaced** by black-box `TestRun_SurfacesServeErrorRacingCancel` (below). |

**`TestRun_SurfacesServeErrorRacingCancel` (new, replaces the white-box drain test).**
Inject a fake `net.Listener` via `WithListener` whose `Accept()` immediately returns
a **permanent** sentinel error (`var errBoom = errors.New(...)`); start `Run` and fire
`cancel()` concurrently; assert `errors.Is(Run(ctx), errBoom)`. Because `Accept`
fails on its own *before* shutdown begins, the error is buffered into the serve
channel first, so `Run` surfaces it whichever select branch wins — the direct
`<-serveErr` path or the drain path's buffered read. This deterministically proves
the documented contract ("a serve error that races with cancellation is always
surfaced, never masked as a clean stop"). Verified 3000/3000 plus `-race`.

Constraints for the fake listener:
- It is declared in a `_test.go` file, so its struct fields must satisfy
  **betteralign** ordering.
- `Accept()` must return a *permanent* error (a plain `error`, or a `net.Error`
  whose `Temporary()` is false) so `net/http`'s `Serve` returns instead of retrying.
- `Addr()` returns a stub `net.Addr`; `Close()` returns `nil`. `Name()` derivation
  is unaffected (the test does not assert on it).

This keeps the entire `server_test.go` in `package httpserver_test` — no file split.

### supervisor

#### `config_test.go` — trivial swap

`TestExportedDefaultConfig`, `TestConfig_Validate`, `TestConfig_EnvTags` use only
exported identifiers. After the swap, run `just fmt` so the local-prefix import lands
in its own group (otherwise `golangci-lint`'s goimports formatter fails).

#### `context_test.go` — trivial swap (+ optional strengthening)

`TestNewContext_CancelsOnSIGTERM` and `TestNewContext_StopIsSafe` use only
`NewContext` + stdlib. Optional strengthening for `StopIsSafe`: after `stop()`,
assert `ctx.Err()` is non-nil and `errors.Is(ctx.Err(), context.Canceled)` — upgrades
the weak "no panic" signal into a positive observable assertion using only the public
API. Keep both non-parallel (they install a real process signal handler).

#### `helpers_test.go` — move the shared double

`fakeService` implements the exported `Service` interface and references no unexported
supervisor identifiers, so it moves into `package supervisor_test` as a one-line
package-clause change. Its unexported `name`/`run` fields remain legal in same-(test)-
package composite literals. It backs the `Run`-based black-box tests in
`supervisor_test.go`.

#### `service_test.go` — reframe via `WithServiceFunc`

`serviceFunc` is an unexported type, so construct it through the exported
`WithServiceFunc` and observe behavior through `Run`:

- **Name** — register `WithServiceFunc("worker", fn)` and assert the `"service started"`
  JSON log record carries `service == "worker"` (emitted unconditionally at Info for
  every started service, independent of any shutdown ordering).
- **Invocation + ctx passthrough** — the func closes a channel and reads
  `ctx.Value(key)`; `runCtx` is `context.WithCancel(parent)`, which delegates `Value`
  to the parent, so an injected value survives to the func.
- **Error propagation** — single service returns a sentinel; assert
  `errors.Is(Run(...), sentinel)`. If an `err.Error()` substring assertion is kept,
  carry `//nolint:nilaway // err is guaranteed non-nil by require.ErrorIs above`.

#### `supervisor_test.go` — swaps, reframes, subprocess test, absorption

Trivial swaps (only package/qualifier change, logic intact):
`TestRun_NoServices_ReturnsErrNoServices`, `TestRun_EmptyName_ReturnsErrUnnamedService`,
`TestRun_SingleService_ReturnsWrappedError` (carry its existing `//nolint:nilaway`),
`TestRun_FirstExitStopsAll`, `TestRun_ContextCancel_ShutsDown`,
`TestRun_AggregatesNonCanceledErrors`, `TestRun_AlreadyCancelledContext_DoesNotStartServices`,
`TestRun_DuplicateNames_Warns`, `TestRun_GraceTimeout_AbandonsStuckService`,
`TestRun_GraceTimeout_LogsStuckNamesStructured`, `TestRun_ZeroTimeout_DrainsCooperativeService`,
`TestRun_InvalidConfigReturnsError`, `TestRun_PanicTriggersGracefulShutdown`.
`discardLogger` helper stays in this file (it moves with the package swap). Services
use the moved `fakeService` or `WithServiceFunc`.

Reframes:

| Current test | Converts to |
|---|---|
| `TestResolveLogger_PassthroughWhenSet` | **Dropped** — "the caller's logger is actually used" is already proven by every buffer-capturing `Run` test (`DuplicateNames_Warns`, `GraceTimeout_LogsStuckNamesStructured`, the panic-log test). |
| `TestResolveLogger_NilReturnsUsableLogger` | `Run` + `WithLogger(nil)` must not panic and returns `nil` (proves a usable discard logger was substituted). |
| `TestRunService_RecoverEnabled_ReturnsSingleLineErrPanic` | `Run` (recover on by default) + panicking `WithServiceFunc("boom", …)`; assert `errors.Is(ErrPanic)`, `Contains("kaboom")`, `NotContains("\n")`. |
| `TestRunService_RecoverEnabled_LogsStackAsAttribute` | Same `Run`, JSON logger; **line-split the buffer and select the `msg=="service panicked"` record** (whole-buffer `json.Unmarshal` fails — the buffer holds multiple records), then assert `service=="boom"` and `stack` (string) contains `"goroutine"`. |
| `TestRunService_RecoverDisabled_Propagates` | **New subprocess re-exec test** (below). |

**`TestRun_RecoverDisabled_PanicCrashesProcess` (new, replaces the white-box
recover-disabled test).** With recovery off, an unrecovered panic in a service
goroutine crashes the whole process and cannot be caught in-process. Use a subprocess
re-exec:

- An env var (e.g. `GO_SUPERVISOR_CRASH_CHILD=1`) gates a child branch that calls the
  **public** `Run(ctx, WithServiceFunc("boom", func(...) error { panic("kaboom") }),
  WithRecover(false), WithLogger(discardLogger()))` and returns.
- The parent runs `exec.Command(os.Args[0], "-test.run=TestRun_RecoverDisabled_PanicCrashesProcess")`
  with that env var set, captures `CombinedOutput()`, and asserts the error is a
  non-nil `*exec.ExitError` (non-zero exit) and the output contains `panic: kaboom`.

The Go runtime prints the panic to stderr even though logging is discarded, so
`CombinedOutput` captures it. Verified deterministic (exit code 2). This keeps
`supervisor_test.go` entirely `package supervisor_test`. Requires `os` and `os/exec`
imports.

Absorbed from the deleted `options_test.go`:

- `TestRun_NilRegistration_ReturnsErrInvalidConfig` — one table over `{nil Service,
  nil func}`, each asserting `errors.Is(err, ErrInvalidConfig)` and
  `NotErrorIs(err, ErrNoServices)` (locks in that the nil-registration error
  short-circuits before the no-services check).
- One lean `WithConfig` test — `WithConfig(Config{ShutdownTimeout: 50ms, …})` + a
  stuck service ⇒ `ErrShutdownTimeout`, proving the option applies the block.

#### `options_test.go` — deleted

Every test in it either inspects internal plumbing with no observable consequence or
duplicates an existing `Run`-based behavioral test:

| Current test | Disposition |
|---|---|
| `TestDefaultConfig` (internal `defaultConfig`) | Drop — `cfg.logger`/`cfg.services` unobservable; exported defaults covered by `config_test.go`. |
| `TestWithService_Appends` | Drop — "two services both run" is already exercised by `TestRun_AggregatesNonCanceledErrors`/`TestRun_FirstExitStopsAll`; slice-index order has no public observable. |
| `TestWithServiceFunc_CreatesNamedService` | Drop — covered by the reframed `service_test.go`. |
| `TestWithShutdownTimeout` | Drop — covered by `TestRun_GraceTimeout_AbandonsStuckService` (which uses `WithShutdownTimeout`). |
| `TestWithLogger_StoresValueIncludingNil` | Drop — capture covered by buffer-capturing `Run` tests; nil case by the reframed `TestResolveLogger_Nil…`. |
| `TestWithRecover_Toggles` | Drop — true branch covered by the recover-enabled reframes/`TestRun_PanicTriggersGracefulShutdown`; false branch by the new subprocess test. |
| `TestWithConfig_SetsWholeBlock` | Absorbed into the lean `WithConfig` test in `supervisor_test.go`. |
| `TestWithService_NilAppendsError`, `TestWithServiceFunc_NilFuncAppendsError` | Merged into `TestRun_NilRegistration_ReturnsErrInvalidConfig`. |

## Cross-cutting implementation rules

These are conversion-wide and were each reproduced during verification:

1. **`http.StateNew`**, not `http.ConnStateNew` (the latter does not exist and won't
   compile).
2. **Import grouping** — the `github.com/dmitrymomot/forge/...` import must be in its
   own goimports group, separated by a blank line from third-party imports, or
   `golangci-lint` fails with "File is not properly formatted (goimports)". Run
   `just fmt` after editing each file.
3. **nilaway** — carry `//nolint:nilaway // err is guaranteed non-nil by
   require.ErrorIs above` on any `err.Error()` assertion that follows a
   `require.ErrorIs`/`require.Error`. Guard every `*http.Response` use behind
   `err == nil && resp != nil`.
4. **Channels in served tests** — `ConnState`/handler-signal channels are buffered
   with non-blocking sends so the server goroutine never blocks; assert receipt within
   a timeout (2s typical). Served round-trips keep the retry-until-ready loop (~50 ×
   5ms) because `Run` binds asynchronously.
5. **betteralign** runs over `_test.go` files — order fields in any new test struct
   (e.g. the fake listener) accordingly.
6. **Timing margins** stay generous (configured 30–50ms vs 2–5s outer waits) to avoid
   CI flakes; no margin is tightened during conversion.
7. **One shared test package per directory** — after conversion all `_test.go` files
   in a directory share a single `_test` package, so every helper/type is declared
   exactly once across the package: `noopHandler`, `startServed`, and the fake
   listener live in `httpserver`'s test package; `fakeService` (in `helpers_test.go`)
   and `discardLogger` (in `supervisor_test.go`) are shared by all supervisor test
   files. Delete the obsolete `baseConfig()` helper in `httpserver/options_test.go`
   (it returned the unexported `config`) and any other now-unused white-box helper.

## Testing & acceptance

- `just fmt` then `just lint` (vet, golangci-lint, nilaway, betteralign) clean across
  both packages.
- `just test` = `go test -race -cover ./...` green, including the new subprocess test
  (which re-execs the test binary) and the fake-listener race test.
- No diff in any non-test `.go` file under `httpserver/` or `supervisor/`.
- `grep` confirms no `_test.go` file in either package declares `package httpserver`
  or `package supervisor` (every file is `…_test`).

## Risks & mitigations

- **Subprocess test portability** — re-exec via `os.Args[0]` + `-test.run` is the
  standard Go pattern and runs anywhere `go test` runs; the env gate prevents the
  child branch from firing in normal runs. Mitigation: assert on `*exec.ExitError`
  and the `panic:` substring, not an exact exit code beyond non-zero.
- **Fake-listener race test** — relies on `Accept` returning a permanent error so the
  serve error is buffered before shutdown. Mitigation: documented permanent-error
  requirement; verified 3000/3000 under `-race`.
- **Lost micro-coverage from dropped internal-plumbing asserts** — each dropped
  assertion was confirmed to have no observable consequence, or its observable half is
  retained/relocated. The `WithConnState` drop is explicitly paired with the new
  `ConnState` test so no wiring coverage is lost.
