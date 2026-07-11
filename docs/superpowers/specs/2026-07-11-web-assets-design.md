# web/assets — design

Date: 2026-07-11
Status: approved for planning

## Purpose

Serve a tree of static files over an `fs.FS` (embed.FS in prod, os.DirFS in
dev) with the correctness and caching a production SaaS needs: right content
types, Range requests, ETag/304, and a **content-fingerprint manifest** so
hashed URLs can carry far-future `immutable` cache headers. Templates resolve
logical names to fingerprinted URLs and SRI hashes via `URL()`/`Integrity()`.
Optional SPA fallback and opportunistic precompressed-sibling serving.

Primary consumers: any forge app serving its own CSS/JS/fonts/images —
especially the htmx/templ Go stack, where there is no JS build step and the
package fingerprints an embedded `static/` dir at startup. Secondary: apps
that bring a JS bundler (Vite/esbuild/webpack) and want forge to serve the
bundler's already-hashed output from a manifest.

**Not a bundler.** No transpile, minify, tree-shake, or import-graph
resolution. forge either reads a manifest another tool produced or hashes
files as-is.

## Placement

`web/assets`. Web-boundary concern, same shelf as `render`, `compress`,
`secheaders`. Sits beside — not on top of — `render.File`/`render.FileFS`
(one-shot single-file serving inside a handler) and `web/compress` (dynamic
per-request response compression). `assets` owns the distinct concern of a
*mounted, fingerprinted, cache-policied static tree*. Zero forge deps, stdlib
only (`net/http`, `io/fs`, `crypto/sha256`+`sha512`, `mime`, `encoding/json`).

When this ships, delete the `web/assets` entry from docs/packages.md.

## Idiom & anatomy

`New(...Option)` with an env-loadable `Config` + `DefaultConfig` + `Validate`.
One `*Assets` = one `fs.FS` root mounted at one URL prefix. It is an
`http.Handler` and also exposes the template-facing lookups. Two roots (e.g.
embedded `/static/` and disk `/uploads/`) = two `*Assets`, wired separately.

Files: `doc.go` (runnable example) · `config.go` · `options.go` · `errors.go`
· `assets.go` (New + ServeHTTP) · `manifest.go` (both build paths + `Reader`
seam) · `fingerprint.go` (hashing) · `mime.go` (content-type overlay) ·
`spa.go` · `precompress.go`. Estimated ~600–800 LOC — upper end of the
250–850 guideline, justified by a single responsibility.

## API surface

```go
package assets

type Config struct {
    Dev          bool   `env:"ASSETS_DEV"`      // flip fingerprint/cache/reload behavior
    Prefix       string `env:"ASSETS_PREFIX"`   // default "/static/"
    ManifestPath string `env:"ASSETS_MANIFEST"` // default "manifest.json"; "" disables external read
}

func DefaultConfig() Config
func (c Config) Validate() error

func New(fsys fs.FS, opts ...Option) (*Assets, error)

// Template-facing lookups.
func (a *Assets) URL(name string) string        // "app.css" -> "/static/app.a1b2c3.css"
func (a *Assets) Integrity(name string) string  // "app.css" -> "sha384-…" ("" if unknown or dev)
func (a *Assets) Lookup(name string) (Entry, bool)
func (a *Assets) Prefix() string
func (a *Assets) FuncMap() template.FuncMap      // {"asset": URL, "sri": Integrity} convenience

// Serving.
func (a *Assets) ServeHTTP(w http.ResponseWriter, r *http.Request)

// Options.
func WithPrefix(p string) Option
func WithDev(dev bool) Option
func WithManifest(path string) Option
func WithReader(r Reader) Option                              // manifest seam (e.g. a Vite adapter)
func WithSPA(index string, when ...func(*http.Request) bool) Option
func WithPrecompressed(encodings ...string) Option           // default {"br","gz"}
func WithCacheControl(immutable, revalidate string) Option   // override header strings

// Manifest seam.
type Reader interface {
    Read(fsys fs.FS) (map[string]Entry, error)
}

type Entry struct {
    Path      string // fingerprinted URL path relative to prefix, e.g. "app.a1b2c3.css"
    Integrity string // SRI hash, e.g. "sha384-…"
    // unexported: real name in fsys, size, content type, modtime
}
```

Consumer wiring:

```go
a, err := assets.New(staticFS, assets.WithSPA("index.html"))
mux.Handle(a.Prefix(), a)                 // serve the tree
tpl.Funcs(a.FuncMap())                    // html/template
// templ: call a.URL(...) / a.Integrity(...) directly
```

## Manifest model — one table, three build paths

Every mode converges on one in-memory map `logical name -> Entry`, built once
at `New()`:

- **External** — a manifest is resolvable (an explicit `WithReader`, else the
  flat `manifest.json` at `ManifestPath`). Flat schema is
  `{"app.css":"app.a1b2c3.css"}` or
  `{"app.css":{"file":"app.a1b2c3.css","integrity":"sha384-…"}}`. Hashed files
  physically exist in `fsys` under their hashed names. If `integrity` is
  absent, it is computed by hashing the target file. A malformed manifest, or
  one referencing a file absent from `fsys`, fails `New` (fail fast).
- **Runtime** (the "else" of external — no manifest present) — walk `fsys`,
  hash each file, inject the hash before the extension
  (`app.css -> app.<hash>.css`), and compute SRI. The hashed name is
  *virtual*; a reverse map resolves it back to the real file at serve time.
- **Dev** (`WithDev(true)`) — no table: `URL` returns the unhashed
  `/static/app.css`, `Integrity` returns `""`, and files are re-read from
  `fsys` each request (live edits when the consumer passes `os.DirFS`).

Reader precedence: `WithReader` > flat `manifest.json` at `ManifestPath` >
runtime fingerprinting. `WithDev(true)` overrides all three at lookup/serve
time. If `ManifestPath` is set but the file is absent, fall back to runtime
fingerprinting (not an error); if it is present but malformed, error.

Filename hash: short hex of sha256 of the file contents. SRI: sha384, base64,
`sha384-…` form (the standard). Two distinct hashes by design.

## Serving pipeline

`ServeHTTP` strips `Prefix`, validates the tail with `fs.ValidPath` (rejecting
traversal, absolute, and `..` paths), then resolves in order:

1. **Known fingerprinted path** (`app.a1b2c3.css`) — resolve to the real file;
   `Cache-Control: public, max-age=31536000, immutable`; `ETag` = content hash.
2. **Plain existing file** (direct hit, or any request in dev) —
   `Cache-Control: no-cache` + `ETag`; revalidates via 304.
3. **SPA fallback** (if `WithSPA` set and the predicate matches) — serve the
   configured `index` with `no-cache`.
4. Otherwise **404**.

Byte-writing goes through `http.ServeContent`, which provides Range, `If-Range`,
`If-Modified-Since`, and `If-None-Match`/304. The corrected `Content-Type` is
set *before* the call so `ServeContent` never sniffs.

### Content-type overlay (`mime.go`)

`mime.TypeByExtension` first; a small overlay fills its gaps and fixes wrong
answers: `.woff2`, `.webmanifest` -> `application/manifest+json`, `.avif`,
`.mjs`/`.js` -> `text/javascript`, `.wasm` -> `application/wasm`, `.map` ->
`application/json`. (Exact table finalized in implementation.)

### Precompressed siblings (`WithPrecompressed`, default `{"br","gz"}`)

Before writing, if the request's `Accept-Encoding` includes an encoding whose
sibling file exists (`<file>.br`, then `.gz`), serve that sibling's bytes with
`Content-Encoding` + `Vary: Accept-Encoding` and the **original** file's
`Content-Type`. Opportunistic only — forge compresses nothing itself; the
sibling must already exist (typically a bundler emitted it). Clean division
with `web/compress` (dynamic) since this fires only on a present static
sibling. Precompressed responses skip Range (no `Accept-Ranges` advertised) —
Range over pre-encoded bytes is a spec footgun not worth the complexity.

### SPA fallback predicate

`WithSPA(index string, when ...func(*http.Request) bool)` installs an exported
default predicate: fall back only when the method is `GET`/`HEAD` **and** the
path has no file extension **or** the client sent `Accept: text/html`; else
404. So `/dashboard/settings` -> `index.html` (200) but `/static/typo.js` ->
404 (missing-asset bugs stay visible). A consumer with unusual routing passes
its own `when`. The `index` file is always served revalidatable (`no-cache` /
ETag), never `immutable` — it is the entry point that references the
fingerprinted assets and must stay fresh. SPA fallback is off unless
`WithSPA` is called (a plain asset server should not invent an index).

## Errors

`errors.go`, `errors.Is`-matchable single-line sentinels:

- `ErrInvalidConfig` — empty or relative `Prefix`, etc. (from `Validate`).
- `ErrManifest` — malformed external manifest, or a manifest referencing a
  file absent from `fsys`.

`New` fails fast on both. `URL`/`Integrity` never panic: an unknown name makes
`URL` return best-effort `Prefix + name` (so a link still points at the raw
file if it exists) and `Integrity` return `""`. Use `Lookup` for a checked
`(Entry, bool)`.

## Documented limitations & non-goals

Flowing from "not a bundler":

- **No intra-asset URL rewriting.** Runtime mode does not rewrite `url(...)`
  inside CSS or import paths inside JS. If assets reference each other by
  hashed name, use external-manifest mode (the bundler already rewrote them)
  or reference by absolute unhashed path.
- **No transpile / minify / tree-shake / import-graph resolution.**
- **No dynamic compression.** That is `web/compress`; `assets` only serves
  *pre*-compressed siblings that already exist.
- **No directory listings.**
- **Read-only.** No upload/write surface.

## Testing

Black-box (`assets_test`), `fstest.MapFS` as the `fs.FS`, `httptest`. Matrix:

- fingerprint determinism (same content -> same hash; content change -> new hash)
- immutable-vs-no-cache header selection per resolution branch
- ETag emission and `If-None-Match` -> 304
- Range / `If-Range`
- SPA predicate table (extension present/absent × Accept × method) and the
  `no-cache` on `index`
- precompressed negotiation + `Vary`, sibling-absent fallthrough, Range skipped
- external manifest: flat, with/without integrity, malformed -> `ErrManifest`,
  absent -> runtime fallback
- `Reader` seam (custom reader wins over `manifest.json`)
- dev mode: unhashed `URL`, empty `Integrity`, `no-cache`, per-request re-read
- content-type overlay coverage
- path traversal rejection (`..`, absolute, encoded)
- unknown-asset behavior (`URL` passthrough, `Integrity` "", `Lookup` false)

A serve-hot-path benchmark is added only if any perf-motivated complexity is
introduced (per design.md §Performance).
