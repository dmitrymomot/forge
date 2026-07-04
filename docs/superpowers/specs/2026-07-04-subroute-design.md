# web/subroute — design

Date: 2026-07-04
Status: approved for planning

## Purpose

`chi.Mount` semantics for the standard library `*http.ServeMux`: mount any
`http.Handler` (typically another `ServeMux`) under a path prefix — including
prefixes with `{name}` wildcards — with the prefix stripped so the mounted
handler sees root-relative paths, and prefix path values still readable from
handlers inside the mount. Standalone package in the `web/` domain,
stdlib-only, in the same spirit as `web/hostrouter`.

## Background: why not exactly chi's mechanism

chi's `Mount` never rewrites `r.URL.Path`. Parent and child chi routers share
one `RouteContext`: the mount handler shifts `rctx.RoutePath` and the child
routes on that, while `rctx.URLParams` accumulates across the chain — so
parent params remain readable in mounted subrouters.

`http.ServeMux` has no such seam: it routes on `r.URL.Path` only, and its
match state (what `r.PathValue` reads) is unexported and overwritten whenever
a nested mux matches. Therefore a std-mux mount must (a) rewrite the path on
a cloned request, and (b) carry prefix params through its own request-context
value — the explicit-accessor equivalent of chi's `RouteContext`.

## API

Two exported functions:

```go
package subroute

// Mount registers h on mux at prefix, both as the exact path and as a
// subtree. prefix may contain single-segment {name} wildcards. Requests are
// dispatched to h with the prefix segments stripped from the URL path; a
// request for the bare prefix reaches h with path "/". Path values matched
// by the prefix are captured and readable inside h via PathValue.
func Mount(mux *http.ServeMux, prefix string, h http.Handler)

// PathValue returns the named path value: r.PathValue first (the current
// mux's own match), then values captured by enclosing Mount prefixes,
// innermost mount first. Returns "" if the name is not present.
func PathValue(r *http.Request, name string) string
```

Usage:

```go
mux := http.NewServeMux()
subroute.Mount(mux, "/admin", middleware.Wrap(adminMux, requireAuth))
subroute.Mount(mux, "/app/{tenant}/dashboard", middleware.Wrap(dashboardMux, mws...))

// dashboardMux routes are root-relative:
dashboardMux.HandleFunc("GET /{$}", home)             // GET /app/acme/dashboard
dashboardMux.HandleFunc("GET /reports/{id}", report)  // GET /app/acme/dashboard/reports/7

func report(w http.ResponseWriter, r *http.Request) {
    tenant := subroute.PathValue(r, "tenant") // "acme" — from the mount prefix
    id := r.PathValue("id")                   // "7" — current mux match, stdlib as usual
}
```

## Behavior

### Registration

`Mount` registers two patterns on the given mux — the exact `prefix` and the
subtree `prefix + "/"` — both pointing at one internal strip handler.
Duplicate-registration conflicts are caught by `ServeMux.Handle` itself,
which already panics; `subroute` inherits that behavior.

### Strip handler (hot path)

- Shallow-clones the request and its URL; the caller's request is never
  mutated.
- Captures prefix path values: for each `{name}` wildcard in the prefix
  (parsed once at Mount time), reads `r.PathValue(name)` — the outer mux has
  just matched them — and stores the set in the request context. Nested
  mounts merge with enclosing captures; on name collision the innermost mount
  wins (matching chi's last-added-wins scan). Static prefixes skip capture
  entirely (no context allocation).
- Strips as many leading path segments as the prefix has (segment-based, not
  literal trim, because wildcard segments vary per request). Operates on the
  escaped path; `URL.Path` and `URL.RawPath` are set consistently. An empty
  result is normalized to `"/"`, so a bare-prefix request reaches h as
  `GET /`. (Plain `http.StripPrefix` cannot do this: it leaves the path
  empty, and a nested `ServeMux` then 301-redirects to `/`.)
- Query string, method, host, headers, and body are untouched.
- Composes under `web/hostrouter`, and nests: a mounted `ServeMux` can itself
  have `Mount` applied.

### PathValue accessor

`subroute.PathValue(r, name)`:

1. `r.PathValue(name)` — non-empty means the current mux's own pattern
   matched it; return it.
2. Otherwise walk mount-captured values from the innermost mount outward.
3. `""` if absent.

Handlers keep using plain `r.PathValue` for their own mux's wildcards;
`subroute.PathValue` is needed only for params originating in mount prefixes
(stdlib physically cannot propagate those across a mux boundary).

### Validation (setup-time panics)

Matching `hostrouter`'s panic-on-invalid-registration convention, `Mount`
panics with a descriptive message on:

- nil `mux` or nil `h`
- prefix not starting with `/`
- prefix `"/"` (use `mux.Handle("/", h)` directly)
- trailing slash in prefix
- malformed wildcards: `{` or `}` not forming a full-segment `{name}`,
  empty name, `{$}`, or multi-segment `{name...}`
- duplicate wildcard names within one prefix
- spaces in prefix (prevents smuggling method/host patterns such as
  `"GET /admin"`)

Static and wildcard segments may mix freely: `/api/v1`,
`/app/{tenant}/dashboard`, `/{org}/{repo}/settings` are all valid. There are
no runtime errors: after setup it is pure routing.

## Middleware composition (documentation concern)

`Mount` deliberately takes no middleware parameters; there is one canonical
way, via `web/middleware`:

- Per-mount: `subroute.Mount(mux, "/admin", middleware.Wrap(adminMux, mws...))`
  — runs only for requests under `/admin`.
- Global: `srv.Handler = middleware.Wrap(mux, reqlog, recoverer)` — wraps the
  outer mux, so it runs for every request including mounted subtrees.

Caveats to document in `doc.go` and `example_test.go`:

- Middleware wrapped inside the mount sees the **stripped** path (`/users`),
  while middleware wrapping the outer mux sees the full original path
  (`/admin/users`). This differs from chi, where mounted middleware sees the
  full path (chi never rewrites the URL); it is unavoidable with a std mux
  as the child router.
- Mount-prefix params are read with `subroute.PathValue`, not `r.PathValue`.

## Testing

Black-box only (`package subroute_test`):

- bare prefix `GET /admin` → mounted handler sees `GET /`
- subtree paths stripped (`/admin/users` → `/users`)
- wildcard prefix: `GET /app/acme/dashboard/reports/7` → handler sees
  `/reports/7`, `subroute.PathValue(r, "tenant") == "acme"`,
  `r.PathValue("id") == "7"`
- bare wildcard prefix (`GET /app/acme/dashboard` → path `/`, tenant
  captured)
- nested mounts: param merging, innermost-wins shadowing, static-inside-
  wildcard and wildcard-inside-wildcard
- PathValue precedence: current-mux match beats captured mount values
- query string preserved; escaped/encoded segments handled consistently in
  `Path`/`RawPath`
- non-matching paths fall through to the outer mux (404 or sibling routes)
- Go 1.22+ method patterns work inside the mounted mux
- middleware ordering across the mount boundary (outer sees full path, inner
  sees stripped path)
- caller's request not mutated
- static-prefix mounts allocate no capture context
- all panic cases

Package layout mirrors `web/hostrouter`: `subroute.go`, `context.go`,
`doc.go`, `example_test.go`, tests, and a small benchmark.

## Non-goals

- No router type, no options, no `Use()` — two functions.
- No middleware parameters on `Mount` (compose via `middleware.Wrap`).
- No method, host, `{$}`, or `{name...}` elements in the prefix.
- No handler-returning variant (`subroute.At`) unless a real need appears.
- No propagation of the parent mux's 404/405 handlers into the child
  (chi does this for chi-to-chi mounts; std `ServeMux` has no seam for it).
