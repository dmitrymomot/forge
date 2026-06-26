# Design: `render` — HTTP response-writing helpers (free functions)

- **Date:** 2026-06-26
- **Status:** Draft for review
- **Scope:** A new standalone `render` package of stateless free functions that write
  an HTTP response from data a handler already holds — `JSON`/`JSONStream`, `HTML`
  (`html/template`), `Templ` (a-h/templ, via a structural interface — **no
  dependency**), `Text`, `Blob`, `CSV`, `Stream`, `Attachment`, `File`/`FileFS`,
  `Redirect`, `NoContent`. No constructor, no options, no global state. Plain
  `net/http` handlers; nothing else in the framework is modified.

## Overview

`render` is a thin, opinion-light layer over `http.ResponseWriter`: each function
sets the right `Content-Type`, writes the status once, encodes/streams the body, and
returns the write/encode error for the caller to log. It exists so handlers stop
hand-repeating `w.Header().Set(...) / w.WriteHeader(...) / json.NewEncoder(w)...` and
get one consistent, tested place for the fiddly parts (transactional encoding,
`Content-Disposition` filename quoting, templ without a dependency).

It is deliberately **free functions, not a configured renderer**. There is no parsed
template set to hold and no error hook to register — the caller owns its
`*template.Template`, and the error comes back as a return value. This matches the
framework's "no magic" stance and composes with stdlib handlers directly:

```go
mux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
    u, err := store.User(r.Context(), r.PathValue("id"))
    if err != nil {
        _ = render.JSON(w, http.StatusNotFound, apiErr{Message: "not found"})
        return
    }
    if err := render.JSON(w, http.StatusOK, u); err != nil {
        logger.ErrorContext(r.Context(), "render user", slog.Any("err", err))
    }
})
```

**Why a package and not just `json.NewEncoder(w).Encode`?** Three things stdlib makes
you re-solve at every call site: (1) **transactional encoding** so a mid-encode
failure doesn't leave a half-written body under an already-sent `200`; (2) **safe
`Content-Disposition`** filename quoting (RFC 5987 + injection-safe) for downloads;
(3) **templ rendering without importing templ.** `render` solves each once.

## The write model: transactional vs pass-through

This is the central behavioral contract, decided during design.

- **Transactional** (`JSON`, `HTML`, `Templ`): encode into a **pooled
  `bytes.Buffer`** first. Only if encoding fully succeeds do we set `Content-Type`,
  `WriteHeader(status)`, and copy the buffer out. On an encode error **nothing is
  written** — no header, no status, no body — and the error is returned, so the
  caller can still send a clean `500`. The buffer copy (pooled, see below) is the
  only cost; for typical payloads it is negligible.

- **Pass-through** (`JSONStream`, `Text`, `Blob`, `CSV`, `Stream`, `Attachment`): set
  headers, `WriteHeader(status)`, then write/copy/encode directly. `Text`/`Blob` carry
  an already-materialized body (the only failure is a dead connection — nothing to
  buffer). `JSONStream`, `CSV`, and `Stream`/`Attachment` write potentially large
  output and must **not** be buffered whole in RAM, so they stream; a failure
  mid-stream leaves a partial body under the sent status, and the returned error is
  only good for logging. This is documented per-function.

  `JSONStream` is the explicit streaming counterpart to the transactional `JSON`: same
  output, but `json.NewEncoder(w)` writes straight to the wire for very large payloads
  where buffering the whole document is wasteful — at the cost of `JSON`'s
  partial-body safety. `JSON` is the default; reach for `JSONStream` deliberately.

- **Server-owned** (`File`, `FileFS`, `Redirect`, `NoContent`): `Redirect` takes the
  3xx code and returns nothing; `NoContent` hard-codes `204` and returns nothing;
  `File`/`FileFS` delegate status, headers, Range, and error handling entirely to
  `http.ServeFile`/`ServeFileFS`. None of these buffer or surface an encode error,
  because there is nothing to encode.

Rule of thumb: **encoded-in-memory ⇒ transactional; genuinely streamed ⇒
pass-through.**

## Goals

- One stateless free function per response kind; consistent signature shape
  `(w, status, …) error` for the writers that can fail.
- Transactional `JSON`/`HTML`/`Templ` (no partial bodies on encode failure).
- Render a-h/templ components **with no dependency on `github.com/a-h/templ`** — via a
  locally-declared structural interface that templ's generated components satisfy.
- Safe file downloads: correct `Content-Disposition` with RFC 5987 + injection-proof
  filename handling, and stdlib-grade local file serving (Range / caching / sniffing).
- Stdlib only; no `Config` (there is nothing serializable to configure); testify in
  tests only.
- Let the caller override `Content-Type` (e.g. a custom charset) by pre-setting the
  header — helpers set it only when absent.

## Non-goals

- **Content negotiation.** No `Accept`-header sniffing or auto-format selection — the
  handler picks the function. (Would be "magic".)
- **Remote fetching.** No function takes a URL and fetches it. Serving an S3 (or any
  remote) object is either a `render.Redirect` to the object/presigned URL
  (preferred — bytes never touch your server) or the handler does its own
  `http.Client.Get` and passes `resp.Body` to `render.Stream`/`render.Attachment`. A
  hidden network call inside `render.File` would be exactly the surprise the
  framework avoids, and doing an HTTP client well (timeouts, retries, auth) is the
  handler's job.
- **Template management.** No parsing, caching, layout resolution, or `embed.FS`
  globbing — the caller builds and owns the `*template.Template`.
- **A configured `Renderer` / options / global state.** Free functions only.
- **JSON error envelope.** No opinion on error-body shape; the caller renders its own
  error type with `render.JSON`.
- **XML.** Not in scope (not requested); addable later as a peer function.
- **Compression, ETag/Last-Modified for dynamic bodies, SSE / chunked-flush
  helpers.** Compression is middleware; conditional caching for dynamic content is
  out of scope (`File`/`FileFS` get it from `http.ServeContent`); SSE is a future
  transport "port", not a render helper.

## Package & module

- Import path: `github.com/dmitrymomot/forge/render`, package `render`.
- Flat top-level layout alongside `httpserver`/`hostrouter`/`supervisor`.
- Stdlib only: `bytes`, `context`, `encoding/csv`, `encoding/json`, `errors`, `fmt`,
  `html/template`, `io`, `io/fs`, `net/http`, `strings`, `sync`, plus `mime`/`unicode`
  as needed by the `Content-Disposition` encoder.
- File layout (split by concern, mirroring the recent `sentry` split):

  ```
  render/
    doc.go            # package doc + runnable Example
    json.go           # JSON, JSONStream
    html.go           # HTML
    templ.go          # Templ + the local Component interface
    text.go           # Text, Blob, NoContent
    redirect.go       # Redirect
    csv.go            # CSV
    stream.go         # Stream, Attachment
    file.go           # File, FileFS
    content.go        # internal: content-type constants, setContentType, buffer pool, contentDisposition
    errors.go         # ErrNilTemplate, ErrNilComponent
    json_test.go html_test.go templ_test.go text_test.go redirect_test.go
    csv_test.go stream_test.go file_test.go content_test.go example_test.go
  ```

## Public API

```go
// --- json.go ---------------------------------------------------------------

// JSON encodes v as JSON and writes it with the given status. It is transactional:
// v is encoded into a pooled buffer first, so on an encode error nothing is written
// and the error is returned (the caller can still send a clean 500). Content-Type
// defaults to "application/json; charset=utf-8" unless already set.
func JSON(w http.ResponseWriter, status int, v any) error

// JSONStream is the streaming counterpart to JSON: it writes the status, then encodes
// v straight to w with json.NewEncoder(w) — no intermediate buffer. Use it for very
// large payloads where buffering the whole document is wasteful. Unlike JSON it is
// NOT transactional: an encode error mid-stream leaves a partial body under the
// already-sent status, so the returned error is only good for logging. Content-Type
// defaults to "application/json; charset=utf-8" unless already set.
func JSONStream(w http.ResponseWriter, status int, v any) error

// --- html.go ---------------------------------------------------------------

// HTML executes an html/template into a pooled buffer, then writes it with status.
// name == "" runs t.Execute(data); otherwise t.ExecuteTemplate(name, data) — the
// layout/{{define}} pattern. Transactional: a template execution error returns with
// nothing written. Returns ErrNilTemplate if t is nil (before writing anything).
// Content-Type defaults to "text/html; charset=utf-8" unless already set.
func HTML(w http.ResponseWriter, status int, t *template.Template, name string, data any) error

// --- templ.go --------------------------------------------------------------

// Component is anything that renders itself to an io.Writer. It is structurally
// satisfied by github.com/a-h/templ components (templ.Component has the identical
// method), so templ output works without this module importing templ.
type Component interface {
    Render(ctx context.Context, w io.Writer) error
}

// Templ renders c into a pooled buffer, then writes it with status. Transactional:
// a Render error returns with nothing written. Returns ErrNilComponent if c is nil
// (before writing anything). ctx is the per-request context (usually r.Context()).
// Content-Type defaults to "text/html; charset=utf-8" unless already set.
func Templ(ctx context.Context, w http.ResponseWriter, status int, c Component) error

// --- text.go ---------------------------------------------------------------

// Text writes s with status as text/plain; charset=utf-8 (unless already set).
func Text(w http.ResponseWriter, status int, s string) error

// Blob writes b with status. If contentType != "" it is used (unless already set);
// otherwise net/http sniffs the body on first write.
func Blob(w http.ResponseWriter, status int, contentType string, b []byte) error

// NoContent writes 204 No Content with no body. It cannot fail, so it returns nothing.
func NoContent(w http.ResponseWriter)

// --- redirect.go -----------------------------------------------------------

// Redirect issues an HTTP redirect to url with status (a 3xx, e.g. http.StatusFound /
// StatusSeeOther). Thin wrapper over http.Redirect.
func Redirect(w http.ResponseWriter, r *http.Request, status int, url string)

// --- csv.go ----------------------------------------------------------------

// CSV streams records as text/csv with status. If filename != "" a
// Content-Disposition: attachment header is set (filename quoted RFC 5987-safe).
// CSV streams (it is not buffered), so a write error mid-output leaves a partial
// body; the returned error is for logging.
func CSV(w http.ResponseWriter, status int, filename string, records [][]string) error

// --- stream.go -------------------------------------------------------------

// Stream copies body to the response inline with status. If contentType != "" it is
// used (unless already set); otherwise net/http sniffs. Pass-through: a copy error
// mid-stream leaves a partial body; the returned error is for logging. Use this to
// proxy an io.Reader (e.g. an upstream/S3 response body) inline.
func Stream(w http.ResponseWriter, status int, contentType string, body io.Reader) error

// Attachment is Stream plus Content-Disposition: attachment; filename="…" (RFC
// 5987-safe). contentType defaults to application/octet-stream when empty (download
// intent). Use for generated downloads (a built CSV/PDF, an export stream, a proxied
// object you want saved rather than displayed).
func Attachment(w http.ResponseWriter, status int, filename, contentType string, body io.Reader) error

// --- file.go ---------------------------------------------------------------

// File serves a single local file by path via http.ServeFile (Range, If-Modified-Since,
// and content sniffing handled by stdlib). path is server-trusted — do NOT pass an
// unsanitized user path; use FileFS with a rooted fs.FS for user-influenced names.
// Status and errors are owned by http.ServeFile.
func File(w http.ResponseWriter, r *http.Request, path string)

// FileFS serves name from fsys via http.ServeFileFS — the safe, rooted form. Pass
// os.DirFS("/var/www") for a directory root, or an embed.FS for bundled assets;
// name resolution is constrained to fsys.
func FileFS(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string)
```

## Internal helpers (`content.go`)

```go
const (
    contentTypeJSON  = "application/json; charset=utf-8"
    contentTypeHTML  = "text/html; charset=utf-8"
    contentTypeText  = "text/plain; charset=utf-8"
    contentTypeCSV   = "text/csv; charset=utf-8"
    contentTypeOctet = "application/octet-stream"
)

// setContentType sets Content-Type to ct only if the caller has not already set one,
// so a handler can pre-set a custom charset/parameters and have it win.
func setContentType(w http.ResponseWriter, ct string) {
    if w.Header().Get("Content-Type") == "" {
        w.Header().Set("Content-Type", ct)
    }
}

// bufPool backs the transactional encoders (JSON/HTML/Templ).
var bufPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

func getBuf() *bytes.Buffer { return bufPool.Get().(*bytes.Buffer) }

func putBuf(b *bytes.Buffer) {
    const maxReuse = 1 << 20 // don't pin >1 MiB buffers in the pool
    if b.Cap() > maxReuse {
        return
    }
    b.Reset()
    bufPool.Put(b)
}
```

The transactional writers share one shape:

```go
func JSON(w http.ResponseWriter, status int, v any) error {
    buf := getBuf()
    defer putBuf(buf)
    if err := json.NewEncoder(buf).Encode(v); err != nil {
        return fmt.Errorf("render: encode json: %w", err) // nothing written yet
    }
    setContentType(w, contentTypeJSON)
    w.WriteHeader(status)
    _, err := w.Write(buf.Bytes())
    return err
}
```

`JSONStream` skips the buffer — it commits the status first, then encodes to the
wire (pass-through, so an error is only loggable):

```go
func JSONStream(w http.ResponseWriter, status int, v any) error {
    setContentType(w, contentTypeJSON)
    w.WriteHeader(status)
    if err := json.NewEncoder(w).Encode(v); err != nil {
        return fmt.Errorf("render: stream json: %w", err) // status/partial body already sent
    }
    return nil
}
```

`HTML` and `Templ` follow the same pattern as `JSON` (execute/Render into `buf`,
return on error before touching `w`), with their nil-guards first:

```go
func HTML(w http.ResponseWriter, status int, t *template.Template, name string, data any) error {
    if t == nil {
        return ErrNilTemplate
    }
    buf := getBuf(); defer putBuf(buf)
    var err error
    if name == "" {
        err = t.Execute(buf, data)
    } else {
        err = t.ExecuteTemplate(buf, name, data)
    }
    if err != nil {
        return fmt.Errorf("render: execute template: %w", err)
    }
    setContentType(w, contentTypeHTML)
    w.WriteHeader(status)
    _, err = w.Write(buf.Bytes())
    return err
}
```

### `Content-Disposition` (download filename safety)

`CSV` and `Attachment` set `Content-Disposition` from a caller-supplied filename,
which may contain quotes, control characters, path separators, or non-ASCII — all of
which can break the header or enable header injection. A single internal helper
produces a safe value:

```go
// contentDisposition builds e.g.
//   attachment; filename="report.csv"; filename*=UTF-8''rapport%20%C3%A9t%C3%A9.csv
// It uses only the base name, replaces control/quote/separator bytes in the ASCII
// fallback with '_', and adds an RFC 5987 filename* (percent-encoded UTF-8) so
// modern clients get the exact (possibly non-ASCII) name. Returns just the
// disposition (no filename params) when name sanitizes to empty.
func contentDisposition(disposition, filename string) string
```

The exact RFC 5987 encoder (attr-char set per the RFC) is a small hand-rolled
function — no dependency — unit-tested against unicode names and injection inputs
(embedded `"`, `\r\n`, `../`).

## Errors (`errors.go`)

```go
// ErrNilTemplate is returned by HTML when the template is nil.
var ErrNilTemplate = errors.New("render: nil template")

// ErrNilComponent is returned by Templ when the component is nil.
var ErrNilComponent = errors.New("render: nil component")
```

Single-line, `render:`-prefixed, `errors.Is`-matchable. Unlike `hostrouter`'s
construction-time panics, these are **returned** — a nil template/component is a
per-request condition, and because the guard runs before any write the caller can
still send a `500`. All other returned errors wrap the underlying
encode/template/copy error with a single-line `render: <op>: %w` prefix (per the
framework's single-line-error rule — no multi-line blobs in error strings; verbose
context belongs on the structured logger).

## templ without a dependency

templ's generated components implement `templ.Component`, whose sole method is
`Render(ctx context.Context, w io.Writer) error`. By declaring a local `Component`
interface with that exact method set, every templ component satisfies `render.Component`
structurally — Go interface satisfaction is structural, so no import of
`github.com/a-h/templ` is needed and the framework's "no external dependencies" rule
holds. A consumer already using templ writes:

```go
func handle(w http.ResponseWriter, r *http.Request) {
    if err := render.Templ(r.Context(), w, http.StatusOK, views.Dashboard(user)); err != nil {
        logger.ErrorContext(r.Context(), "render dashboard", slog.Any("err", err))
    }
}
```

`views.Dashboard(user)` returns a `templ.Component`, which is assignable to
`render.Component`. If templ ever changed that signature the consumer's code would
fail to compile at the call site (caught immediately), not silently — an acceptable,
visible coupling for a zero-dependency win.

## Remote / S3 objects (no new API)

Serving an object that lives in S3 (or behind any URL) uses existing functions:

```go
// (a) Preferred — redirect; the client pulls bytes straight from S3.
//     For a private bucket, generate a presigned URL first, then:
render.Redirect(w, r, http.StatusFound, objectURL)

// (b) Proxy — only when you must hide the origin or add auth. The handler owns the
//     HTTP client (timeouts/retries); render just writes the body through.
resp, err := httpClient.Get(objectURL)
if err != nil {
    _ = render.JSON(w, http.StatusBadGateway, apiErr{Message: "upstream"})
    return
}
defer resp.Body.Close()
// inline:
_ = render.Stream(w, resp.StatusCode, resp.Header.Get("Content-Type"), resp.Body)
// or force download:
_ = render.Attachment(w, http.StatusOK, "report.pdf", resp.Header.Get("Content-Type"), resp.Body)
```

The `io.Reader` signature on `Stream`/`Attachment` is what makes the proxy case (and
in-memory `bytes.Reader`, and any generated stream) work without a URL-aware
function. `render` never opens a socket.

## Behavior details & edge cases

- **Header ordering.** Every writer sets all headers (`Content-Type`,
  `Content-Disposition`) **before** `WriteHeader`, the only correct order.
- **Content-Type override.** Set-if-empty everywhere, including `Blob`/`Stream`/
  `Attachment` whose explicit `contentType` arg is itself the override; a pre-set
  header still wins. Documented.
- **`status` validity.** Callers pass a valid code (`net/http` panics for `< 100` or
  `> 999`); helpers don't second-guess it. `NoContent` hard-codes `204`.
- **`JSON(w, status, nil)`** writes `null` (encoder behavior); `json.NewEncoder`
  appends a trailing newline — kept (standard, harmless).
- **Double write.** Calling a render helper after the handler already wrote a body
  triggers net/http's "superfluous WriteHeader" — documented as caller error; the
  package does not track prior writes.
- **`NoContent` + body.** None is written; correct per RFC for 204.
- **`CSV` with empty `records`** writes only headers (CT + optional disposition) and
  a 0-row body — valid.
- **Empty filename** to `CSV`/`Attachment` omits `Content-Disposition` entirely.
- **`File` path safety.** `http.ServeFile` special-cases `..` in the *request* path
  but the `path` argument is trusted; the doc comment directs user-influenced names
  to `FileFS`.

## Usage

```go
// JSON API
_ = render.JSON(w, http.StatusOK, dto)
_ = render.JSON(w, http.StatusUnprocessableEntity, validationErrors)
_ = render.JSONStream(w, http.StatusOK, hugeReport) // stream a large payload, no buffering

// Server-rendered HTML (layout + define blocks)
_ = render.HTML(w, http.StatusOK, tmpl, "dashboard.html", vm)

// templ
_ = render.Templ(r.Context(), w, http.StatusOK, views.Invoice(inv))

// primitives
_ = render.Text(w, http.StatusOK, "pong")
_ = render.Blob(w, http.StatusOK, "image/png", pngBytes)
render.NoContent(w)
render.Redirect(w, r, http.StatusSeeOther, "/login")

// downloads
_ = render.CSV(w, http.StatusOK, "users.csv", rows)
_ = render.Attachment(w, http.StatusOK, "invoice.pdf", "application/pdf", pdf)
render.FileFS(w, r, assets, "logo.svg") // embed.FS
```

## Testing

White-box (`package render`) `httptest`-driven tests; testify only.

- **`JSON`:** success (status, `Content-Type`, body, trailing newline); **transactional
  failure** with an unmarshalable value (`make(chan int)` / a `MarshalJSON` that
  errors) → asserts the error is returned **and** the recorder shows nothing written
  (default `200`, empty body, no `Content-Type` set); `nil` → `null`; pre-set
  `Content-Type` is honored.
- **`JSONStream`:** success (status, CT, body matches `JSON`); **pass-through failure**
  with an unmarshalable value → asserts the error is returned **and** (unlike `JSON`)
  the status was already committed (`200` written, CT set) — proving it streams rather
  than buffers.
- **`HTML`:** `Execute` (name=="") and `ExecuteTemplate` (named) success; `nil`
  template → `ErrNilTemplate`, nothing written; an execution error (template action
  that fails) → wrapped error, nothing written.
- **`Templ`:** success via a fake `Component`; `nil` → `ErrNilComponent`; a `Render`
  error → propagated, nothing written; the passed `ctx` reaches the component (the
  fake records it).
- **`Text`/`Blob`/`NoContent`/`Redirect`:** status + CT + body; `Blob` empty
  contentType → sniffed; `NoContent` → 204, empty body; `Redirect` → `Location` + 3xx.
- **`CSV`:** CT `text/csv`, body matches `encoding/csv`; with/without `filename` →
  presence/absence of `Content-Disposition`; empty records.
- **`Stream`/`Attachment`:** success (CT, body, `Content-Disposition`, octet-stream
  default); a body `io.Reader` that errors → returned error (partial allowed,
  pass-through).
- **`File`/`FileFS`:** serve a temp file and an `embed.FS`/`fstest.MapFS`; assert body
  + sniffed content-type; a `Range` request returns `206` (proves stdlib delegation).
- **`contentDisposition`:** table test — ASCII, unicode (`filename*` correct), and
  injection inputs (embedded quote, CRLF, `../`) produce a safe single-line value.
- **Write-error propagation:** a failing `http.ResponseWriter` (its `Write` returns an
  error) → every writer surfaces the error.
- **`example_test.go`:** a runnable `Example` for godoc.
- **(Optional) `bench_test.go`:** `BenchmarkJSON -benchmem` to show the pooled buffer
  keeps allocations flat.

## Future fit

- `XML` (`encoding/xml`) and an SSE/event-stream helper can be added later as peer
  free functions without touching the existing ones.
- A future **`htmx` sibling package** (HTMX response/request header helpers —
  `HX-Redirect`, `HX-Location`, `HX-Trigger`, `htmx.IsRequest(r)`, etc.) is
  deliberately **out of scope** here and will get its own spec. It would depend on
  `render`/`net/http` (e.g. its non-HTMX redirect branch calling `render.Redirect`),
  a one-way dependency — `render` never learns about HTMX. Branching a response on a
  request header (HTMX-aware redirect) is a form of content negotiation, which this
  package lists under Non-goals, so it belongs in the dedicated package, not here.
- If consumers converge on one error-body shape, an opinionated `render.Error` could
  be added on top of `JSON` — deferred until there's a shared convention to encode.

## Deferred

- Content negotiation, remote URL fetching, template management/caching, a configured
  `Renderer`/options, compression, dynamic-body ETag/Last-Modified, SSE/chunked-flush
  helpers, XML, and a JSON error envelope — all per "Non-goals", revisited only on
  demonstrated need.
