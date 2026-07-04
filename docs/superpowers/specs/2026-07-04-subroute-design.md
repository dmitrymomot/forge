# web/subroute — design

Date: 2026-07-04
Status: approved for planning

## Purpose

`chi.Mount` ergonomics for the standard library `*http.ServeMux`: mount any
`http.Handler` (typically another `ServeMux`) under a path prefix, with the
prefix stripped so the mounted handler sees root-relative paths. Standalone
package in the `web/` domain, stdlib-only, in the same spirit as
`web/hostrouter`.

## API

One exported function, nothing else:

```go
package subroute

// Mount registers h on mux at prefix, both as the exact path and as a
// subtree. Requests are dispatched to h with prefix stripped from the URL
// path; a request for the bare prefix reaches h with path "/".
func Mount(mux *http.ServeMux, prefix string, h http.Handler)
```

Usage:

```go
mux := http.NewServeMux()
subroute.Mount(mux, "/admin", middleware.Wrap(adminMux, requireAuth))
subroute.Mount(mux, "/api/v1", apiMux)
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
- Trims `prefix` from `URL.Path`; an empty result is normalized to `"/"`, so
  `GET /admin` reaches the mounted handler as `GET /`. (Plain
  `http.StripPrefix` cannot do this: it leaves the path empty, and a nested
  `ServeMux` then 301-redirects to `/`.)
- `URL.RawPath`: trim the prefix if present; otherwise clear `RawPath` so
  `EscapedPath()` recomputes consistently.
- Query string, method, host, headers, and body are untouched.
- Composes under `web/hostrouter`, and nests: a mounted `ServeMux` can itself
  have `Mount` applied.

### Validation (setup-time panics)

Matching `hostrouter`'s panic-on-invalid-registration convention, `Mount`
panics with a descriptive message on:

- nil `mux` or nil `h`
- prefix not starting with `/`
- prefix `"/"` (use `mux.Handle("/", h)` directly)
- trailing slash in prefix
- ServeMux pattern metacharacters in prefix: `{`, `}`, or any space
  (prevents smuggling method/host patterns such as `"GET /admin"` or
  wildcards)

Multi-segment prefixes such as `/api/v1` are valid. There are no runtime
errors: after setup it is pure routing.

## Middleware composition (documentation concern)

`Mount` deliberately takes no middleware parameters; there is one canonical
way, via `web/middleware`:

- Per-mount: `subroute.Mount(mux, "/admin", middleware.Wrap(adminMux, mws...))`
  — runs only for requests under `/admin`.
- Global: `srv.Handler = middleware.Wrap(mux, reqlog, recoverer)` — wraps the
  outer mux, so it runs for every request including mounted subtrees.

Caveat to document: middleware wrapped inside the mount sees the **stripped**
path (`/users`), while middleware wrapping the outer mux sees the full
original path (`/admin/users`). `doc.go` and `example_test.go` must show both
patterns and the caveat.

## Testing

Black-box only (`package subroute_test`):

- bare prefix `GET /admin` → mounted handler sees `GET /`
- subtree paths stripped (`/admin/users` → `/users`)
- nested mounts (mount inside a mounted mux)
- query string preserved; `RawPath`/encoded segments handled
- non-matching paths fall through to the outer mux (404 or sibling routes)
- Go 1.22+ method patterns work inside the mounted mux
- middleware ordering across the mount boundary (outer sees full path, inner
  sees stripped path)
- caller's request not mutated
- all panic cases

Package layout mirrors `web/hostrouter`: `subroute.go`, `doc.go`,
`example_test.go`, tests, and a small benchmark.

## Non-goals

- No router type, no options, no `Use()` — one function.
- No middleware parameters on `Mount` (compose via `middleware.Wrap`).
- No pattern features in the prefix (methods, hosts, wildcards).
- No handler-returning variant (`subroute.At`) unless a real need appears.
