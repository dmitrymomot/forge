# Web-transport middleware bundle (P3 keystone) — design

> Status: **approved design**, pre-implementation. Date: 2026-07-03.
> Bundle: `middleware` · `problem` · `recoverer` · `requestid` · `reqlog` · `clientip`.
> Follows the resilience/caching bundle (PR #25) as the second recommended-tier bundle.

## 1. Context & motivation

forge ships the HTTP *seed* — `httpserver`, `hostrouter`, `render`, `htmx`, `request`, `logger`, `supervisor` — but nothing in the P3 web-transport layer. There is no `middleware.Middleware` seam, no error responder, no panic recovery, no request-id/access-log/client-ip plumbing. Every real HTTP app needs these, and every later layer (P4 `csrf`/`ratelimit`/`idempotency`, P5 `authmw`/`session`) depends on the `middleware` seam and the `problem` responder. This bundle is the keystone that makes the HTTP seed production-usable.

`httpserver.New(handler, ...)` takes a single `http.Handler`, so middleware composes **externally** and the wrapped handler is passed in — there is no `.Use()` on the server. `middleware` is therefore a standalone composition package.

## 2. Scope

**In (6 packages):**

- `middleware` — the `Middleware` seam, `Chain`/`Wrap`, and a shared status/size-capturing `ResponseWriter`.
- `problem` — a pluggable error-**Responder** seam with a predefined JSON (RFC 9457) responder. The seam stays open so a consumer can supply their own (e.g. HTML) responder; no HTML/negotiated responder ships in this cut.
- `recoverer` — panic → 500-via-responder, logged.
- `requestid` — inbound-or-generated correlation id + `logger.ContextExtractor`.
- `clientip` — hardened, batteries-included client-IP resolution engine + caching middleware + canonical accessor + `logger.ContextExtractor`. **Absorbs and replaces `request.ClientIP`** and the roadmap's planned `realip`.
- `reqlog` — one structured access-log line per request.

Plus one `examples/` recipe wiring the whole chain end-to-end.

**Out (deferred to later PRs):** `timeout`, `bodylimit`, `cors`, `compress`, `bind`, `negotiate` (the standalone content-negotiation package), `static`, `upload`, `proxy`, `conditional`.

## 3. Conventions (roadmap Design DNA)

- No magic: no reflection; values via params, not context (context only for request-scoped reads).
- One idiom per package: stateless free-funcs, or `New(...Option)` where `type Option func(*config)` — **never builders**.
- Anatomy: `doc.go` (runnable example), `config.go`/`options.go`, `errors.go` (`errors.Is`-matchable single-line sentinels), impl.
- Flat top-level packages, ~80–250 LOC each; **black-box tests only** (`package X_test`).
- Minimal deps: stdlib + existing seed packages only; no new external dependency.
- Run `just fmt <file>` per change, `just lint` at the end; race-green like PR #25.

## 4. `middleware`

Stdlib-only composition seam. ~120 LOC.

```go
// Middleware wraps a handler. The FIRST middleware in a chain is the OUTERMOST.
type Middleware func(http.Handler) http.Handler

func Chain(mws ...Middleware) Middleware                    // compose N into 1
func Wrap(h http.Handler, mws ...Middleware) http.Handler   // apply to a handler
```

Composition order: `Wrap(h, a, b, c)` yields `a(b(c(h)))` — `a` is outermost (sees the request first, the response last). Empty chain returns the handler unchanged.

### Shared response writer

`recoverer` and `reqlog` both need the final status, byte count, and whether the header was committed. One shared wrapper, rather than each re-implementing it:

```go
type ResponseWriter interface {
    http.ResponseWriter
    Status() int    // 0 until first write; the code passed to WriteHeader, or 200 on implicit write
    Written() int64 // bytes written to the body
    Wrote() bool    // has the header been committed?
    Unwrap() http.ResponseWriter
}

func WrapWriter(w http.ResponseWriter) ResponseWriter
```

**Optional-interface preservation via `Unwrap`.** Rather than the 2ⁿ interface-combination problem, the wrapper implements `Unwrap() http.ResponseWriter`; downstream code that needs `Flush`/`Hijack`/`SetWriteDeadline` uses `http.ResponseController` (Go 1.20+), which unwraps to reach the real writer. `WrapWriter` is idempotent-friendly: if `w` is already a `middleware.ResponseWriter`, it is returned as-is.

## 5. `problem`

Pluggable error-responder seam. Depends on `render`, `errorsx`, `request`, `bufpool`.

```go
// Responder writes err as an HTTP error response. The seam every error-writing
// middleware/handler accepts.
type Responder func(w http.ResponseWriter, r *http.Request, err error)
```

### The document (RFC 9457 + forge extensions)

Exported so custom responders can build one:

```go
type Problem struct {
    Type     string            `json:"type,omitempty"`     // URI; default "about:blank"
    Title    string            `json:"title,omitempty"`    // status text by default
    Status   int               `json:"status"`
    Detail   string            `json:"detail,omitempty"`   // 4xx only; never for 5xx
    Instance string            `json:"instance,omitempty"` // request path
    Code     string            `json:"code,omitempty"`     // errorsx.Code(err)
    Fields   map[string]string `json:"fields,omitempty"`   // validate/request field errors
}

func From(err error, opts ...Option) Problem
```

`From` behavior:
- **Status** via a pluggable `StatusOf func(error) int`, default `request.StatusCode` (nil → 200, `*request.Error` → 400/413/415, other → 400). A `WithStatus(code)` option forces a specific status.
- **Code** via `errorsx.Code(err)` when present.
- **Fields** extracted when the error is a `validate.Errors` (via its `ByField() map[string][]Violation`, each field's violation messages joined) or a `*request.Error` (`{source+key: kind}`).
- **Detail**: for 4xx, the error message; for **5xx, omitted** (never leak internals). `Title` defaults to `http.StatusText(status)`.

### Predefined responders

Configurable via options (`WithLogger`, `WithStatusOf`/`WithStatus`, `WithTypeBaseURI`):

```go
func JSON(opts ...Option) Responder      // application/problem+json, via render.JSON + bufpool
```

- 5xx responders **log** the underlying error (with `r.Context()`, so `request_id`/`client_ip` ride along) but never place `err.Error()` in the body.
- Default responder anywhere one is unset: `JSON()`.
- **JSON is the only shipped responder in this cut.** The `Responder` seam is open: a consumer wanting an HTML error page passes their own `func(w, r, err)`. A shipped `problem.HTML` (and a `Negotiate` that picks by `Accept`) can be added later without breaking the seam.

### Sentinels

`errors.go`: none required beyond what callers pass; `problem` classifies, it does not mint domain errors.

## 6. `clientip`

Owns **all** client-IP concerns: the hardened stateless resolver (moved out of `request`), the caching middleware, the canonical accessor, and the log extractor. Depends on `middleware`, `ctxkey`, `logger`, stdlib `net`/`netip`.

### 6.1 Resolution engine

One engine, one code path, parameterised by a mode. It reads **all** instances of each forwarding header (`r.Header.Values`, not `Get`) and understands both `X-Forwarded-For` and RFC 7239 `Forwarded`.

Modes (set by strategy options, last-wins):
- **remoteAddr** (default) — `RemoteAddr` only; ignores all headers. Safest.
- **singleHeader(name)** — first valid IP from the named header (port stripped); falls back to `RemoteAddr` if absent.
- **trustedRanges(cidrs)** — build the chain from XFF + `Forwarded` `for=` (in header order) + `RemoteAddr`; walk right-to-left; return the first address not inside a trusted prefix. If every hop is trusted, return the leftmost **non-private** address, else `RemoteAddr`.
- **trustedHopCount(n)** — from the right of the combined chain, skip exactly `n` hops, return the next valid address (bounds-guarded).
- **leftmostNonPrivate** — best-effort: leftmost public address in the chain; `RemoteAddr` if none. Spoofable; logging-only.

This is "option B" from the design discussion: multi-header reads, `Forwarded` parsed in trusted mode, and a non-private all-trusted fallback.

### 6.2 Options — strategies + provider presets

```go
// Strategies (last-wins)
func RemoteAddrOnly() Option
func SingleHeader(name string) Option
func TrustedRanges(cidrs ...string) Option   // string CIDRs; parse errors accumulate on config
func TrustedHopCount(n int) Option
func LeftmostNonPrivate() Option

// Presets — dedicated-header providers only (honest: the vendor guarantees the header)
func Cloudflare() Option      // SingleHeader("CF-Connecting-IP")
func Fastly() Option          // SingleHeader("Fastly-Client-IP")
func CloudFront() Option      // SingleHeader("CloudFront-Viewer-Address") (strips port)
func Akamai() Option          // SingleHeader("True-Client-IP")
func AzureFrontDoor() Option  // SingleHeader("X-Azure-ClientIP")
func Envoy() Option           // SingleHeader("x-envoy-external-address")

// Generic reverse-proxy conveniences
func TrustPrivateProxies() Option  // TrustedRanges over all private/loopback/link-local/CGNAT/ULA
func XRealIP() Option              // SingleHeader("X-Real-IP") — de-facto nginx/Traefik/ingress header

func PrivateRanges() []netip.Prefix  // exported for custom composition
```

**Design principle (documented in `doc.go`):** a per-vendor preset exists **only** when the vendor sets a dedicated, reliable header it overwrites/strips on ingress (Cloudflare, Fastly, CloudFront, Akamai, Azure Front Door, Envoy). Generic XFF proxies — nginx, Caddy, Traefik, HAProxy, k8s ingress, DigitalOcean LB, AWS ALB — have no guaranteed dedicated header; a vendor preset there would be a false promise. Their correct, topology-based resolution:

| Proxy | Recommended |
|---|---|
| nginx / Caddy / Traefik / HAProxy / k8s ingress (private net) | `TrustPrivateProxies()`, or `XRealIP()` if you set X-Real-IP |
| Envoy | `Envoy()` |
| DigitalOcean LB / AWS ALB / cloud LB with known ranges | `TrustedRanges("<lb cidr>")` or `TrustedHopCount(n)` |
| Cloudflare / Fastly / CloudFront / Akamai / Azure Front Door | matching provider preset |

### 6.3 Middleware + canonical accessor

```go
func Resolve(r *http.Request, opts ...Option) string   // stateless parse (engine)
func Middleware(opts ...Option) middleware.Middleware    // resolve once, cache in ctx
func Get(r *http.Request) string                         // ctx-first, safe header fallback
func From(ctx context.Context) (string, bool)            // pure-ctx read; bool = "middleware ran"
var LogExtractor logger.ContextExtractor                 // slog attr "client_ip"; skips when empty
```

`Get` is the single accessor everyone (handlers, `ratelimit`, `audit`, the extractor) calls:

```go
func Get(r *http.Request) string {
    if ip, ok := From(r.Context()); ok {
        return ip        // middleware already decided, using the app's proxy config
    }
    return Resolve(r)    // middleware not installed → safe default (RemoteAddr-only)
}
```

Invariants:
- Once `Middleware` runs, `From` returns `(value, true)` **even when value is ""** — the authoritative decision, made once with the configured trust. `Get` never re-parses after the middleware ran (no chance of a handler resolving with looser trust).
- `Resolve` with no options is **safe-by-default** (`RemoteAddr`-only). The spoofable best-effort scan is opt-in via `LeftmostNonPrivate()`/`SingleHeader(...)`. This is a deliberate, documented divergence from the removed `request.ClientIP`, whose default was the spoofable scan.
- Cached via a package-private `ctxkey.Key[string]`; `ctxkey`'s `From` already returns `(value, ok)` where `ok` reports whether the key was set, so the empty-but-resolved case (`("", true)`) is distinguished from not-run (`("", false)`) without any extra marker.

### 6.4 Migration: remove `request.ClientIP`

Blast radius verified: `request.ClientIP`, `WithClientIPHeaders`, `WithTrustedProxies`, `ClientIPOption` are used **only inside `request/`** (impl, tests, one `doc.go` mention) — no external consumers.

- Delete `request/clientip.go`; move + harden the logic into `clientip`.
- Move `request/clientip_test.go` into `clientip` (black-box), adapting to the new API and safe default.
- The existing `TestClientIPTrustedIgnoresForwarded` documents the *old* limitation; it flips to `TestClientIPTrustedParsesForwarded` (Forwarded now honored in trusted mode).
- Update the `BearerToken, ClientIP, ...` mention in `request/doc.go`.
- `netip.Prefix` string parsing (`TrustedRanges`) accumulates parse errors on the middleware config, surfaced the forge way (options accumulate, reported at construction/first use).

## 7. `recoverer`

Panic → 500, logged. Depends on `middleware`, `problem`, `logger`.

```go
func New(opts ...Option) middleware.Middleware
func WithResponder(r problem.Responder)  // default problem.JSON()
func WithLogger(l *slog.Logger)          // default slog.Default()

var ErrPanic = errors.New("recoverer: handler panicked")  // errors.Is-matchable
```

Behavior:
- `defer`/`recover`; wraps the writer with `middleware.WrapWriter` (or reuses if already wrapped).
- On a recovered value equal to `http.ErrAbortHandler` → **re-panic** (net/http's sanctioned silent-abort signal; must propagate).
- Otherwise log the value + stack at **Error** using `r.Context()`; then if `!w.Wrote()` invoke the responder with `fmt.Errorf("%w: %v", ErrPanic, v)` → 500; if the header was already committed, only log (status can't change).

## 8. `requestid`

Inbound-or-generated correlation id. Depends on `middleware`, `id`, `ctxkey`, `logger`.

```go
func New(opts ...Option) middleware.Middleware
func WithHeader(name string)        // default "X-Request-ID"
func WithGenerator(gen func() string) // default id.New
func WithTrustInbound(trust bool)   // default true

func From(ctx context.Context) (string, bool)
var LogExtractor logger.ContextExtractor  // slog attr "request_id"
```

Behavior:
- If `WithTrustInbound` (default) and the inbound header is present **and** passes a conservative guard — printable ASCII, `1..=128` chars — use it; else generate `id.New()`.
- Store in ctx via a package-private `ctxkey.Key[string]`; echo the value on the response header (set before the handler runs so it is present even on panic/early write).

## 9. `reqlog`

One structured access line per request. Depends on `middleware`, `logger`.

```go
func New(log *slog.Logger, opts ...Option) middleware.Middleware
func WithLevelFunc(fn func(status int) slog.Level)  // default 5xx→Error, 4xx→Warn, else Info
func WithSkip(pred func(*http.Request) bool)        // e.g. skip /healthz
```

Behavior:
- Wrap the writer; `defer` a single line with `method`, `path`, `status`, `duration`, `bytes`, logged via `log.LogAttrs(r.Context(), level, ...)` so `request_id`/`client_ip` inject automatically through the wired extractors — and so do all other logs emitted during the request.
- `status` uses `ResponseWriter.Status()` (200 if the handler wrote a body without an explicit `WriteHeader`).
- Does **not** read requestid/clientip itself — decoupled; those arrive via the logger's `ContextExtractor`s.

## 10. Wiring, dependency DAG & the example recipe

Internal edges (all within seed + this bundle; no new external dep):

```
middleware → (stdlib)
problem    → render, errorsx, request, bufpool
clientip   → middleware, ctxkey, logger
requestid  → middleware, id, ctxkey, logger
reqlog     → middleware, logger
recoverer  → middleware, problem, logger
```

Canonical end-to-end wiring (the `examples/` recipe):

```go
log, _ := logger.New(
    logger.WithContextExtractors(requestid.LogExtractor, clientip.LogExtractor),
)

h := middleware.Wrap(mux,
    recoverer.New(),  // defaults to problem.JSON()
    requestid.New(),
    clientip.Middleware(clientip.TrustPrivateProxies()),
    reqlog.New(log, reqlog.WithSkip(healthz)),
)

srv := httpserver.New(h, httpserver.WithAddr(":8080"), httpserver.WithLogger(log))
```

The recipe includes a handler that returns a domain error carrying an `errorsx` code and a `validate.Errors`, demonstrating the `problem` field/code mapping — the "demonstrably usable" bar from PR #25.

## 11. Testing strategy

Black-box (`package X_test`), `httptest` + a next-handler spy, race-green:

- **middleware** — chain order (outermost-first), empty chain identity, `WrapWriter` status/bytes capture, `Unwrap` reaches `http.Flusher`/`Hijacker` via `http.ResponseController`, idempotent re-wrap.
- **problem** — `From` status/code/field mapping for `request.Error`, `validate.Errors`, coded and plain errors; `JSON` emits `application/problem+json`; **5xx never contains `err.Error()`**; `WithStatus` override; a custom `Responder` is honored where one is passed.
- **clientip** — every strategy and preset: remoteAddr default; single-header (present/absent → fallback); trusted-ranges rightmost-untrusted over XFF **and** Forwarded; multi-instance XFF headers flattened; trusted-hop-count bounds; leftmost-non-private; all-trusted → non-private fallback; bad CIDR string → config error; IPv6/IPv4-mapped normalization; `Middleware` caches (resolves once, `From` true-even-when-empty); `Get` fallback when uninstalled; `LogExtractor` skips empty. Port the migrated `request` cases; add `TestClientIPTrustedParsesForwarded`.
- **recoverer** — panic → 500 problem; already-written response only logs; `http.ErrAbortHandler` re-panics; `ErrPanic` is `errors.Is`-matchable; custom responder used.
- **requestid** — inbound trusted vs generated; guard rejects oversized/non-ASCII/empty; response header echoed; `WithGenerator`/`WithHeader`/`WithTrustInbound`; `LogExtractor` injects.
- **reqlog** — level-by-status; `WithSkip`; end-to-end assertion that `request_id` + `client_ip` appear when the logger has both extractors wired (integration with `requestid` + `clientip`).

## 12. Breaking changes

- `request.ClientIP` + `ClientIPOption` + `WithClientIPHeaders` + `WithTrustedProxies` are **removed** (relocated to `clientip`). Pre-release v2, no deprecation shim. Only in-package consumers affected.

## 13. Build order within the bundle

1. `middleware` (seam + `WrapWriter`) — everything depends on it.
2. `problem` (needs `render`/`errorsx`/`request`/`bufpool`) — `recoverer` depends on it.
3. `clientip` (independent of `problem`; needs `middleware`) — includes the `request.ClientIP` migration.
4. `requestid`, `reqlog`, `recoverer` — leaf middleware.
5. `examples/` recipe.

Ship as one PR (matching the resilience-bundle cadence), or split `middleware`+`problem`+`clientip` first and the three leaf middleware second if review surface is a concern.

## 14. Non-goals (this bundle)

`timeout`, `bodylimit`, `cors`, `compress`, `bind`, standalone `negotiate`, `static`, `upload`, `proxy`, `conditional` — the next P3 PR. Full realclientip-style pluggable Strategy *interface* — not needed; the option-based strategies + presets cover the cases, and a rarely-needed one can be added as another option later. HTML / negotiated error responders — out of this cut; only `problem.JSON` ships. The `Responder` seam stays open so a consumer supplies their own HTML responder, and a shipped `problem.HTML` / `Negotiate` can be added later without breaking the seam.
