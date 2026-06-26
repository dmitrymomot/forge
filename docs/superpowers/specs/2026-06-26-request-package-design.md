# Design: `request` — HTTP request-reading helpers (free functions)

- **Date:** 2026-06-26
- **Status:** Draft for review
- **Scope:** A new standalone `request` package of stateless, reflection-free helpers
  that read data off an `*http.Request` into Go values — typed part accessors
  (`Query`/`Path`/`Header`/`Cookie`/`FormValue`, their `…Func`/`…Slice`/`…Split`
  variants), strict body decoding (`DecodeJSON`, `RawBody`, multipart `File`/`Files`),
  and a handful of focused readers (`BearerToken`, `ClientIP`, presence predicates,
  `QueryPage`/`QueryCursor`). No constructor, no options object except where there is
  real policy to configure (`DecodeJSON`/`RawBody`/`ClientIP`/pagination), no global
  state, no struct-tag binding, no reflection. It is the input-side counterpart to
  `render`; stdlib only; the rest of the framework is untouched.

## Overview

`request` is a thin, opinion-light layer over `*http.Request`. Each accessor pulls one
named value out of one part of the request and converts it to the Go type the caller
asks for, so handlers stop hand-repeating
`strconv.Atoi(r.URL.Query().Get("page"))` and the fiddly parts (transactional strict
JSON decoding, `Content-Type`/size guards, `TextUnmarshaler` parsing, real-client-IP
resolution) get one consistent, tested place.

It is deliberately **free functions, not a binder or a request-scoped wrapper type**.
There is nothing to hold and nothing to register — the handler owns its `r` and calls
a function. This matches the framework's "no magic" stance and composes with stdlib
handlers, `render`, and `htmx` directly:

```go
mux.HandleFunc("POST /orgs/{org}/reports", func(w http.ResponseWriter, r *http.Request) {
	org, _ := request.Path[uuid.UUID](r, "org")            // TextUnmarshaler — for free
	page, err := request.Query[int](r, "page", 1)          // default 1 when absent
	if err != nil {
		render.JSON(w, request.StatusCode(err), apiErr{err.Error()}) // 400
		return
	}
	tags, _ := request.QuerySplit[string](r, "filter", ",") // ?filter=orange,blue,gray

	var body createReport
	if err := request.DecodeJSON(r, &body); err != nil {    // 413 / 415 / 400 by Kind
		render.JSON(w, request.StatusCode(err), apiErr{err.Error()})
		return
	}
	// org, page, tags, body are ready; the handler owns the rest.
})
```

**Why a package and not just `strconv` + `r.URL.Query().Get`?** Things stdlib makes you
re-solve at every call site: (1) **typed conversion** with a uniform missing-vs-malformed
contract across query/path/header/cookie/form; (2) **custom-type parsing** without
reflection (`uuid.UUID`, `netip.Addr`, custom enums via `encoding.TextUnmarshaler`);
(3) **strict, transactional JSON decoding** (size cap → `413`, wrong `Content-Type` →
`415`, unknown field / trailing data → `400`); (4) **real-client-IP resolution** across
the CDN/proxy headers, with a clear spoofing caveat; (5) the small but real risk of
**typos and inconsistent error handling** in repeated `strconv` plumbing. `request`
solves each once, with stdlib only.

**Relationship to `render` and `htmx` (one-way, and standalone).** `request` reads;
`render` writes; `htmx` handles `HX-*` headers. `request` imports none of them and
none of them import `request`. A handler typically uses `request` to read input and
`render` to write output, and the typed `*request.Error` plus `request.StatusCode`
hand the right status straight to a `render.JSON` error body — but the three packages
stay fully independent and any one can be used alone.

## The central contract: missing vs malformed

This is the behavioral spine of the accessor family, decided during design.

- **Single accessor, zero on missing.** `Query[T](r, key) (T, error)` returns the
  **zero value of `T` and a nil error** when the key is absent, and returns
  `(zero, *Error{Kind: Malformed})` only when the key is **present but unparseable**.
  There is no separate "required" function; absence is not an error. The caller decides
  whether absence matters.

- **Optional default via variadic.** `Query[int](r, "page", 1)` returns `1` when the
  key is absent. Only the first `def` is consulted; any further values are ignored
  (documented). The default is used **only** for absence — a present-but-malformed
  value still returns an error, never the default, so bad client input is never
  silently swallowed.

- **Presence is a separate question.** Because zero and absent collapse to the same
  return when no default is given, presence predicates (`HasQuery`, …) exist to
  distinguish `?count=0` from "count absent" when that distinction matters. This keeps
  the common call site a clean two-value unpack while still answering "was it there?"
  on demand.

Rule of thumb: **absent ⇒ zero/default, nil error; present-but-bad ⇒ zero, typed
error.**

## Goals

- One stateless free function per (source, shape); consistent signature shape
  `(r, key, def …) (T, error)` for the typed accessors.
- Reflection-free in package code: built-in scalars via a type switch,
  `encoding.TextUnmarshaler` for custom types, and `…Func` variants for a
  caller-supplied parser. No `reflect`, no struct tags, no `init()`.
- Strict, transactional `DecodeJSON` (size/`Content-Type`/unknown-field/trailing-data
  guards) with functional options to relax per endpoint.
- A typed `*Error` carrying `Source`, `Key`, and `Kind`, plus a `StatusCode` mapper, so
  handlers map failures to `400`/`413`/`415` and name the offending field.
- Focused convenience readers that are otherwise re-written everywhere: `BearerToken`,
  `RawBody`, `ClientIP` (real-IP across CDN/proxy headers), page/cursor pagination.
- Stdlib only; testify in tests only; black-box tests (`package request_test`).

## Non-goals

- **No validation rules engine.** No required/min/max/regex DSL and no field-error
  collection. Absence and range are the handler's call; `*Error` already exposes
  `Source`+`Key` if the caller wants to build a field-keyed response. (Validation may
  be a future sibling package, not part of `request`.)
- **No struct binding / reflection / tags.** Accessors only; whole structs arrive via
  `DecodeJSON`. There is no `Bind(r, &dto)` pulling from multiple sources by tag.
- **No content negotiation.** There is no `Accept`-driven decoder selection.
  `DecodeJSON` is JSON; `IsContentType` inspects the request's own `Content-Type`, not
  `Accept`. (`DecodeXML` may be added later as a peer free function; it is not in this
  scope.)
- **No query/form merge.** `Query` reads the URL query; `FormValue` reads the body
  form. They stay separate (stdlib's `r.FormValue` merges the two; `request` does not),
  so the caller is explicit about which it wants.
- **No signed/encrypted cookies, CSRF, or sessions.** Those are a separate
  security/`cookie` sibling package; `request` only reads the raw cookie value.
- **No global state, middleware, or context plumbing.** Every function is a pure
  function of its arguments.

## Package & module

- Import path: `github.com/dmitrymomot/forge/request`, package `request`.
- Flat top-level layout alongside `httpserver`/`hostrouter`/`render`/`htmx`.
- Stdlib only: `encoding`, `encoding/json`, `errors`, `fmt`, `mime`, `mime/multipart`,
  `net/http`, `net/netip`, `strconv`, `strings`, `time`. testify in tests only.
- No constructor and no package-level mutable state. Functional options exist only for
  `DecodeJSON`, `RawBody`, `File`/`Files`, `ClientIP`, and the pagination readers.

### Files (one concern each)

| File | Contents |
|---|---|
| `parse.go` | the private generic `parse[T]` conversion engine |
| `query.go` | `Query`, `QueryFunc`, `QuerySlice`/`Func`, `QuerySplit`/`Func`, `HasQuery` |
| `path.go` | `Path`, `PathFunc`, `HasPath` |
| `header.go` | `Header`, `HeaderFunc`, `HasHeader`, `BearerToken` |
| `cookie.go` | `Cookie`, `CookieFunc`, `HasCookie` |
| `form.go` | `FormValue`, `FormValueFunc`, `FormSlice`/`Func`, `FormSplit`/`Func`, `HasForm` |
| `body.go` | `DecodeJSON`, `RawBody`, `File`, `Files`, `IsContentType`, the `Option` type and option funcs |
| `clientip.go` | `ClientIP` and its options |
| `pagination.go` | `Page`, `Cursor`, `QueryPage`, `QueryCursor` and their options |
| `errors.go` | `Source`, `Kind`, `*Error`, `StatusCode` |
| `doc.go` | package documentation |

## The generic core

Every typed accessor funnels through one private function:

```go
// parse converts a single raw string into T. Resolution order:
//   1. built-in scalars via a type switch on any(&v): *string, *bool,
//      *int/int8/16/32/64, *uint/uint8/16/32/64, *float32/64;
//   2. *time.Duration via time.ParseDuration;
//   3. any type whose pointer implements encoding.TextUnmarshaler —
//      time.Time (RFC 3339), uuid.UUID, netip.Addr, custom enums.
// No reflect package: interface assertions only. Returns the parse cause on
// failure; the calling accessor wraps it in *Error with Source/Key.
func parse[T any](raw string) (T, error)
```

`time.Time` needs no special case — `*time.Time` already implements
`encoding.TextUnmarshaler` (RFC 3339), so it falls out of branch 3. The `…Func`
variants bypass `parse` entirely and call the caller's `func(string) (T, error)`, so
genuinely exotic types never touch the engine.

## The accessor API

### Scalar accessors (one family per source)

```go
func Query[T any]    (r *http.Request, key string, def ...T) (T, error)
func Path[T any]     (r *http.Request, key string, def ...T) (T, error)
func Header[T any]   (r *http.Request, key string, def ...T) (T, error)
func Cookie[T any]   (r *http.Request, key string, def ...T) (T, error)
func FormValue[T any](r *http.Request, key string, def ...T) (T, error)
```

Semantics for every family: absent key → `def[0]` if supplied, else the zero value of
`T`, with a nil error; present-but-unparseable → `(zero, *Error{Kind: Malformed})`.

### `…Func` escape-hatch variants

Same five sources, caller supplies the parser; the built-in engine is bypassed:

```go
func QueryFunc[T any](r *http.Request, key string, parse func(string) (T, error), def ...T) (T, error)
// PathFunc, HeaderFunc, CookieFunc, FormValueFunc — identical shape
```

### Slice accessors — two distinct shapes

**Repeated keys** (`?id=1&id=2&id=3`), for the two sources where repetition is
idiomatic:

```go
func QuerySlice[T any](r *http.Request, key string, def ...[]T) ([]T, error)
func FormSlice[T any] (r *http.Request, key string, def ...[]T) ([]T, error)
```

**Single delimited value** (`?filter=orange,blue,gray`):

```go
func QuerySplit[T any](r *http.Request, key, sep string, def ...[]T) ([]T, error)
func FormSplit[T any] (r *http.Request, key, sep string, def ...[]T) ([]T, error)
```

`…Split` behavior: split the single value on the **explicit** `sep` (`","`, `" "`,
`"|"`, …; no hidden default), `strings.TrimSpace` each element, **skip empty elements**
(so `orange, blue, gray` and a trailing `a,b,` behave sensibly), then run each element
through `parse[T]`. Absent key → default (or nil slice) + nil error; any element
unparseable → `*Error{Kind: Malformed, Key: key}` naming the offending value.

Both slice shapes get `…Func` peers (`QuerySliceFunc`, `QuerySplitFunc`, `FormSliceFunc`,
`FormSplitFunc`) for custom element parsers.

### Presence predicates

```go
func HasQuery(r *http.Request, key string) bool
func HasPath(r *http.Request, key string) bool
func HasHeader(r *http.Request, key string) bool
func HasCookie(r *http.Request, key string) bool
func HasForm(r *http.Request, key string) bool
```

These close the "zero vs absent" gap left by the zero-on-missing contract: a caller
that must distinguish `?count=0` from a missing `count` checks `HasQuery` first.

### Per-source lookup mechanics

| Source | Backing read | Notes |
|---|---|---|
| `Query` | `r.URL.Query().Get` / `[key]` for slices | parses the raw query string per call |
| `Path` | `r.PathValue(key)` | needs a Go 1.22+ `ServeMux` pattern; no slice form |
| `Header` | `r.Header.Get(key)` | canonicalized key |
| `Cookie` | `r.Cookie(key)`; `http.ErrNoCookie` → absent | uses `cookie.Value` |
| `FormValue` | `r.PostFormValue(key)`; `…Slice` uses `r.PostForm[key]` | body form only (POST/PUT/PATCH); triggers stdlib form parsing |

`FormValue` reads the **body** form, not the query string, so query and form stay
distinct — to read either, the caller calls both explicitly.

## Body decoding & multipart

### `DecodeJSON`

```go
func DecodeJSON(r *http.Request, dst any, opts ...Option) error
```

Strict-by-default policy, each guard independently overridable:

| Guard | Default | On violation |
|---|---|---|
| Body size | `http.MaxBytesReader`, **1 MiB** | `*Error{Kind: TooLarge}` → `413` |
| Content-Type | require `application/json` (params allowed) | `*Error{Kind: UnsupportedMediaType}` → `415` |
| Unknown fields | `DisallowUnknownFields` | `*Error{Kind: InvalidBody}` → `400` |
| Trailing data | reject anything after the first JSON value | `*Error{Kind: InvalidBody}` → `400` |
| Empty body | rejected (EOF) | `*Error{Kind: InvalidBody}` → `400` |

`Content-Type` is matched with `mime.ParseMediaType` (robust to casing/params). The
too-large case is detected via `errors.As` on `*http.MaxBytesError`, so it maps to
`413` rather than a generic `400`.

### `RawBody`

```go
func RawBody(r *http.Request, opts ...Option) ([]byte, error)
```

Reads the full body into a byte slice under the **same** `MaxBytes` cap as `DecodeJSON`
(`WithMaxBytes` applies; overflow → `*Error{Kind: TooLarge}` → `413`). For webhook HMAC
verification and arbitrary payloads — the one body shape `DecodeJSON` can't return. No
`Content-Type` check by default.

### Multipart files

```go
func File(r *http.Request, key string, opts ...Option) (multipart.File, *multipart.FileHeader, error)
func Files(r *http.Request, key string, opts ...Option) ([]*multipart.FileHeader, error)
```

`File` returns the first uploaded file for `key` (caller `defer`s `Close()`); `Files`
returns every header for repeated file inputs (open lazily via `fh.Open()`). Both
trigger `r.ParseMultipartForm`; `WithMaxBytes` caps the in-memory portion (stdlib
default 32 MiB spills to temp files otherwise). A missing file → `*Error{Kind:
Malformed}`; a non-multipart request → `*Error{Kind: UnsupportedMediaType}` → `415`.
Multipart **field** values are already covered by `FormValue`/`FormSlice`, so
`File`/`Files` handle only the file parts — no overlap.

### `IsContentType`

```go
func IsContentType(r *http.Request, media string) bool
```

Reports whether the request's `Content-Type` media type equals `media`, compared via
`mime.ParseMediaType` (case-insensitive, parameters like `; charset=utf-8` ignored) —
the same matcher `DecodeJSON` uses. A lightweight predicate for branching on body
format (e.g. `if request.IsContentType(r, "application/json")`); it is **not** content
negotiation — it inspects the request's own `Content-Type`, never the `Accept` header.

### Options

There is **one** `Option` type and one internal `config`, shared by every
option-taking function (`DecodeJSON`, `RawBody`, `File`/`Files`, `ClientIP`,
`QueryPage`, `QueryCursor`). Each such function starts from its own per-function
defaults, applies the supplied options, and reads only the fields it cares about, so an
option that doesn't apply to a given function (e.g. `AllowUnknownFields` on `RawBody`)
is a harmless no-op. The accessor families (`Query`, `Path`, …) take no options.

```go
type Option func(*config)

// Body (DecodeJSON / RawBody / File / Files)
func WithMaxBytes(n int64) Option   // raise/lower the cap; n <= 0 disables the limit
func AllowUnknownFields() Option    // turn off DisallowUnknownFields (DecodeJSON)
func SkipContentType() Option       // accept any/absent Content-Type (DecodeJSON)

// ClientIP
func WithClientIPHeaders(names ...string) Option
func WithTrustedProxies(prefixes ...netip.Prefix) Option

// Pagination
func WithPageParams(pageKey, sizeKey string) Option
func WithDefaultPageSize(n int) Option
func WithMaxPageSize(n int) Option
func WithCursorParams(cursorKey, limitKey string) Option
```

Functional options, no builder (per project convention).

## `ClientIP` — real-client-IP resolution

```go
func ClientIP(r *http.Request, opts ...Option) string   // "" if nothing parses
```

**Default (best-effort):** scan well-known headers in priority order, take the first
value that parses as an IP (`netip.ParseAddr`), else fall back to the host of
`r.RemoteAddr`:

`CF-Connecting-IP` → `True-Client-IP` → `Fastly-Client-IP` → `X-Real-IP` →
`Forwarded` (RFC 7239 `for=`) → `X-Forwarded-For` (first valid) → `RemoteAddr`.

**Options:**

```go
func WithClientIPHeaders(names ...string) Option        // replace the priority list
func WithTrustedProxies(prefixes ...netip.Prefix) Option // correct XFF resolution
```

- `WithClientIPHeaders` is the **secure pattern for a known CDN**: pin to exactly the
  one header your edge always sets and strips on ingress (e.g.
  `WithClientIPHeaders("CF-Connecting-IP")`), so a client can't forge it.
- `WithTrustedProxies` switches to spoof-resistant `X-Forwarded-For` resolution: walk
  the chain right-to-left, skip trusted hops, return the first **untrusted** address.

**Security note (stated plainly in the doc):** the default best-effort mode trusts
client-supplied headers and is **spoofable** unless the service sits behind a proxy
that overwrites them. Do not use the raw result for auth or rate-limiting without
`WithTrustedProxies` or a pinned header. CDN IP ranges are not hardcoded (they change);
that allowlist is the caller's to supply.

## Pagination

```go
type Page struct {
	Number int // 1-based page number
	Size   int // page size
	Offset int // derived: (Number - 1) * Size
}
func QueryPage(r *http.Request, opts ...Option) (Page, error)

type Cursor struct {
	Value string // opaque token, passed through verbatim
	Limit int
}
func QueryCursor(r *http.Request, opts ...Option) (Cursor, error)
```

- `QueryPage` reads `page` + `per_page`; defaults: page 1, size 20. Options:
  `WithPageParams(pageKey, sizeKey)`, `WithDefaultPageSize(n)`, `WithMaxPageSize(n)`.
- `QueryCursor` reads `cursor` + `limit` (param names via `WithCursorParams`); the
  cursor stays an **opaque string** — encoding/decoding its payload is the app's job (a
  codec would be a sibling concern). `limit` shares the same default/max bounds as page
  size (`WithDefaultPageSize`/`WithMaxPageSize`).
- **Behavior split:** a *non-numeric* `page`/`size`/`limit` → `*Error{Kind: Malformed}`
  → `400`. A *valid but out-of-range* value (page < 1, size > max) is **clamped** to the
  bound — the conventional, less hostile pagination behavior — rather than rejected.

## Error model

```go
type Source string
const (
	SourceQuery  Source = "query"
	SourcePath   Source = "path"
	SourceHeader Source = "header"
	SourceCookie Source = "cookie"
	SourceForm   Source = "form"
	SourceBody   Source = "body"
)

type Kind int
const (
	KindMalformed            Kind = iota // unparseable value           → 400
	KindTooLarge                         // body exceeded the size cap   → 413
	KindUnsupportedMediaType             // wrong/absent Content-Type    → 415
	KindInvalidBody                      // malformed/unknown-field JSON → 400
)
func (k Kind) String() string

type Error struct {
	Source Source // which part of the request
	Key    string // param/field/cookie name ("" for whole-body errors)
	Kind   Kind
	Err    error  // wrapped cause (strconv error, json error, *MaxBytesError, …)
}
func (e *Error) Error() string  // single line, e.g. `request: query "page": malformed: ...`
func (e *Error) Unwrap() error  // returns Err — errors.Is/As reach the cause

// StatusCode reports the HTTP status for err: a *request.Error maps by Kind
// (400/413/415); any other non-nil error is 400; nil is 200.
func StatusCode(err error) int
```

Design notes:

- **One `*Error` type, not a tree.** `errors.As(err, &reqErr)` then switch on
  `reqErr.Kind`. `Source`+`Key` are right there for building a field-keyed response.
- **Single-line `Error()` strings** (no embedded blobs/stacks), matching the project's
  structured-logging rule. The wrapped `Err` stays reachable via `Unwrap` for
  `errors.Is`/`errors.As`.
- A handler stays three lines and never re-derives status logic:

  ```go
  page, err := request.Query[int](r, "page", 1)
  if err != nil {
  	render.JSON(w, request.StatusCode(err), apiErr{Message: err.Error()})
  	return
  }
  ```

## Data flow

- **Accessor:** `accessor → raw string from r → parse[T] (scalar switch → Duration →
  TextUnmarshaler, or the Func) → (value, nil) | (zero, *Error{Source, Key, Kind})`.
- **`DecodeJSON`:** `MaxBytesReader → Content-Type check → strict json.Decoder → reject
  trailing → (nil | *Error)`.
- No shared state between calls; each function is independent and idempotent.

## Testing

Black-box only (`package request_test`, per project convention; testify for assertions):

- Per source × type matrix: present/valid, present/malformed (→ `*Error` with the right
  `Kind`/`Source`/`Key`), absent (→ zero), absent-with-default, `…Func`, `…Slice`
  (repeated keys), `…Split` (delimited: separator, trim, skipped empties).
- `TextUnmarshaler` path via a tiny local custom type (no external dependency) plus
  `time.Time` and `time.Duration`.
- `DecodeJSON`: happy path, oversize → 413, wrong/missing `Content-Type` → 415, unknown
  field → 400, trailing data → 400, empty → 400, and each option
  (`WithMaxBytes`/`AllowUnknownFields`/`SkipContentType`) flipping the outcome.
- `RawBody`: happy path and oversize → 413.
- `IsContentType`: exact match, case/parameter insensitivity, mismatch, absent header.
- Multipart `File`/`Files`: present, absent, non-multipart → 415, in-memory cap.
- `ClientIP`: header priority order, first-valid selection, `WithClientIPHeaders`
  pinning, `WithTrustedProxies` right-to-left resolution, `RemoteAddr` fallback,
  unparseable candidates skipped.
- `QueryPage`/`QueryCursor`: defaults, custom param names, clamping vs malformed error,
  opaque cursor passthrough.
- Presence predicates across all sources; `StatusCode` mapping table; `errors.Is`/`As`
  reach the wrapped cause.
- `example_test.go` (compiles, doubles as documentation); allocation-sensitive
  benchmarks for the `parse` hot path.

## Future fit

- `DecodeXML` (`encoding/xml`) can be added later as a peer body decoder without
  touching the existing functions.
- A validation sibling package can consume `*Error`'s `Source`/`Key` to assemble
  field-keyed error responses, without `request` itself growing a rules engine.
