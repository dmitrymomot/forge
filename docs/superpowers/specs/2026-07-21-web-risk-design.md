# web/risk — Design

**Date:** 2026-07-21
**Status:** Approved

## Purpose

Generic anti-fraud gate: consumer-supplied scorer functions each return a fraud probability for an input, a pluggable strategy combines them, and a threshold gate decides whether the call proceeds, trips a fraud handler, or errors. Mechanism only — the package owns combining, gating, and error flow; all fraud *logic* (useragent checks, fingerprint mismatch, velocity, geo rules) lives in consumer scorers. Fills the consumer-side "fraud diversion / bot filtering" seam that `web/smartlink` explicitly leaves to callers, and is equally usable mid-handler (signup, deposit, bonus claim) or off-request (queue consumers).

Not `auth/abac` (boolean predicate policy → allow/deny): `risk` is graded scoring with a threshold and an action hook. Not `web/ipfilter` (static list gate). If a consumer only needs boolean rules, they should use `abac`, not this.

## Decisions (from brainstorming Q&A)

1. **Multiple scorers + combining strategy**, not a single function. Default strategy `Max` — fraud signals are not additive; one 0.9 signal must not be diluted by innocent 0.1s in an average. `Mean` and `Weighted` ship as alternatives.
2. **Generic transport-free core + thin `net/http` middleware adapter in the same package** (option b from Q&A). Core never imports `net/http`; the adapter follows the `web/tenant` / `auth/guard` middleware pattern.
3. **Fraud handler decides the outcome.** On gate trip with an `OnFraud` handler set, the handler runs and its return value controls flow: `nil` → proceed (shadow mode / divert-and-allow), error → blocked with that error. No handler → blocked with `ErrFraud`.
4. **Fail closed, no fail-open option in v1.** Scorer error, NaN, or out-of-range score fails the check. Consumers wanting lenient scorers wrap their own recover-to-zero.
5. **Name `web/risk`** — short, mechanism-named like `fingerprint`/`ipfilter`/`lockout`.

## Core API

```go
package risk

type Scorer[T any] func(ctx context.Context, input T) (float64, error)

type Strategy func(scores []float64) float64 // Max (default), Mean; weighted average via WithWeights

type Engine[T any] struct{ /* unexported */ }

func New[T any](opts ...Option[T]) (*Engine[T], error)

// Options
func WithScorer[T any](s Scorer[T]) Option[T]          // repeatable; New errors with zero scorers
func WithGate[T any](threshold float64) Option[T]      // required; must be in (0,1]
func WithStrategy[T any](st Strategy) Option[T]        // default Max; mutually exclusive with WithWeights
func WithWeights[T any](weights ...float64) Option[T]  // weighted-average strategy; arity validated in New
func OnFraud[T any](h func(ctx context.Context, input T, score Score) error) Option[T]

// Methods
func (e *Engine[T]) Check(ctx context.Context, input T) error          // nil = proceed
func (e *Engine[T]) Score(ctx context.Context, input T) (Score, error) // combined score, no gating — telemetry / shadow use
```

`Score` carries the combined value plus attribution:

```go
type Score struct {
    Value    float64   // combined score
    Peak     float64   // highest individual scorer value
    PeakIdx  int       // index (registration order) of the peaking scorer
    Scores   []float64 // per-scorer values, registration order
}
```

`ErrFraud` is an `errors.As`-able type wrapping the `Score`, so callers and the middleware can log which scorer tripped the gate:

```go
type FraudError struct{ Score Score }
func (e *FraudError) Error() string
func (e *FraudError) Is(target error) bool // true for ErrFraud

var ErrFraud = errors.New("risk: fraud detected") // errors.Is(err, ErrFraud) matches any *FraudError
```

## Semantics

**Check flow:** run all scorers in registration order → validate each result (see Edge handling) → combine via strategy → compare against gate. `score >= gate` trips (boundary inclusive: a scorer returning exactly the gate value is fraud). Below gate → `Check` returns nil.

**On trip:** no `OnFraud` → return `*FraudError` (matches `errors.Is(err, ErrFraud)`). With `OnFraud` → call it with input and `Score`; return its error verbatim (nil proceeds). The handler's error is NOT wrapped in `FraudError` — the handler owns the contract with its caller; it can return a `FraudError` itself if it wants that shape.

**Scorers run sequentially, all of them, even under `Max`.** No short-circuit on first score ≥ gate: `Score.Scores` attribution must be complete, and scorers are consumer code whose side effects (metric increments) should not silently vary with registration order. Scorer count is small (units, not hundreds); if a proven-hot path needs short-circuiting later, that is a measured optimization behind the same API.

**Strategies:**

- `Max` — highest score wins. Default.
- `Mean` — arithmetic mean.
- `WithWeights(w ...float64)` — a weighted average configured as an option rather than a `Strategy` value, because a plain `func([]float64) float64` cannot expose its weight arity for `New`-time validation. Weight count must equal scorer count, weights must be non-negative and sum > 0; normalized internally so `WithWeights(1, 3)` means 25%/75%. Mutually exclusive with `WithStrategy`.
- Custom: any `func([]float64) float64`. Output is validated like a scorer output (NaN / out-of-range from a custom strategy → error).

## Edge handling (fail closed)

- Scorer returns error → `Check`/`Score` return that error wrapped with scorer index context. No skip, no zero-substitute.
- Scorer returns NaN, ±Inf, or a value outside [0,1] → error, not clamp. A NaN comparing false against the gate is the `auth/lockout` NaN hole; silent clamping hides broken scorers.
- Strategy output NaN / out-of-range → error (covers custom strategies).
- `New` validation errors: zero scorers, gate outside (0,1], nil scorer, nil strategy, `Weighted` arity/negative/zero-sum violations.
- `ctx` cancellation: scorers receive ctx and are expected to honor it; the engine checks `ctx.Err()` between scorers and aborts with the ctx error.

## Middleware adapter

```go
func Middleware[T any](e *Engine[T], buildInput func(*http.Request) T, opts ...MiddlewareOption) func(http.Handler) http.Handler

func WithRejectHandler(h func(w http.ResponseWriter, r *http.Request, err error)) MiddlewareOption
func WithLogger(l *slog.Logger) MiddlewareOption // default logger.NewNope()
```

- `Check` nil → `next.ServeHTTP`.
- `Check` returns any error (fraud or scorer infrastructure failure) → reject: default plain `403 Forbidden` response; `WithRejectHandler` overrides (consumer can emit `web/problem`, redirect to a decoy, etc.). Fail closed on infrastructure errors — same as fraud.
- Rejections and scorer errors are logged at Warn/Error respectively via the optional logger, single-line slog attrs (score value, peak index, path); never the error text in the response body beyond the status line.
- `OnFraud`-returns-nil surfaces as `Check` nil → request proceeds; the middleware never knows the gate tripped. Shadow mode works unchanged under the middleware.

## Tenancy

Pure compute, no storage, no I/O → tenancy is a passed value, not a seam (`core/phone` precedent). Tenant identity travels inside `T` for scorers to read, or the consumer constructs per-tenant engines (construction is cheap and allocation-free after `New`). No `WithScope`; the package doc states this explicitly.

## Dependencies

Core: stdlib only. Middleware: `net/http`, `log/slog`, `ops/logger` (for `NewNope` default). No storage, no drivers.

## Anti-scope

- No built-in scorers — fingerprint/geoip/useragent/velocity checks are consumer functions wired from existing packages.
- No velocity/counter storage — a velocity scorer backs itself with `resilience/cache` or `ratelimit.Store` on the consumer side.
- No ML, no model loading, no feature extraction.
- No verdict persistence or case management — audit is the consumer's `ops/auditlog` call inside `OnFraud`.
- No challenge/step-up flows (captcha, OTP) — an `OnFraud` handler or reject handler can redirect to one.
- No async/batch scoring API.

## Testing (black-box, `risk_test`)

- Strategy math: `Max`, `Mean`, `Weighted` normalization, custom strategy.
- Gate boundary: `score == gate` trips; `gate - ε` passes.
- Handler semantics: nil-proceeds, error-blocks-verbatim, no-handler → `ErrFraud` with correct `Score` attribution (Peak/PeakIdx/Scores order).
- Fail-closed: scorer error, NaN, +Inf, -0.1, 1.1, strategy-output violations; ctx cancellation between scorers.
- `New` validation matrix.
- Middleware: pass → next reached; fraud → 403 default; `WithRejectHandler` override; infrastructure error → 403; shadow (OnFraud nil) → next reached.
- Fuzz: `Weighted` normalization over random weight/score vectors — output always in [0,1], no NaN.

## Benchmarks

`bench_test.go` per repo rule: `Check` pass path (target zero allocs — `Score.Scores` slice must not escape on the nil-return path; reuse via stack or pool if measurement demands), `Check` trip path, 1/4/16 scorers, each built-in strategy. Post-benchmark optimization pass with before/after numbers in the PR.
