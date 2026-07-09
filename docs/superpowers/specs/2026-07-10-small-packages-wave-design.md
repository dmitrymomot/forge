# Design — small-packages wave: logsample · idempotency · ipfilter

Date: 2026-07-10
Status: approved design, pending implementation plan

Three independent, single-responsibility packages built entirely on already-shipped
seams. Each follows the forge package anatomy from [design.md](../../design.md):
`doc.go` (runnable example) · `config.go`/`options.go` (`type Option func(*config)`,
never builders) · `errors.go` (`errors.Is`-matchable single-line sentinels) · impl.
Black-box tests only (`package X_test`); test doubles live with the seam owner.

All three are removed from [packages.md](../../packages.md) the moment they ship
(the roadmap lists only unbuilt packages).

---

## 1. `ops/logsample`

### Purpose
An `slog.Handler` decorator that caps log volume: records **below** a configured
threshold level are sampled "keep 1 of N"; records **at or above** the threshold
always pass. The "turn down the noise" knob for structured logs — sample the
floods, always keep the important levels. Sibling in shape to `ops/logredact`
(both are `slog.Handler` decorators, not features of `ops/logger`).

### Public surface
```go
func New(next slog.Handler, opts ...Option) slog.Handler

func WithRate(n int) Option            // keep 1 of every n sub-threshold records; default 10
func WithMinLevel(l slog.Level) Option // records with level >= l always pass; default slog.LevelWarn
```

### Behavior
- `Handle(ctx, r)`:
  - if `r.Level >= minLevel` → forward to `next` unchanged.
  - else atomically increment a shared 1-based counter and forward iff
    `(count-1) % rate == 0` (so the **1st, (1+N)th, (1+2N)th, …** sub-threshold
    record always passes — the first occurrence of a burst is never lost — and
    `rate == 1` keeps everything because `(count-1) % 1 == 0` always), drop
    otherwise (`return nil`).
- `Enabled(ctx, l)` delegates to `next.Enabled` (level filtering stays upstream;
  sampling is a per-record decision that can only happen in `Handle`).
- `WithAttrs(attrs)` / `WithGroup(name)` return derived handlers that wrap
  `next.WithAttrs(...)` / `next.WithGroup(...)` and **share the same counter
  pointer and immutable config pointer** — `logger.With(...)` spawns child
  handlers off one logical stream, and they must sample as one. Mirrors
  `logredact`'s `cfg`-by-pointer structure.
- Counter is a `*atomic.Uint64` (or `*atomic.Int64`) held in the handler struct;
  copied by pointer into every derived handler.

### Config / defaults
- `rate`: default **10** (keep 10% of sub-threshold records). `WithRate(1)` keeps
  everything (documented no-op escape hatch). `n < 1` is clamped to 1.
- `minLevel`: default **`slog.LevelWarn`** — Warn and Error always logged;
  Info and Debug sampled.

### errors.go
None — construction cannot fail.

### Dependencies
Stdlib only (`log/slog`, `sync/atomic`, `context`).

### Testing (black-box)
- Feed N×k records at Info through a capturing `next`; assert exactly k passed and
  the first record is among them.
- Warn/Error always pass regardless of rate.
- `WithRate(1)` passes 100%.
- Derived handler from `WithGroup`/`WithAttrs` samples against the same counter as
  its parent (interleave records through parent and child, assert combined 1-of-N).
- Concurrency: many goroutines calling `Handle` on shared + derived handlers,
  `-race` clean, kept-count within expected bound.

### Benchmarks (`Benchmark*` in the test file)
Isolate the decorator by wrapping a no-op `next` (`slog.DiscardHandler`), so the
numbers measure only the sampling decision.
- `BenchmarkHandle_AlwaysPass` — an at/above-threshold record (Warn); expect
  **0 allocs/op** (record forwarded as-is).
- `BenchmarkHandle_Sampled` — a sub-threshold record exercising the
  increment-and-modulo drop/keep path; expect **0 allocs/op**.
- `BenchmarkHandle_Parallel` (`b.RunParallel`) — the shared atomic counter under
  contention, to catch a hot-path regression.

### Estimated size
~120–160 LOC.

---

## 2. `web/ipfilter`

### Purpose
IP/CIDR allow/deny middleware over `web/clientip`: admin allowlists, partner IP
pinning, blocklists. One deny-wins evaluator covering allowlist-only,
denylist-only, and both-at-once.

### Public surface
```go
func New(opts ...Option) middleware.Middleware

func WithAllow(cidrs ...string) Option              // add to allowlist (CIDR or bare IP)
func WithDeny(cidrs ...string) Option               // add to denylist
func WithClientIP(opts ...clientip.Option) Option   // proxy/trust config for IP resolution
func WithResponder(r problem.Responder) Option      // rejection; default problem.JSON(WithStatus(403))
```

### Behavior — deny-wins, allowlist = default-deny gate
Resolve the client IP once per request via `clientip.Resolve(r, ipOpts...)`
(self-contained; does not require `clientip.Middleware` to have run first), then:

```
1. deny matches addr?      → BLOCK (responder, 403)   // deny always wins
2. allowlist configured?
      matches addr?        → ALLOW (next)
      no match?            → BLOCK (responder, 403)    // default-deny gate
3. (no allowlist)          → ALLOW (next)              // denylist-only mode
```

Worked examples (all approved during design):

- **Allowlist only** `WithAllow("203.0.113.0/24","198.51.100.7")`:
  `203.0.113.42` ✅ · `198.51.100.7` ✅ (bare IP = `/32`) · `8.8.8.8` ⛔403.
- **Denylist only** `WithDeny("192.0.2.0/24","203.0.113.66")`:
  `192.0.2.15` ⛔403 · `8.8.8.8` ✅.
- **Both** `WithAllow("203.0.113.0/24")` + `WithDeny("203.0.113.66")`:
  `203.0.113.10` ✅ · `203.0.113.66` ⛔403 (deny wins) · `8.8.8.8` ⛔403.

### Edge cases
- **Unresolvable / unparseable client IP:** deny doesn't match and allowlist
  doesn't match → **blocked when an allowlist is configured**, **allowed in
  denylist-only mode**. Safe under both models (an allowlist can never be
  satisfied by an unknown IP).
- CIDRs are parsed once at construction into `[]netip.Prefix`; a bare address
  becomes `/32` (IPv4) or `/128` (IPv6). IPv4 and IPv6 both supported.

### Config errors
`New` **panics** on an invalid `WithAllow`/`WithDeny` string (wrapping
`ErrInvalidCIDR`). Rationale: CIDRs are effectively compile-time config; a bad one
is a programmer error that should fail loud at startup (like `regexp.MustCompile`),
and panicking keeps the chain-friendly `func(...) middleware.Middleware` signature
(no `(mw, error)` return threading through every mount site).

### errors.go
- `ErrInvalidCIDR` — sentinel wrapped in the construction-time panic value so a
  `recover()` in a test can `errors.Is` it.

### Dependencies
`web/clientip`, `web/middleware`, `web/problem` (all shipped); stdlib `net/netip`.

### Testing (black-box)
- Table of (allow, deny, clientIP) → allow/403 covering the three example modes.
- Deny beats allow when both match.
- Unresolvable IP under allowlist vs denylist-only.
- IPv6 CIDR and bare-IP membership.
- `WithResponder` override changes the rejection body/status.
- `WithClientIP` proxy config: `X-Forwarded-For` honored only when trusted.
- Invalid CIDR → `New` panics with `ErrInvalidCIDR`.

### Benchmarks (`Benchmark*` in the test file)
Drive the middleware wrapping a no-op `next` handler via `httptest` requests; the
prefix sets are parsed once at construction, so the benchmark measures per-request
resolve + match.
- `BenchmarkServe_Allowed` and `BenchmarkServe_Blocked` — the two outcomes.
- Vary list size: a small set (a few CIDRs) and a larger set (~100 CIDRs) to show
  membership cost scales with list length as expected.
- Report allocs/op; the match itself (`netip.Prefix.Contains`) is alloc-free, so
  any per-request allocation traces to resolution and is worth surfacing.

### Estimated size
~160–220 LOC.

---

## 3. `web/idempotency`

### Purpose
`Idempotency-Key` middleware for mutating, partner-facing API calls: replay the
first response on retry, reject concurrent in-flight duplicates with 409, and
reject key-reuse-with-a-different-payload with 422. Rides `resilience/cache`'s
atomic SetNX claim.

### Public surface
```go
func New(store cache.Store, opts ...Option) middleware.Middleware

func WithHeader(name string) Option            // default "Idempotency-Key"
func WithMethods(m ...string) Option           // guarded methods; default POST, PUT, PATCH, DELETE
func WithTTL(d time.Duration) Option           // stored-response TTL; default 24h
func WithProcessingTTL(d time.Duration) Option // in-flight claim TTL; default 1m
func WithMaxBodySize(n int64) Option           // request+response buffer cap; default 1 MiB
func WithRequireKey() Option                   // guarded method w/o key → 400 (default: pass through)
```

### Request flow
1. **Not applicable → pass through untouched** when the method is not in the
   guarded set, or the key header is absent and `WithRequireKey` is not set.
   (Missing key + `WithRequireKey` → `400`.)
2. **Fingerprint the request:** read the body up to the cap, restore `r.Body`
   (buffer + `io.NopCloser`, webhook-style), compute
   `fingerprint = sha256(method + "\n" + path + "\n" + body)`. A request body over
   the cap → `413` (cannot safely fingerprint).
3. **Claim the key:**
   `store.Set(key, processingMarker, cache.WithSetNonExist(), cache.WithTTL(processingTTL))`.
   - **`ErrExists`** → `store.Get(key)` and decode the stored record:
     - *processing marker* → **409 Conflict** ("a request with this key is in
       progress"). The client retries later.
     - *completed response* → compare fingerprints:
       - match → **replay**: write stored status + captured headers + body.
       - mismatch → **422 Unprocessable Entity** (key reused with a different
         payload).
   - **success (claim won)** → we own the key; run the wrapped handler through a
     **buffering ResponseWriter** (our own — `middleware.recorder` records status
     and size but does not buffer the body, so it can't be replayed). After the
     handler returns, classify by status:
     - **2xx or 4xx**, response body within cap → `store.Set(key, doneRecord,
       cache.WithTTL(ttl))` (overwrites the marker with the full response), then
       flush the buffered response to the real client writer.
     - **5xx** → `store.Delete(key)` (release the claim so a genuine retry can
       re-execute), then flush to the client.
     - **response body over cap** → `store.Delete(key)`, flush uncached.
   - **panic in the handler** → release the claim (`store.Delete(key)`) via
     `defer`, then re-panic (leave the actual 500 handling to the app's recoverer).

### Stored record encoding
The `cache.Store` value is a `[]byte`; the package uses a small internal
length-prefixed encoding (no JSON base64 blow-up on the body) containing:
- a **discriminator** byte: `processing` vs `done`;
- the request **fingerprint** (32 bytes, only on `done`);
- HTTP **status code**;
- captured **response headers**;
- response **body**.

### Header capture policy
Captured/replayed response headers **exclude Set-Cookie and hop-by-hop headers**
(Connection, Keep-Alive, Transfer-Encoding, Upgrade, …). Replaying a `Set-Cookie`
(e.g. a rotated session/auth cookie) to a later retry is unsafe. This exclusion is
fixed and documented, not configurable.

### Two TTLs (why both)
- **Processing TTL** (short, default 1m): the lifetime of the in-flight claim
  marker. If the first request crashes/panics without releasing, the marker
  auto-expires and the key becomes usable again instead of wedging for 24h.
- **Response TTL** (long, default 24h): how long a completed response is replayable.

### Constraints / non-goals
- **Buffers the response** → not for streaming endpoints (SSE, chunked). Documented;
  the package targets mutating JSON API calls. Bodies over `WithMaxBodySize` are not
  cached (response) or rejected with 413 (request).
- **Store durability:** the built-in memory `cache.Store` is LRU-evicting and
  unsuitable for idempotency; `doc.go` directs users to `cache/redis` or another
  durable Store (per design.md's cache-seam note — same guidance as `session`,
  `otp`, `lockout`).
- Rejections (`400`/`409`/`422`/`413`) are emitted via `problem.JSON` with distinct
  problem `type`/`code` values so clients can branch on `errors.Is`-matchable codes.

### errors.go
- Internal decode sentinel(s) for a malformed stored record (treated as a cache miss
  / re-claim rather than a 500 where safe).
- Rejection responses are `problem` documents, not returned Go errors.

### Dependencies
`resilience/cache`, `web/middleware`, `web/problem`; stdlib `crypto/sha256`,
`bytes`, `io`, `net/http`.

### Testing (black-box)
- First call executes handler once and returns its response; identical retry
  replays **without** re-invoking the handler (assert handler call-count == 1).
- Concurrent duplicate while the first is in-flight → second gets **409**
  (coordinate with a handler that blocks on a channel).
- Same key, different body → **422**.
- Non-guarded method (GET) → pass-through, never touches the store.
- Missing key: default pass-through; `WithRequireKey` → **400**.
- 5xx response releases the claim (a subsequent retry re-executes the handler).
- Over-cap request → 413; over-cap response → sent to client but not stored (retry
  re-executes).
- Set-Cookie set by the handler is **not** present in a replayed response.
- Processing marker expiry (short `WithProcessingTTL`) frees a wedged key.
- Runs against the in-memory `cache.Store` for the suite (durability caveat is a
  deployment concern, not a test concern).

### Benchmarks (`Benchmark*` in the test file)
Drive the middleware via `httptest` against the in-memory `cache.Store`, isolating
the decorator's overhead from real network/DB latency.
- `BenchmarkReplay` — the hottest path under a retry storm: key present, fingerprint
  matches, stored response decoded and written (handler **not** invoked).
- `BenchmarkFirstCall` — claim + run a trivial handler + capture + encode + store.
- `BenchmarkFingerprint` — `sha256(method+path+body)` over a representative body
  size, plus record encode/decode round-trip.
These paths allocate by nature (buffering, hashing, stored bytes); the benchmarks
establish a baseline and guard against regressions rather than targeting 0 allocs.

### Estimated size
~320–400 LOC (largest of the three; still one responsibility).

---

## Cross-cutting notes
- **No new seams.** All three consume existing seams (`slog.Handler`,
  `cache.Store`, `clientip`, `middleware.Middleware`, `problem.Responder`).
- **Product-or-brick:** `logsample` and `ipfilter` are complete products
  (wire-and-go); `idempotency` is a complete product. None is a single-consumer
  slice of another package.
- **Env-loadable Config:** none of the three needs an env-loadable `Config` — they
  are middleware/handler constructors configured by the wiring code, not
  independently env-bootstrapped services. (No env-prefix tags required.)
- **Benchmarks ship with every package** — each has `Benchmark*` functions
  covering its hot path(s) with `b.ReportAllocs()`, run via `go test -bench=. -benchmem`.
  `logsample` and `ipfilter` target **0 allocs/op** on the decision path; `idempotency`
  benchmarks establish a regression baseline (allocation is inherent to buffering +
  hashing + serialization).
- **`just fmt ./<pkg>/...` + `just lint`** after each package.
