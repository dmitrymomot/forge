# Design: `hostrouter` — Host-header routing as a plain `http.Handler`

- **Date:** 2026-06-26
- **Status:** Draft for review
- **Scope:** A new standalone `hostrouter` package that dispatches requests to
  different `http.Handler`s by the request's `Host` header (exact + single-label
  wildcard), exposing the matched host/pattern/subdomain to handlers via request
  context. It is consumed by `httpserver` like any other handler; `httpserver` is
  **not** modified.

## Overview

`hostrouter.Router` is an `http.Handler` that looks at `req.Host` and forwards the
request to the handler registered for that host, falling back to a default handler
(HTTP 404 unless overridden). It is the v2 successor to the v1 `pkg/hostrouter`,
rebuilt to match the framework's conventions (functional options, single-line
`errors.Is` sentinels, stdlib-only) and tuned hard for the request hot path.

Because it is just an `http.Handler`, it composes directly:

```go
srv := httpserver.New(router, httpserver.WithAddr(":8080"))
```

`httpserver` stays agnostic — no host-routing knowledge leaks into the server
lifecycle package.

**Why a dedicated package when `http.ServeMux` already routes by host?** Since Go
1.22 `mux.Handle("api.example.com/", h)` matches an exact host, so the value-add
here is the two things stdlib cannot do: **single-label wildcard subdomains**
(`*.example.com` → `foo.example.com`) for multi-tenant apps, and a **clean,
overridable fallback**. The matched-subdomain extraction (the tenant label) is
surfaced to handlers so multi-tenant code does not re-parse `req.Host`.

## Goals

- Route by `Host` header to per-host `http.Handler`s, with a default fallback.
- **Exact** matches and **single-label wildcard** matches (`*.example.com` →
  `foo.example.com` only); exact always wins.
- Expose the match (normalized host, matched pattern, captured subdomain label) to
  the downstream handler ergonomically, via request context + thin accessors.
- **Hot-path performance:** lock-free concurrent serving, zero allocations in the
  match/normalize logic, and the minimum possible allocations on dispatch — with a
  zero-allocation opt-out for routes that don't need the accessors.
- Stdlib only; no `Config` (routes are code, not serializable data).
- Misconfiguration is a startup programming error → **panic** with a matchable
  sentinel, like `http.ServeMux`.

## Non-goals

- Path/method routing, middleware, or handler helpers — the registered handlers
  (typically `*http.ServeMux` or chi routers) own that.
- Multi-label wildcards (`*.example.com` matching `a.b.example.com`), apex matching
  via wildcard, regex/glob host patterns, or longest-suffix precedence. Out of
  scope by decision; see "Deferred".
- TLS SNI routing, virtual-host certificate selection (that's `httpserver`'s
  `WithTLSConfig` / a future autocert concern).
- Any serializable/env-loadable configuration. There is deliberately no `Config`
  struct — a host→handler map is code.
- Mutable/after-construction route registration. The `Router` is immutable after
  `New` (this is what makes lock-free serving sound).

## Package & module

- Import path: `github.com/dmitrymomot/forge/hostrouter`, package `hostrouter`.
- Flat top-level layout alongside `httpserver`/`supervisor`. Stdlib only:
  `context`, `net/http`, `strings`, `errors`, `fmt`.
- File layout (mirrors `httpserver`):

  ```
  hostrouter/
    hostrouter.go     # Router, New, ServeHTTP, normalizeHost, splitFirstLabel, wildcardEntry
    options.go        # Option, WithHost, WithFallback, WithoutMatchContext
    context.go        # Match, matchCtx, key, FromContext, Subdomain, Pattern, Host
    errors.go         # sentinel errors (panic payloads)
    doc.go            # package doc + runnable Example
    hostrouter_test.go
    options_test.go
    context_test.go
    bench_test.go
  ```

## Public API

```go
// Router dispatches by Host header. Immutable after New; safe for concurrent use.
// Implements http.Handler.
type Router struct { /* unexported */ }

// New builds a Router from options applied in order. With no WithHost options the
// Router serves everything to the fallback. New panics on any invalid registration
// (see "Misconfiguration"). It does no I/O.
func New(opts ...Option) *Router

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request)

type Option func(*Router)

// WithHost registers handler h for pattern. pattern is either an exact host
// ("api.example.com") or a single-label wildcard ("*.example.com"). Panics if h is
// nil, the pattern is malformed, or the pattern is already registered.
func WithHost(pattern string, h http.Handler) Option

// WithFallback sets the handler for unmatched hosts. Default: http.NotFoundHandler()
// (404). Panics if h is nil. Last call wins.
func WithFallback(h http.Handler) Option

// WithoutMatchContext disables match-context injection for this Router. Matched
// requests are dispatched without the http.Request copy, making the matched path
// zero-allocation. FromContext / Subdomain / Pattern / Host then return zero values.
func WithoutMatchContext() Option

// Match describes how a request was routed. Exposed to the matched handler via the
// request context (unless WithoutMatchContext is set).
type Match struct {
    Host      string // normalized host that matched, e.g. "foo.example.com"
    Pattern   string // registered pattern, e.g. "*.example.com" or "api.example.com"
    Subdomain string // captured wildcard label ("foo"); "" for exact matches
}

// FromContext returns the Match injected by the Router. ok is false when there was
// no match (fallback handler) or injection was disabled.
func FromContext(ctx context.Context) (m Match, ok bool)

// Convenience accessors over FromContext; each returns "" when absent.
func Subdomain(ctx context.Context) string
func Pattern(ctx context.Context) string
func Host(ctx context.Context) string
```

### Internal `Router`

```go
type wildcardEntry struct {
    handler http.Handler
    pattern string // pre-built "*."+parent, computed once at registration
}

type Router struct {
    exact     map[string]http.Handler   // normalized host -> handler
    wildcard  map[string]wildcardEntry  // parent domain (no "*.") -> entry
    fallback  http.Handler
    injectCtx bool                      // default true; WithoutMatchContext clears it
}

func New(opts ...Option) *Router {
    r := &Router{
        exact:     make(map[string]http.Handler),
        wildcard:  make(map[string]wildcardEntry),
        fallback:  http.NotFoundHandler(),
        injectCtx: true,
    }
    for _, opt := range opts {
        opt(r) // panics on invalid registration
    }
    return r
}
```

## Matching & dispatch (`ServeHTTP`)

```go
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
    host := normalizeHost(req.Host)

    if h, ok := r.exact[host]; ok {
        r.serve(w, req, h, Match{Host: host, Pattern: host})
        return
    }
    if label, parent, ok := splitFirstLabel(host); ok {
        if e, found := r.wildcard[parent]; found {
            r.serve(w, req, e.handler, Match{Host: host, Pattern: e.pattern, Subdomain: label})
            return
        }
    }
    r.fallback.ServeHTTP(w, req) // no match: no context injected, original request
}

func (r *Router) serve(w http.ResponseWriter, req *http.Request, h http.Handler, m Match) {
    if r.injectCtx {
        req = req.WithContext(&matchCtx{Context: req.Context(), m: m})
    }
    h.ServeHTTP(w, req)
}
```

Precedence: **exact → single-label wildcard → fallback.** Exact always beats
wildcard. The single-label semantic falls out of "strip exactly one leading label,
look up the remainder":

- `foo.example.com` → label `foo`, parent `example.com` → matches `*.example.com`.
- `a.b.example.com` → label `a`, parent `b.example.com` → **no** wildcard key
  `b.example.com` → falls through (multi-level not matched).
- `example.com` (apex) → label `example`, parent `com` → no `*.com` key → falls
  through (apex not matched by `*.example.com`; register it exactly if needed).

`*.example.com` (wildcard, stored under key `example.com`) and `example.com`
(exact, stored under key `example.com` in the *exact* map) live in different maps,
so they coexist without a false duplicate.

### Host normalization

```go
// normalizeHost lowercases, strips any port, removes IPv6 brackets, and trims a
// trailing FQDN dot. Zero-allocation for an already-lowercase host (the norm); the
// only possible allocation is strings.ToLower's slow path on uppercase input.
func normalizeHost(host string) string {
    if host == "" {
        return ""
    }
    if host[0] == '[' { // IPv6 literal: "[::1]" or "[::1]:8080"
        if i := strings.IndexByte(host, ']'); i >= 0 {
            host = host[1:i] // inside brackets; drops "]" and any ":port"
        }
    } else if i := strings.LastIndexByte(host, ':'); i >= 0 &&
        strings.IndexByte(host, ':') == i {
        host = host[:i] // exactly one colon => host:port (not bracketless IPv6)
    }
    host = strings.TrimSuffix(host, ".") // rooted FQDN "example.com."
    return strings.ToLower(host)
}

// splitFirstLabel splits "foo.example.com" into ("foo", "example.com", true).
// Returns ok=false for no dot, a leading dot, or a trailing dot.
func splitFirstLabel(host string) (label, parent string, ok bool) {
    i := strings.IndexByte(host, '.')
    if i <= 0 || i == len(host)-1 {
        return "", "", false
    }
    return host[:i], host[i+1:], true
}
```

`net.SplitHostPort` is deliberately **not** used: it allocates an `*AddrError`
whenever the host has no port (common behind proxies / HTTP/2), which would be a
per-request allocation. The hand-rolled scan above allocates nothing. A bracketless
IPv6 with no port (`::1`, multiple colons) is left intact rather than mangled; a
bracketless IPv6 *with* a port is not a valid `Host` value and is not special-cased.

## Misconfiguration (panics)

Building a router with a bad route is a programming error caught at startup, so
`New` (via the option closures) **panics**, like `http.ServeMux.Handle`. Panic
payloads wrap single-line sentinels so tests can `recover()` + `errors.Is`:

```go
var (
    ErrNilHandler     = errors.New("hostrouter: nil handler")
    ErrInvalidPattern = errors.New("hostrouter: invalid host pattern")
    ErrDuplicateHost  = errors.New("hostrouter: duplicate host pattern")
)
```

`WithHost` panics when:

| Condition | Sentinel | Examples |
|---|---|---|
| `h == nil` | `ErrNilHandler` | `WithHost("x.com", nil)` |
| malformed pattern | `ErrInvalidPattern` | `""`, `"*."`, `"*"`, `"*.*.com"`, `"fo*.com"` |
| already registered in the same map | `ErrDuplicateHost` | two `WithHost("api.com", …)` |

```go
func WithHost(pattern string, h http.Handler) Option {
    return func(r *Router) {
        if h == nil {
            panic(fmt.Errorf("%w: %q", ErrNilHandler, pattern))
        }
        if strings.HasPrefix(pattern, "*.") {
            parent := normalizeHost(pattern[2:])
            if parent == "" || strings.ContainsRune(parent, '*') {
                panic(fmt.Errorf("%w: %q", ErrInvalidPattern, pattern))
            }
            if _, dup := r.wildcard[parent]; dup {
                panic(fmt.Errorf("%w: %q", ErrDuplicateHost, pattern))
            }
            r.wildcard[parent] = wildcardEntry{handler: h, pattern: "*." + parent}
            return
        }
        host := normalizeHost(pattern)
        if host == "" || strings.ContainsRune(host, '*') {
            panic(fmt.Errorf("%w: %q", ErrInvalidPattern, pattern))
        }
        if _, dup := r.exact[host]; dup {
            panic(fmt.Errorf("%w: %q", ErrDuplicateHost, pattern))
        }
        r.exact[host] = h
    }
}

func WithFallback(h http.Handler) Option {
    return func(r *Router) {
        if h == nil {
            panic(fmt.Errorf("%w: WithFallback handler", ErrNilHandler))
        }
        r.fallback = h
    }
}

func WithoutMatchContext() Option {
    return func(r *Router) { r.injectCtx = false }
}
```

Patterns are normalized at registration with the **same** `normalizeHost` used at
match time, so a registered pattern and an incoming host can never disagree on
casing, port, or trailing dot. Exact patterns containing a `*`, and wildcard
patterns whose parent contains a `*`, are rejected — `"*."` alone yields an empty
parent and is rejected too. An exact host and a wildcard sharing the same parent
(`example.com` + `*.example.com`) is **not** a duplicate (different maps).

## Match context (single-allocation injection)

The matched handler reads the `Match` from the request context. Rather than
`context.WithValue` (which allocates a `*valueCtx` node **and** boxes the value — two
allocations), a custom context type folds both into one allocation and returns the
same pointer on every read (no per-read allocation):

```go
type ctxKey struct{}

var matchKey = ctxKey{}

type matchCtx struct {
    context.Context // parent; promotes Deadline/Done/Err/Value-delegation
    m Match
}

func (c *matchCtx) Value(key any) any {
    if key == matchKey {
        return &c.m // interior pointer into the already-heap matchCtx; no new alloc
    }
    return c.Context.Value(key)
}

func FromContext(ctx context.Context) (Match, bool) {
    if m, ok := ctx.Value(matchKey).(*Match); ok {
        return *m, true // return a copy; callers cannot mutate the router's Match
    }
    return Match{}, false
}

func Subdomain(ctx context.Context) string { m, _ := FromContext(ctx); return m.Subdomain }
func Pattern(ctx context.Context) string   { m, _ := FromContext(ctx); return m.Pattern }
func Host(ctx context.Context) string      { m, _ := FromContext(ctx); return m.Host }
```

`Deadline`/`Done`/`Err` are promoted from the embedded parent, so the wrapped
context behaves exactly like its parent except for our one key. Downstream
middleware that wraps the context further still resolves the `Match` (the lookup
walks the chain to our `matchCtx`). `FromContext` returns a **value** copy so the
router's `Match` cannot be mutated through the returned pointer.

## Performance

The router is built to add the least possible overhead to every request.

- **Lock-free serving.** The `Router` is immutable after `New` — `ServeHTTP` only
  reads the maps. Go maps are safe for concurrent reads, so there is **no mutex and
  no `sync` primitive** on the serve path; throughput scales across cores.
- **Zero-allocation matching.** `normalizeHost` produces only sub-slices of
  `req.Host` plus a `strings.ToLower` that returns the input unchanged for an
  already-lowercase host (the common case). Map lookups on those sub-slice string
  keys don't allocate. `splitFirstLabel` is pure slicing. So **normalize + exact
  lookup + wildcard split + wildcard lookup = 0 allocations.**
- **No hot-path string building.** `Match.Host` and `Match.Subdomain` are sub-slices
  of `req.Host` (which outlives the request, so retaining them in the `Match` for
  the request's lifetime is safe). `Match.Pattern` for wildcards is the
  pre-built `"*."+parent` stored at registration — the hot path never concatenates.
- **Minimal dispatch allocations:**
  - **No match / fallback → 0 allocations** (the original `*http.Request` is passed
    straight through).
  - **Matched (default, injection on) → 2 allocations:** the mandatory
    `http.Request` shallow copy from `req.WithContext` (the only way to attach a
    context to a handler) plus the single `*matchCtx`. This is the floor for any
    context-carrying router.
  - **Matched + `WithoutMatchContext()` → 0 allocations:** pure dispatch, no request
    copy, for ultra-hot routes that don't read the accessors.
- **Map over slice.** Exact and wildcard lookups use maps (O(1), robust as route
  counts grow). For a handful of routes a linear slice scan could micro-win by
  avoiding the hash, but the map is the predictable default and is not the
  bottleneck; not optimizing prematurely.

A `bench_test.go` with `-benchmem` pins these guarantees so regressions surface in
review:

- `BenchmarkServeHTTP_Exact`, `_Wildcard`, `_Fallback` — assert the expected
  allocs/op (2 matched with injection, 0 matched with `WithoutMatchContext`, 0
  fallback).
- `BenchmarkNormalizeHost` over lowercase / with-port / IPv6 / uppercase inputs —
  assert 0 allocs/op except the uppercase slow path.
- A parallel benchmark (`b.RunParallel`) to confirm the absence of contention.

## Usage

```go
router := hostrouter.New(
    hostrouter.WithHost("api.example.com", apiMux),       // exact
    hostrouter.WithHost("*.example.com", tenantMux),      // wildcard
    hostrouter.WithFallback(marketingSite),               // optional; default 404
)

srv := httpserver.New(router, httpserver.WithAddr(":8080"))
_ = supervisor.Run(ctx, supervisor.WithService(srv))

// inside tenantMux's handler:
func handle(w http.ResponseWriter, r *http.Request) {
    tenant := hostrouter.Subdomain(r.Context()) // "foo" for foo.example.com
    // ...
}

// ultra-hot, accessors not needed:
fast := hostrouter.New(
    hostrouter.WithHost("static.cdn.example.com", assets),
    hostrouter.WithoutMatchContext(), // matched path is now zero-allocation
)
```

## Edge cases

- **`New()` with no `WithHost`** → every request goes to the fallback (404 by
  default). Valid, no panic.
- **Empty / missing `Host` header** → `normalizeHost("") == ""`, no exact or
  wildcard key matches `""` → fallback.
- **Host with port** (`example.com:8080`) → port stripped → matches `example.com`.
- **Uppercase host** (`API.Example.com`) → lowercased → matches `api.example.com`
  (the only path that may allocate, via `ToLower`'s slow path).
- **Rooted FQDN** (`example.com.`) → trailing dot trimmed → matches `example.com`.
- **IPv6** (`[::1]:8080`) → normalized to `::1`; register `WithHost("::1", h)` (note
  the registered pattern is normalized identically) to match it.
- **Apex vs wildcard** → `*.example.com` does **not** match `example.com`; add an
  exact `WithHost("example.com", …)` for the apex.
- **Multi-level host** (`a.b.example.com`) → not matched by `*.example.com`; falls
  through to fallback unless registered exactly.
- **Exact + wildcard same parent** (`example.com` and `*.example.com`) → both
  registered, no duplicate; `example.com` → exact handler, `foo.example.com` →
  wildcard handler.
- **`WithoutMatchContext()` then calling an accessor** → returns `""` / `Match{}`,
  `false` (documented).
- **Repeated `WithFallback`** → last one wins (not a route; no duplicate panic).

## Testing

White-box (`package hostrouter`) so tests can assert the internal maps and the
`injectCtx` flag without exporting them. `httptest`-driven; testify only (no other
third-party deps).

- **Routing matrix:** exact hit, wildcard single-label hit, exact-beats-wildcard
  precedence, multi-level non-match, apex non-match, empty-host → fallback, default
  404 fallback, custom fallback.
- **Normalization:** port stripping, case-insensitivity, IPv6 bracket/port, trailing
  FQDN dot; table-driven against `normalizeHost` and end-to-end via `ServeHTTP`.
- **Context:** `FromContext` / `Subdomain` / `Pattern` / `Host` return the right
  values for exact (empty `Subdomain`) and wildcard hits; all return zero values
  inside the fallback handler; downstream `context.WithValue` wrapping still resolves
  the `Match`.
- **`WithoutMatchContext`:** accessors return zero values; (benchmark) matched path
  is zero-allocation.
- **Panics (recover + `errors.Is`):** nil handler, nil fallback, each malformed
  pattern (`""`, `"*."`, `"*"`, `"*.*.com"`, `"fo*.com"`), duplicate exact, duplicate
  wildcard; and the non-cases that must *not* panic (exact + wildcard same parent;
  repeated `WithFallback`).
- **Benchmarks (`-benchmem`):** alloc-count assertions per the Performance section.
- **Integration:** wrap the router in `httpserver.New` and confirm a real request to
  a wildcard host routes and exposes the subdomain.

## Future fit

- The `Match` struct can gain fields (e.g. a port, or the raw unnormalized host)
  additively without breaking the accessors.
- If multi-tenant apps later need it, a multi-label wildcard could be added as a
  distinct pattern syntax with its own precedence tier below single-label — but only
  on demonstrated need.

## Deferred

- Multi-label wildcards, apex-matching wildcards, longest-suffix precedence,
  regex/glob host patterns.
- Path/method awareness (compose a per-host `http.ServeMux` instead).
- TLS SNI / per-host certificate selection.
- `sync.Pool`-based request-context reuse to reclaim the matched-path `http.Request`
  copy — only worthwhile if profiling shows it matters and the server cooperates;
  `WithoutMatchContext()` already covers the no-accessor case.
- Runtime/mutable route registration (would forfeit the lock-free property).
