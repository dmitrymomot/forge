# web/assets Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `web/assets` — a mounted static-file `http.Handler` over an `fs.FS` with content-fingerprinting, a manifest for `URL()`/`Integrity()`, correct content types, Range/ETag/304, immutable caching, opportunistic precompressed siblings, and optional SPA fallback.

**Architecture:** `New(fsys, ...Option) (*Assets, error)` builds one in-memory `logical name → Entry` table at startup by one of three paths — external flat `manifest.json`, a custom `Reader`, or runtime fingerprinting (walk + hash the `fs.FS`) — or skips it entirely in dev mode. `*Assets` is an `http.Handler` that resolves each request to an immutable-cached fingerprinted file, a no-cache plain file, a precompressed sibling, an SPA index, or 404; templates call `URL`/`Integrity`.

**Tech Stack:** Go 1.26, stdlib only (`net/http`, `io/fs`, `crypto/sha256`+`sha512`, `mime`, `encoding/json`, `html/template`). No forge deps. Spec: [docs/superpowers/specs/2026-07-11-web-assets-design.md](../specs/2026-07-11-web-assets-design.md).

## Global Constraints

- **Go 1.26**, single module `github.com/dmitrymomot/forge`; package path `web/assets`; import path `github.com/dmitrymomot/forge/web/assets`.
- **stdlib only** — zero forge deps, no third-party deps.
- **Idiom:** `New(...Option) (*Assets, error)` with env-loadable `Config` + `DefaultConfig` + `Validate`; `type Option func(*config)`, never builders. Public methods never return unexported types.
- **Tests are black-box only:** package `assets_test`; exercise behavior through the public API (`New`, `URL`, `Integrity`, `Lookup`, `ServeHTTP`). Use `testing/fstest.MapFS` as the `fs.FS` and `net/http/httptest`.
- **Errors:** `errors.Is`-matchable single-line sentinels (`ErrInvalidConfig`, `ErrManifest`).
- **Field layout** is enforced by betteralign via `just lint`; **always run `just fmt ./web/assets/...`** (package-path form — the single-file form trips a spurious betteralign error) after editing, before committing.
- **Env prefix** baked into tags: `ASSETS_PREFIX`, `ASSETS_MANIFEST`, `ASSETS_DEV`.
- **Commits:** Conventional Commits; no Claude attribution/co-author trailers of any kind.
- **After the final task:** run `just lint` and `just test ./web/assets/...` and confirm both pass.
- Per-step fast iteration uses `go test ./web/assets/ -run <Name> -v`; task-final verification uses `just test ./web/assets/...` (race + cover).

---

### Task 1: Package spine — Config, errors, type scaffold, `New`, `Prefix`

**Files:**
- Create: `web/assets/config.go`
- Create: `web/assets/errors.go`
- Create: `web/assets/options.go`
- Create: `web/assets/assets.go`
- Test: `web/assets/assets_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Config struct { Prefix string; ManifestPath string; Dev bool }` with `env` tags; `func DefaultConfig() Config`; `func (c Config) Validate() error`.
  - `var ErrInvalidConfig error`; `var ErrManifest error`.
  - `type Entry struct { Path string; Integrity string; real string }` (unexported `real`).
  - `type Reader interface { Read(fsys fs.FS) (map[string]Entry, error) }`.
  - `type Option func(*config)`; `WithConfig(Config) Option`; `WithPrefix(string) Option`; `WithDev(bool) Option`.
  - `type Assets struct{…}`; `func New(fsys fs.FS, opts ...Option) (*Assets, error)`; `func (a *Assets) Prefix() string`.
  - Package constants `defaultImmutable`, `defaultRevalidate`.

- [ ] **Step 1: Write the failing test**

`web/assets/assets_test.go`:
```go
package assets_test

import (
	"errors"
	"testing"
	"testing/fstest"

	"github.com/dmitrymomot/forge/web/assets"
)

func newFS() fstest.MapFS {
	return fstest.MapFS{
		"app.css":     {Data: []byte("body{color:red}")},
		"js/app.js":   {Data: []byte("console.log(1)")},
		"index.html":  {Data: []byte("<!doctype html><title>x</title>")},
	}
}

func TestNewDefaultPrefix(t *testing.T) {
	a, err := assets.New(newFS())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := a.Prefix(); got != "/static/" {
		t.Fatalf("Prefix = %q, want /static/", got)
	}
}

func TestWithPrefixNormalizesTrailingSlash(t *testing.T) {
	a, err := assets.New(newFS(), assets.WithPrefix("/assets"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := a.Prefix(); got != "/assets/" {
		t.Fatalf("Prefix = %q, want /assets/", got)
	}
}

func TestValidateRejectsUnrootedPrefix(t *testing.T) {
	_, err := assets.New(newFS(), assets.WithPrefix("assets"))
	if !errors.Is(err, assets.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/assets/ -run 'TestNew|TestWith|TestValidate' -v`
Expected: build failure — `undefined: assets.New` / `assets.ErrInvalidConfig`.

- [ ] **Step 3: Write the implementation**

`web/assets/config.go`:
```go
package assets

import (
	"errors"
	"fmt"
)

// ErrInvalidConfig marks a Config that fails Validate (e.g. a Prefix that is
// not rooted at "/").
var ErrInvalidConfig = errors.New("assets: invalid config")

// Config is the env-loadable asset-serving policy.
type Config struct {
	Prefix       string `env:"ASSETS_PREFIX"`   // URL mount prefix, e.g. "/static/"
	ManifestPath string `env:"ASSETS_MANIFEST"` // flat manifest path within fsys; "" disables external read
	Dev          bool   `env:"ASSETS_DEV"`      // serve unhashed + no-cache + re-read each request
}

// DefaultConfig serves from "/static/" and looks for "manifest.json".
func DefaultConfig() Config {
	return Config{Prefix: "/static/", ManifestPath: "manifest.json"}
}

// Validate checks that Prefix is rooted at "/".
func (c Config) Validate() error {
	if c.Prefix == "" || c.Prefix[0] != '/' {
		return fmt.Errorf("%w: prefix %q must start with /", ErrInvalidConfig, c.Prefix)
	}
	return nil
}
```

`web/assets/errors.go`:
```go
package assets

import "errors"

// ErrManifest marks a malformed external manifest, or one that references a
// file absent from the fs.FS.
var ErrManifest = errors.New("assets: invalid manifest")
```

`web/assets/options.go`:
```go
package assets

import (
	"io/fs"
	"net/http"
)

// Entry is one resolved asset, returned by Lookup.
type Entry struct {
	Path      string // fingerprinted name relative to Prefix, e.g. "app.a1b2c3.css"
	Integrity string // Subresource Integrity value, e.g. "sha384-…"; "" if unknown
	real      string // real path within the fs.FS
}

// Reader builds a logical-name → Entry table from the fs.FS. Implement it to
// support a bundler manifest forge does not read natively (e.g. Vite). The
// returned Entry.Path is the hashed filename that exists within fsys.
type Reader interface {
	Read(fsys fs.FS) (map[string]Entry, error)
}

type config struct {
	cfg          Config
	reader       Reader
	spaWhen      func(*http.Request) bool
	spaIndex     string
	immutableCC  string
	revalidateCC string
	precompress  []string
}

// Option configures New.
type Option func(*config)

// WithConfig applies an env-loaded Config. Place it first; later options override.
func WithConfig(c Config) Option { return func(cf *config) { cf.cfg = c } }

// WithPrefix sets the URL mount prefix (default "/static/"). A missing trailing
// slash is added.
func WithPrefix(p string) Option { return func(cf *config) { cf.cfg.Prefix = p } }

// WithDev toggles dev mode: unhashed URLs, no-cache, per-request re-read.
func WithDev(dev bool) Option { return func(cf *config) { cf.cfg.Dev = dev } }
```

`web/assets/assets.go`:
```go
package assets

import (
	"io/fs"
	"strings"
)

const (
	defaultImmutable  = "public, max-age=31536000, immutable"
	defaultRevalidate = "no-cache"
)

// Assets serves a fingerprinted static file tree from a single fs.FS mounted at
// one URL prefix. It is an http.Handler and also resolves logical asset names to
// fingerprinted URLs (URL) and SRI hashes (Integrity). Construct it with New.
type Assets struct {
	fsys         fs.FS
	table        map[string]Entry  // logical name → Entry
	reverse      map[string]string // served path → real name in fsys
	spaWhen      func(*http.Request) bool
	prefix       string
	spaIndex     string
	immutableCC  string
	revalidateCC string
	precompress  []string
	dev          bool
}

// New builds an Assets over fsys. It validates the config, then builds the
// fingerprint table (external manifest, custom Reader, or runtime hashing) —
// unless dev mode is set, which skips the table. It returns ErrInvalidConfig for
// a bad config and ErrManifest for a malformed/incoherent external manifest.
func New(fsys fs.FS, opts ...Option) (*Assets, error) {
	c := config{cfg: DefaultConfig(), immutableCC: defaultImmutable, revalidateCC: defaultRevalidate}
	for _, o := range opts {
		o(&c)
	}
	if !strings.HasSuffix(c.cfg.Prefix, "/") {
		c.cfg.Prefix += "/"
	}
	if err := c.cfg.Validate(); err != nil {
		return nil, err
	}
	a := &Assets{
		fsys:         fsys,
		table:        map[string]Entry{},
		reverse:      map[string]string{},
		spaWhen:      c.spaWhen,
		prefix:       c.cfg.Prefix,
		spaIndex:     c.spaIndex,
		immutableCC:  c.immutableCC,
		revalidateCC: c.revalidateCC,
		precompress:  c.precompress,
		dev:          c.cfg.Dev,
	}
	return a, nil
}

// Prefix returns the normalized URL mount prefix (always trailing-slashed).
func (a *Assets) Prefix() string { return a.prefix }
```

Note: `net/http` is imported in options.go for the `spaWhen` field type; `Reader`/`Entry` are unused by Task 1 behavior but are part of the spine the later tasks fill in.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./web/assets/ -run 'TestNew|TestWith|TestValidate' -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Format, then commit**

```bash
just fmt ./web/assets/...
git add web/assets/
git commit -m "feat(assets): package spine — Config, errors, New, Prefix"
```

---

### Task 2: Runtime fingerprinting + `URL` / `Integrity` / `Lookup` / `FuncMap`

**Files:**
- Create: `web/assets/fingerprint.go`
- Create: `web/assets/manifest.go`
- Modify: `web/assets/assets.go` (call `load` from `New`; add lookup methods)
- Test: `web/assets/manifest_test.go`

**Interfaces:**
- Consumes: `Entry`, `Assets`, `New` (Task 1).
- Produces:
  - `func shortHash(data []byte) string` (8 hex chars), `func sri(data []byte) string` ("sha384-…"), `func injectHash(name, hash string) string`.
  - `func buildRuntime(fsys fs.FS) (table map[string]Entry, reverse map[string]string, err error)`.
  - `func (a *Assets) load(c config) error` (runtime path only; extended in Task 4).
  - `func (a *Assets) URL(name string) string`; `func (a *Assets) Integrity(name string) string`; `func (a *Assets) Lookup(name string) (Entry, bool)`; `func (a *Assets) FuncMap() template.FuncMap`.

- [ ] **Step 1: Write the failing test**

`web/assets/manifest_test.go`:
```go
package assets_test

import (
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dmitrymomot/forge/web/assets"
)

var hashedCSS = regexp.MustCompile(`^/static/app\.[0-9a-f]{8}\.css$`)

func TestURLFingerprintsAtRuntime(t *testing.T) {
	a, err := assets.New(newFS())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := a.URL("app.css")
	if !hashedCSS.MatchString(got) {
		t.Fatalf("URL = %q, want /static/app.<8hex>.css", got)
	}
	if s := a.Integrity("app.css"); !strings.HasPrefix(s, "sha384-") {
		t.Fatalf("Integrity = %q, want sha384- prefix", s)
	}
}

func TestURLDeterministicAndContentSensitive(t *testing.T) {
	a1, _ := assets.New(fstest.MapFS{"app.css": {Data: []byte("A")}})
	a2, _ := assets.New(fstest.MapFS{"app.css": {Data: []byte("A")}})
	a3, _ := assets.New(fstest.MapFS{"app.css": {Data: []byte("B")}})
	if a1.URL("app.css") != a2.URL("app.css") {
		t.Fatal("same content produced different hashes")
	}
	if a1.URL("app.css") == a3.URL("app.css") {
		t.Fatal("different content produced same hash")
	}
}

func TestLookupAndUnknownPassthrough(t *testing.T) {
	a, _ := assets.New(newFS())
	if _, ok := a.Lookup("app.css"); !ok {
		t.Fatal("Lookup(app.css) not found")
	}
	if _, ok := a.Lookup("missing.css"); ok {
		t.Fatal("Lookup(missing.css) unexpectedly found")
	}
	if got := a.URL("missing.css"); got != "/static/missing.css" {
		t.Fatalf("unknown URL = %q, want /static/missing.css", got)
	}
	if got := a.Integrity("missing.css"); got != "" {
		t.Fatalf("unknown Integrity = %q, want empty", got)
	}
}

func TestDevModePassthrough(t *testing.T) {
	a, _ := assets.New(newFS(), assets.WithDev(true))
	if got := a.URL("app.css"); got != "/static/app.css" {
		t.Fatalf("dev URL = %q, want unhashed", got)
	}
	if got := a.Integrity("app.css"); got != "" {
		t.Fatalf("dev Integrity = %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/assets/ -run 'TestURL|TestLookup|TestDevMode' -v`
Expected: build failure — `a.URL undefined`.

- [ ] **Step 3: Write the implementation**

`web/assets/fingerprint.go`:
```go
package assets

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"path"
)

// shortHash is the first 8 hex chars of the SHA-256 of data — the filename tag.
func shortHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:8]
}

// sri is the Subresource Integrity value (SHA-384, base64) for data.
func sri(data []byte) string {
	sum := sha512.Sum384(data)
	return "sha384-" + base64.StdEncoding.EncodeToString(sum[:])
}

// injectHash inserts hash before name's extension: app.css → app.<hash>.css.
// A name without an extension gets the hash appended: LICENSE → LICENSE.<hash>.
func injectHash(name, hash string) string {
	ext := path.Ext(name)
	return name[:len(name)-len(ext)] + "." + hash + ext
}
```

`web/assets/manifest.go`:
```go
package assets

import "io/fs"

// buildRuntime walks fsys, fingerprints every file, and returns the
// logical→Entry table plus the served-path→real-name reverse map.
func buildRuntime(fsys fs.FS) (map[string]Entry, map[string]string, error) {
	table := map[string]Entry{}
	reverse := map[string]string{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		hashed := injectHash(p, shortHash(data))
		table[p] = Entry{Path: hashed, Integrity: sri(data), real: p}
		reverse[hashed] = p
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return table, reverse, nil
}
```

Modify `web/assets/assets.go` — add the `load` call at the end of `New` (just before `return a, nil`):
```go
	if err := a.load(c); err != nil {
		return nil, err
	}
	return a, nil
}

// load populates the fingerprint table. Runtime hashing only for now; Task 4
// adds the external-manifest and Reader paths. Dev mode keeps an empty table.
func (a *Assets) load(c config) error {
	if a.dev {
		return nil
	}
	table, reverse, err := buildRuntime(a.fsys)
	if err != nil {
		return err
	}
	a.table, a.reverse = table, reverse
	return nil
}
```

Add the lookup methods to `web/assets/assets.go` (and `import "html/template"`):
```go
// URL returns the mounted URL for a logical asset name. In dev, or for an
// unknown name, it returns the unhashed Prefix+name.
func (a *Assets) URL(name string) string {
	if e, ok := a.table[name]; ok {
		return a.prefix + e.Path
	}
	return a.prefix + name
}

// Integrity returns the SRI hash for a logical name, or "" if unknown or in dev.
func (a *Assets) Integrity(name string) string {
	return a.table[name].Integrity
}

// Lookup returns the Entry for a logical name and whether it is known.
func (a *Assets) Lookup(name string) (Entry, bool) {
	e, ok := a.table[name]
	return e, ok
}

// FuncMap exposes URL and Integrity as html/template funcs "asset" and "sri".
func (a *Assets) FuncMap() template.FuncMap {
	return template.FuncMap{"asset": a.URL, "sri": a.Integrity}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./web/assets/ -run 'TestURL|TestLookup|TestDevMode' -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Format, then commit**

```bash
just fmt ./web/assets/...
git add web/assets/
git commit -m "feat(assets): runtime fingerprinting and URL/Integrity/Lookup"
```

---

### Task 3: Serving core — `ServeHTTP`, immutable vs no-cache, ETag/304, Range, content types

**Files:**
- Create: `web/assets/mime.go`
- Modify: `web/assets/assets.go` (`ServeHTTP` + serve helpers)
- Modify: `web/assets/options.go` (`WithCacheControl`)
- Test: `web/assets/serve_test.go`

**Interfaces:**
- Consumes: `Assets`, `table`, `reverse`, `URL` (Tasks 1–2).
- Produces:
  - `func contentType(name string) string`.
  - `func (a *Assets) ServeHTTP(w http.ResponseWriter, r *http.Request)`.
  - `func (a *Assets) serveFingerprinted(w http.ResponseWriter, r *http.Request, served, real string)`.
  - `func (a *Assets) servePlain(w http.ResponseWriter, r *http.Request, name string)`.
  - `func (a *Assets) serveFile(w http.ResponseWriter, r *http.Request, name string)` (streams a file; Task 5 adds the precompress hook).
  - `func fileExists(fsys fs.FS, name string) bool`; `func statTime(fsys fs.FS, name string) time.Time`.
  - `WithCacheControl(immutable, revalidate string) Option`.

- [ ] **Step 1: Write the failing test**

`web/assets/serve_test.go`:
```go
package assets_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/web/assets"
)

func get(t *testing.T, a *assets.Assets, target string, hdr http.Header) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, target, nil)
	for k, vs := range hdr {
		r.Header[k] = vs
	}
	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, r)
	return rec
}

func TestServeFingerprintedImmutable(t *testing.T) {
	a, _ := assets.New(newFS())
	url := a.URL("app.css")
	rec := get(t, a, url, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("Cache-Control = %q, want immutable", cc)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Fatalf("Content-Type = %q, want text/css", ct)
	}
	if rec.Header().Get("Etag") == "" {
		t.Fatal("missing Etag")
	}
	if body := rec.Body.String(); body != "body{color:red}" {
		t.Fatalf("body = %q", body)
	}
}

func TestServeIfNoneMatch304(t *testing.T) {
	a, _ := assets.New(newFS())
	url := a.URL("app.css")
	etag := get(t, a, url, nil).Header().Get("Etag")
	rec := get(t, a, url, http.Header{"If-None-Match": {etag}})
	if rec.Code != http.StatusNotModified {
		t.Fatalf("code = %d, want 304", rec.Code)
	}
}

func TestServeRange(t *testing.T) {
	a, _ := assets.New(newFS())
	url := a.URL("app.css")
	rec := get(t, a, url, http.Header{"Range": {"bytes=0-3"}})
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("code = %d, want 206", rec.Code)
	}
	if rec.Body.String() != "body" {
		t.Fatalf("range body = %q, want body", rec.Body.String())
	}
}

func TestServePlainNoCache(t *testing.T) {
	a, _ := assets.New(newFS())
	rec := get(t, a, "/static/app.css", nil) // unhashed path → plain branch
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", cc)
	}
	if rec.Header().Get("Etag") == "" {
		t.Fatal("plain response missing Etag")
	}
}

func TestServeMissing404AndTraversal(t *testing.T) {
	a, _ := assets.New(newFS())
	if rec := get(t, a, "/static/nope.js", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing code = %d, want 404", rec.Code)
	}
	if rec := get(t, a, "/static/../secret", nil); rec.Code == http.StatusOK {
		t.Fatal("traversal served a 200")
	}
	if rec := get(t, a, "/elsewhere/x", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("off-prefix code = %d, want 404", rec.Code)
	}
}

func TestServeDevReadsPlain(t *testing.T) {
	a, _ := assets.New(newFS(), assets.WithDev(true))
	rec := get(t, a, "/static/app.css", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("dev serve code=%d cc=%q", rec.Code, rec.Header().Get("Cache-Control"))
	}
}

func TestWithCacheControlOverride(t *testing.T) {
	a, _ := assets.New(newFS(), assets.WithCacheControl("public, max-age=60", "private"))
	rec := get(t, a, a.URL("app.css"), nil)
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=60" {
		t.Fatalf("Cache-Control = %q, want override", cc)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/assets/ -run TestServe -v`
Expected: build failure — `a.ServeHTTP undefined` (Assets is not yet an http.Handler).

- [ ] **Step 3: Write the implementation**

`web/assets/mime.go`:
```go
package assets

import (
	"mime"
	"path"
	"strings"
)

// mimeOverlay corrects or fills content types stdlib mime gets wrong or misses.
var mimeOverlay = map[string]string{
	".js":          "text/javascript; charset=utf-8",
	".mjs":         "text/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".json":        "application/json",
	".map":         "application/json",
	".woff2":       "font/woff2",
	".webmanifest": "application/manifest+json",
	".avif":        "image/avif",
	".wasm":        "application/wasm",
}

// contentType returns the content type for name, or "" to let ServeContent sniff.
func contentType(name string) string {
	ext := strings.ToLower(path.Ext(name))
	if ct, ok := mimeOverlay[ext]; ok {
		return ct
	}
	return mime.TypeByExtension(ext)
}
```

Add `WithCacheControl` to `web/assets/options.go`:
```go
// WithCacheControl overrides the Cache-Control header strings for fingerprinted
// (immutable) and revalidated (plain / index) responses.
func WithCacheControl(immutable, revalidate string) Option {
	return func(cf *config) { cf.immutableCC, cf.revalidateCC = immutable, revalidate }
}
```

Add serving to `web/assets/assets.go` (extend imports: `bytes`, `io`, `net/http`, `path`, `strconv`, `time`):
```go
// ServeHTTP resolves the request under Prefix to a fingerprinted (immutable),
// plain (no-cache), or 404 response. Task 5 adds precompressed siblings; Task 6
// adds SPA fallback.
func (a *Assets) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, a.prefix)
	if rel == r.URL.Path { // request path was not under the prefix
		http.NotFound(w, r)
		return
	}
	name := path.Clean("/" + rel)[1:] // root at "/" before Clean neutralizes traversal
	if name == "" || !fs.ValidPath(name) {
		http.NotFound(w, r)
		return
	}
	if real, ok := a.reverse[name]; ok {
		a.serveFingerprinted(w, r, name, real)
		return
	}
	if fileExists(a.fsys, name) {
		a.servePlain(w, r, name)
		return
	}
	http.NotFound(w, r)
}

func (a *Assets) serveFingerprinted(w http.ResponseWriter, r *http.Request, served, real string) {
	h := w.Header()
	h.Set("Cache-Control", a.immutableCC)
	h.Set("Etag", strconv.Quote(served)) // served name is content-addressed
	if ct := contentType(real); ct != "" {
		h.Set("Content-Type", ct)
	}
	a.serveFile(w, r, real)
}

func (a *Assets) servePlain(w http.ResponseWriter, r *http.Request, name string) {
	data, err := fs.ReadFile(a.fsys, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Cache-Control", a.revalidateCC)
	h.Set("Etag", strconv.Quote(shortHash(data)))
	if ct := contentType(name); ct != "" {
		h.Set("Content-Type", ct)
	}
	http.ServeContent(w, r, name, statTime(a.fsys, name), bytes.NewReader(data))
}

// serveFile streams name via http.ServeContent (Range, If-Range, If-None-Match,
// 304). The caller has already set Content-Type / Cache-Control / Etag.
func (a *Assets) serveFile(w http.ResponseWriter, r *http.Request, name string) {
	f, err := a.fsys.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	modtime := statTime(a.fsys, name)
	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, name, modtime, rs)
		return
	}
	data, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, "read error", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, name, modtime, bytes.NewReader(data))
}

func fileExists(fsys fs.FS, name string) bool {
	f, err := fsys.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	return err == nil && !info.IsDir()
}

func statTime(fsys fs.FS, name string) time.Time {
	if info, err := fs.Stat(fsys, name); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./web/assets/ -run TestServe -v && go test ./web/assets/ -run TestWithCacheControl -v`
Expected: PASS.

- [ ] **Step 5: Format, then commit**

```bash
just fmt ./web/assets/...
git add web/assets/
git commit -m "feat(assets): serving core with immutable/no-cache, ETag, Range, MIME"
```

---

### Task 4: External flat manifest, precedence, and the `Reader` seam

**Files:**
- Modify: `web/assets/manifest.go` (`readFlatManifest`)
- Modify: `web/assets/assets.go` (`load` precedence + `finalizeExternal` + `runtime`)
- Modify: `web/assets/options.go` (`WithManifest`, `WithReader`)
- Test: `web/assets/external_test.go`

**Interfaces:**
- Consumes: `Entry`, `Reader`, `buildRuntime`, `fileExists`, `sri`, serving (Tasks 1–3).
- Produces:
  - `func readFlatManifest(fsys fs.FS, path string) (map[string]Entry, error)` — returns unwrapped `fs.ErrNotExist` when absent; `ErrManifest`-wrapped on malformed.
  - `func (a *Assets) finalizeExternal(table map[string]Entry) error`.
  - `func (a *Assets) runtime() error`.
  - `load` extended to: `reader` > `manifest.json` (absent → runtime) > runtime.
  - `WithManifest(path string) Option`; `WithReader(r Reader) Option`.

- [ ] **Step 1: Write the failing test**

`web/assets/external_test.go`:
```go
package assets_test

import (
	"errors"
	"io/fs"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dmitrymomot/forge/web/assets"
)

// externalFS has hashed files physically present plus a flat manifest.json.
func externalFS(manifest string) fstest.MapFS {
	return fstest.MapFS{
		"app.a1b2c3d4.css": {Data: []byte("body{}")},
		"manifest.json":    {Data: []byte(manifest)},
	}
}

func TestExternalStringManifest(t *testing.T) {
	a, err := assets.New(externalFS(`{"app.css":"app.a1b2c3d4.css"}`))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := a.URL("app.css"); got != "/static/app.a1b2c3d4.css" {
		t.Fatalf("URL = %q, want manifest path", got)
	}
	if got := a.Integrity("app.css"); !strings.HasPrefix(got, "sha384-") {
		t.Fatalf("Integrity = %q, want computed sha384-", got)
	}
	rec := get(t, a, a.URL("app.css"), nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("serve code=%d cc=%q", rec.Code, rec.Header().Get("Cache-Control"))
	}
}

func TestExternalObjectManifestKeepsIntegrity(t *testing.T) {
	a, _ := assets.New(externalFS(`{"app.css":{"file":"app.a1b2c3d4.css","integrity":"sha384-XYZ"}}`))
	if got := a.Integrity("app.css"); got != "sha384-XYZ" {
		t.Fatalf("Integrity = %q, want sha384-XYZ", got)
	}
}

func TestExternalMalformedIsErrManifest(t *testing.T) {
	_, err := assets.New(externalFS(`{not json`))
	if !errors.Is(err, assets.ErrManifest) {
		t.Fatalf("err = %v, want ErrManifest", err)
	}
}

func TestExternalMissingFileIsErrManifest(t *testing.T) {
	_, err := assets.New(externalFS(`{"app.css":"gone.abcd0000.css"}`))
	if !errors.Is(err, assets.ErrManifest) {
		t.Fatalf("err = %v, want ErrManifest", err)
	}
}

func TestManifestAbsentFallsBackToRuntime(t *testing.T) {
	// No manifest.json present → runtime fingerprinting.
	a, err := assets.New(fstest.MapFS{"app.css": {Data: []byte("x")}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := a.URL("app.css"); got == "/static/app.css" {
		t.Fatal("expected runtime hashing, got passthrough")
	}
}

type staticReader map[string]assets.Entry

func (s staticReader) Read(fsys fs.FS) (map[string]assets.Entry, error) {
	return map[string]assets.Entry(s), nil
}

func TestWithReaderWins(t *testing.T) {
	fsys := externalFS(`{"app.css":"app.a1b2c3d4.css"}`)
	a, err := assets.New(fsys, assets.WithReader(staticReader{
		"logo": {Path: "app.a1b2c3d4.css"},
	}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := a.URL("logo"); got != "/static/app.a1b2c3d4.css" {
		t.Fatalf("URL(logo) = %q, want reader entry", got)
	}
	if _, ok := a.Lookup("app.css"); ok {
		t.Fatal("flat manifest should have been ignored when a Reader is set")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/assets/ -run 'TestExternal|TestManifestAbsent|TestWithReader' -v`
Expected: build failure — `assets.WithManifest`/`assets.WithReader` undefined (and behavioral failures).

- [ ] **Step 3: Write the implementation**

Add `readFlatManifest` to `web/assets/manifest.go` (add imports `encoding/json`, `fmt`):
```go
// readFlatManifest parses a flat JSON manifest at path within fsys. Each value
// is either a hashed filename string, or an object {"file","integrity"}. It
// returns an unwrapped fs.ErrNotExist when the file is absent so the caller can
// fall back to runtime fingerprinting, and an ErrManifest-wrapped error when the
// JSON is malformed.
func readFlatManifest(fsys fs.FS, path string) (map[string]Entry, error) {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, err // may be fs.ErrNotExist → caller falls back to runtime
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrManifest, err)
	}
	out := make(map[string]Entry, len(raw))
	for logical, rm := range raw {
		var s string
		if json.Unmarshal(rm, &s) == nil {
			out[logical] = Entry{Path: s}
			continue
		}
		var o struct {
			File      string `json:"file"`
			Integrity string `json:"integrity"`
		}
		if err := json.Unmarshal(rm, &o); err != nil || o.File == "" {
			return nil, fmt.Errorf("%w: entry %q", ErrManifest, logical)
		}
		out[logical] = Entry{Path: o.File, Integrity: o.Integrity}
	}
	return out, nil
}
```

Replace `load` in `web/assets/assets.go` and add `finalizeExternal` + `runtime` (add imports `errors`, `fmt`):
```go
// load populates the fingerprint table by precedence: a custom Reader, else the
// flat manifest.json (absent → runtime), else runtime fingerprinting. Dev mode
// keeps an empty table.
func (a *Assets) load(c config) error {
	if a.dev {
		return nil
	}
	switch {
	case c.reader != nil:
		table, err := c.reader.Read(a.fsys)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrManifest, err)
		}
		return a.finalizeExternal(table)
	case c.cfg.ManifestPath != "":
		table, err := readFlatManifest(a.fsys, c.cfg.ManifestPath)
		if errors.Is(err, fs.ErrNotExist) {
			return a.runtime()
		}
		if err != nil {
			return err
		}
		return a.finalizeExternal(table)
	default:
		return a.runtime()
	}
}

func (a *Assets) runtime() error {
	table, reverse, err := buildRuntime(a.fsys)
	if err != nil {
		return err
	}
	a.table, a.reverse = table, reverse
	return nil
}

// finalizeExternal verifies each entry's hashed file exists in fsys, fills a
// missing Integrity by hashing that file, and builds the reverse map. For an
// external manifest the served path equals the real path.
func (a *Assets) finalizeExternal(table map[string]Entry) error {
	reverse := make(map[string]string, len(table))
	for logical, e := range table {
		if !fileExists(a.fsys, e.Path) {
			return fmt.Errorf("%w: %q → missing file %q", ErrManifest, logical, e.Path)
		}
		if e.Integrity == "" {
			data, err := fs.ReadFile(a.fsys, e.Path)
			if err != nil {
				return fmt.Errorf("%w: %v", ErrManifest, err)
			}
			e.Integrity = sri(data)
		}
		e.real = e.Path
		table[logical] = e
		reverse[e.Path] = e.Path
	}
	a.table, a.reverse = table, reverse
	return nil
}
```

Add options to `web/assets/options.go`:
```go
// WithManifest sets the flat manifest path within the fs.FS (default
// "manifest.json"). Empty disables the external read (always runtime-fingerprint).
func WithManifest(path string) Option { return func(cf *config) { cf.cfg.ManifestPath = path } }

// WithReader supplies a custom manifest Reader (e.g. a Vite adapter). It takes
// precedence over the flat manifest.json.
func WithReader(r Reader) Option { return func(cf *config) { cf.reader = r } }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./web/assets/ -run 'TestExternal|TestManifestAbsent|TestWithReader' -v`
Expected: PASS (6 tests).

- [ ] **Step 5: Format, then commit**

```bash
just fmt ./web/assets/...
git add web/assets/
git commit -m "feat(assets): external flat manifest, precedence, and Reader seam"
```

---

### Task 5: Precompressed siblings (`.br` / `.gz`)

**Files:**
- Create: `web/assets/precompress.go`
- Modify: `web/assets/assets.go` (`serveFingerprinted` calls the precompress hook)
- Modify: `web/assets/options.go` (`WithPrecompressed`)
- Test: `web/assets/precompress_test.go`

**Interfaces:**
- Consumes: `Assets.precompress`, `fileExists`, `serveFingerprinted`, `serveFile` (Tasks 1–4).
- Produces:
  - `func (a *Assets) serveCompressed(w http.ResponseWriter, r *http.Request, name string) bool` — serves a sibling when negotiated; returns true if it handled the response. Sets `Vary: Accept-Encoding` whenever precompression is enabled. Range is unsupported for compressed responses.
  - `WithPrecompressed(encodings ...string) Option` (no args → `{"br","gz"}`).

- [ ] **Step 1: Write the failing test**

`web/assets/precompress_test.go`:
```go
package assets_test

import (
	"net/http"
	"testing"
	"testing/fstest"

	"github.com/dmitrymomot/forge/web/assets"
)

// precompFS ships a manifest asset plus a .br sibling.
func precompFS() fstest.MapFS {
	return fstest.MapFS{
		"app.a1b2c3d4.css":    {Data: []byte("PLAINCSS")},
		"app.a1b2c3d4.css.br": {Data: []byte("BROTLIBYTES")},
		"manifest.json":       {Data: []byte(`{"app.css":"app.a1b2c3d4.css"}`)},
	}
}

func TestPrecompressedServedWhenAccepted(t *testing.T) {
	a, _ := assets.New(precompFS(), assets.WithPrecompressed())
	rec := get(t, a, a.URL("app.css"), http.Header{"Accept-Encoding": {"br"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if enc := rec.Header().Get("Content-Encoding"); enc != "br" {
		t.Fatalf("Content-Encoding = %q, want br", enc)
	}
	if ct := rec.Header().Get("Content-Type"); ct == "" {
		t.Fatal("Content-Type should be the original asset's, not empty")
	}
	if v := rec.Header().Get("Vary"); v != "Accept-Encoding" {
		t.Fatalf("Vary = %q, want Accept-Encoding", v)
	}
	if rec.Body.String() != "BROTLIBYTES" {
		t.Fatalf("body = %q, want sibling bytes", rec.Body.String())
	}
}

func TestPrecompressedSkippedWhenNotAccepted(t *testing.T) {
	a, _ := assets.New(precompFS(), assets.WithPrecompressed())
	rec := get(t, a, a.URL("app.css"), nil)
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("must not set Content-Encoding without Accept-Encoding")
	}
	if rec.Body.String() != "PLAINCSS" {
		t.Fatalf("body = %q, want plain asset", rec.Body.String())
	}
}

func TestPrecompressedSiblingAbsentFallsThrough(t *testing.T) {
	fsys := fstest.MapFS{
		"only.a1b2c3d4.js": {Data: []byte("NOSIBLING")},
		"manifest.json":    {Data: []byte(`{"only.js":"only.a1b2c3d4.js"}`)},
	}
	a, _ := assets.New(fsys, assets.WithPrecompressed())
	rec := get(t, a, a.URL("only.js"), http.Header{"Accept-Encoding": {"br"}})
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("no sibling → must not set Content-Encoding")
	}
	if rec.Body.String() != "NOSIBLING" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestPrecompressedGzToken(t *testing.T) {
	fsys := fstest.MapFS{
		"a.a1b2c3d4.js":    {Data: []byte("X")},
		"a.a1b2c3d4.js.gz": {Data: []byte("GZ")},
		"manifest.json":    {Data: []byte(`{"a.js":"a.a1b2c3d4.js"}`)},
	}
	a, _ := assets.New(fsys, assets.WithPrecompressed())
	rec := get(t, a, a.URL("a.js"), http.Header{"Accept-Encoding": {"gzip"}})
	if enc := rec.Header().Get("Content-Encoding"); enc != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", enc)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/assets/ -run TestPrecompressed -v`
Expected: build failure — `assets.WithPrecompressed` undefined.

- [ ] **Step 3: Write the implementation**

`web/assets/precompress.go`:
```go
package assets

import (
	"io/fs"
	"net/http"
	"strconv"
	"strings"
)

// serveCompressed serves a precompressed sibling of name when precompression is
// enabled and the client accepts an encoding whose sibling exists. It returns
// true when it wrote the response. Range is intentionally unsupported for
// compressed responses; the caller has already set Content-Type / Cache-Control
// / Etag. Vary: Accept-Encoding is added whenever precompression is enabled.
func (a *Assets) serveCompressed(w http.ResponseWriter, r *http.Request, name string) bool {
	if len(a.precompress) == 0 {
		return false
	}
	w.Header().Add("Vary", "Accept-Encoding")
	accept := r.Header.Get("Accept-Encoding")
	for _, enc := range a.precompress {
		if !strings.Contains(accept, encodingToken(enc)) && !strings.Contains(accept, enc) {
			continue
		}
		sibling := name + "." + enc
		data, err := fs.ReadFile(a.fsys, sibling)
		if err != nil {
			continue
		}
		h := w.Header()
		h.Set("Content-Encoding", encodingToken(enc))
		h.Del("Accept-Ranges")
		if inm := r.Header.Get("If-None-Match"); inm != "" && h.Get("Etag") != "" && strings.Contains(inm, h.Get("Etag")) {
			w.WriteHeader(http.StatusNotModified)
			return true
		}
		h.Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(data)
		}
		return true
	}
	return false
}

// encodingToken maps a sibling extension to its Content-Encoding token.
func encodingToken(enc string) string {
	if enc == "gz" {
		return "gzip"
	}
	return enc // "br", "zstd", …
}
```

Insert the hook into `serveFingerprinted` in `web/assets/assets.go`, after headers are set and before `a.serveFile`:
```go
func (a *Assets) serveFingerprinted(w http.ResponseWriter, r *http.Request, served, real string) {
	h := w.Header()
	h.Set("Cache-Control", a.immutableCC)
	h.Set("Etag", strconv.Quote(served))
	if ct := contentType(real); ct != "" {
		h.Set("Content-Type", ct)
	}
	if a.serveCompressed(w, r, real) {
		return
	}
	a.serveFile(w, r, real)
}
```

Add the option to `web/assets/options.go`:
```go
// WithPrecompressed serves <file>.<enc> siblings for the given Accept-Encoding
// tokens (default "br","gz"). Call with no args to use the defaults. forge never
// compresses anything itself — the sibling must already exist in the fs.FS.
func WithPrecompressed(encodings ...string) Option {
	return func(cf *config) {
		if len(encodings) == 0 {
			encodings = []string{"br", "gz"}
		}
		cf.precompress = encodings
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./web/assets/ -run TestPrecompressed -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Format, then commit**

```bash
just fmt ./web/assets/...
git add web/assets/
git commit -m "feat(assets): serve precompressed .br/.gz siblings"
```

---

### Task 6: SPA fallback

**Files:**
- Create: `web/assets/spa.go`
- Modify: `web/assets/assets.go` (`ServeHTTP` fallback before the final 404)
- Modify: `web/assets/options.go` (`WithSPA`)
- Test: `web/assets/spa_test.go`

**Interfaces:**
- Consumes: `Assets.spaIndex`, `Assets.spaWhen`, `fileExists`, `statTime`, `contentType` (Tasks 1–3).
- Produces:
  - `func DefaultSPAWhen(r *http.Request) bool` — GET/HEAD and (no extension OR `Accept: text/html`).
  - `func (a *Assets) serveSPA(w http.ResponseWriter, r *http.Request)`.
  - `WithSPA(index string, when ...func(*http.Request) bool) Option`.

- [ ] **Step 1: Write the failing test**

`web/assets/spa_test.go`:
```go
package assets_test

import (
	"net/http"
	"testing"

	"github.com/dmitrymomot/forge/web/assets"
)

func TestSPAServesIndexForNavigation(t *testing.T) {
	a, _ := assets.New(newFS(), assets.WithSPA("index.html"))
	rec := get(t, a, "/static/dashboard/settings", nil) // no extension
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache (never immutable)", cc)
	}
	if !contains(rec.Body.String(), "<title>x</title>") {
		t.Fatalf("body = %q, want index.html", rec.Body.String())
	}
}

func TestSPADoesNotHideMissingAsset(t *testing.T) {
	a, _ := assets.New(newFS(), assets.WithSPA("index.html"))
	rec := get(t, a, "/static/typo.js", http.Header{"Accept": {"*/*"}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing asset code = %d, want 404", rec.Code)
	}
}

func TestSPAAcceptHTMLWithExtension(t *testing.T) {
	a, _ := assets.New(newFS(), assets.WithSPA("index.html"))
	rec := get(t, a, "/static/some.thing", http.Header{"Accept": {"text/html"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 (Accept text/html falls back)", rec.Code)
	}
}

func TestSPAIgnoresNonGET(t *testing.T) {
	a, _ := assets.New(newFS(), assets.WithSPA("index.html"))
	r := httptestRequest(http.MethodPost, "/static/dashboard")
	rec := recorderFor(a, r)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST code = %d, want 404", rec.Code)
	}
}

func TestSPAOffByDefault(t *testing.T) {
	a, _ := assets.New(newFS())
	rec := get(t, a, "/static/dashboard", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("no WithSPA → code = %d, want 404", rec.Code)
	}
}

func TestSPACustomPredicate(t *testing.T) {
	never := func(*http.Request) bool { return false }
	a, _ := assets.New(newFS(), assets.WithSPA("index.html", never))
	rec := get(t, a, "/static/dashboard", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("custom never-predicate code = %d, want 404", rec.Code)
	}
}
```

Add these test helpers to `web/assets/spa_test.go`:
```go
import (
	"net/http/httptest"
	"strings"
)

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func httptestRequest(method, target string) *http.Request {
	return httptest.NewRequest(method, target, nil)
}

func recorderFor(a *assets.Assets, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, r)
	return rec
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/assets/ -run TestSPA -v`
Expected: build failure — `assets.WithSPA` undefined.

- [ ] **Step 3: Write the implementation**

`web/assets/spa.go`:
```go
package assets

import (
	"bytes"
	"net/http"
	"path"
	"strings"
)

// DefaultSPAWhen is the fallback predicate installed by WithSPA: fall back to the
// index for GET/HEAD requests that look like app navigations — no file extension,
// or an explicit Accept: text/html. A request for a concrete asset (extension +
// non-HTML Accept, e.g. a <script>) returns 404 so missing-asset bugs stay
// visible.
func DefaultSPAWhen(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	return path.Ext(r.URL.Path) == "" || strings.Contains(r.Header.Get("Accept"), "text/html")
}

// serveSPA serves the configured index with no-cache (never immutable — it is the
// entry point that references the fingerprinted assets and must stay fresh).
func (a *Assets) serveSPA(w http.ResponseWriter, r *http.Request) {
	data, err := readFileFS(a.fsys, a.spaIndex)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Cache-Control", a.revalidateCC)
	if ct := contentType(a.spaIndex); ct != "" {
		h.Set("Content-Type", ct)
	}
	http.ServeContent(w, r, a.spaIndex, statTime(a.fsys, a.spaIndex), bytes.NewReader(data))
}
```

Add the small `readFileFS` wrapper to `web/assets/assets.go` (keeps spa.go import list minimal; or inline `fs.ReadFile` and import `io/fs` in spa.go — either is fine, pick one and stay consistent):
```go
func readFileFS(fsys fs.FS, name string) ([]byte, error) { return fs.ReadFile(fsys, name) }
```

Wire the fallback into `ServeHTTP` in `web/assets/assets.go`, replacing the final `http.NotFound(w, r)`:
```go
	if a.spaIndex != "" && a.spaWhen != nil && a.spaWhen(r) && fileExists(a.fsys, a.spaIndex) {
		a.serveSPA(w, r)
		return
	}
	http.NotFound(w, r)
}
```

Add the option to `web/assets/options.go`:
```go
// WithSPA enables single-page-app fallback: an unmatched request that the
// predicate accepts serves index (with no-cache). The default predicate is
// DefaultSPAWhen; pass your own to override.
func WithSPA(index string, when ...func(*http.Request) bool) Option {
	return func(cf *config) {
		cf.spaIndex = index
		if len(when) > 0 && when[0] != nil {
			cf.spaWhen = when[0]
		} else {
			cf.spaWhen = DefaultSPAWhen
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./web/assets/ -run TestSPA -v`
Expected: PASS (6 tests).

- [ ] **Step 5: Format, then commit**

```bash
just fmt ./web/assets/...
git add web/assets/
git commit -m "feat(assets): opt-in SPA fallback with smart default predicate"
```

---

### Task 7: `doc.go`, runnable example, and full verification

**Files:**
- Create: `web/assets/doc.go`
- Create: `web/assets/example_test.go`
- Modify: `docs/packages.md` (delete the `web/assets` entry)

**Interfaces:**
- Consumes: the full public API (Tasks 1–6).
- Produces: package documentation + a runnable `ExampleNew`.

- [ ] **Step 1: Write the runnable example (this is the test)**

`web/assets/example_test.go`:
```go
package assets_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing/fstest"

	"github.com/dmitrymomot/forge/web/assets"
)

func ExampleNew() {
	// In a real app: //go:embed static; embed.FS. Here, an in-memory fs.FS.
	fsys := fstest.MapFS{"app.css": {Data: []byte("body{color:red}")}}

	a, err := assets.New(fsys, assets.WithSPA("index.html"))
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	mux.Handle(a.Prefix(), a) // serve the fingerprinted tree at /static/

	// Templates resolve logical names to fingerprinted URLs:
	req := httptest.NewRequest(http.MethodGet, a.URL("app.css"), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	fmt.Println("Cache-Control:", rec.Header().Get("Cache-Control"))
	fmt.Println("served:", rec.Code == http.StatusOK)
	// Output:
	// Cache-Control: public, max-age=31536000, immutable
	// served: true
}
```

- [ ] **Step 2: Run the example to verify it fails**

Run: `go test ./web/assets/ -run ExampleNew -v`
Expected: FAIL — no `doc.go` package comment yet is fine, but confirm the example itself runs; if the package already builds it should PASS. If it PASSES, proceed (the example is the deliverable).

- [ ] **Step 3: Write `doc.go`**

`web/assets/doc.go`:
```go
// Package assets serves a fingerprinted static file tree from an fs.FS with the
// caching and correctness a production app needs: right content types, Range
// requests, ETag/304, and content-fingerprinted URLs that carry a far-future
// immutable Cache-Control header.
//
// One *Assets is one fs.FS mounted at one URL prefix (default "/static/"). It is
// an http.Handler and also resolves logical asset names to fingerprinted URLs
// (URL) and Subresource Integrity hashes (Integrity), so templates reference
// "app.css" and get "/static/app.a1b2c3d4.css".
//
// The fingerprint table is built once at New by one of three paths:
//
//   - a custom manifest Reader (WithReader), for a bundler forge does not read;
//   - a flat manifest.json in the fs.FS ({"app.css":"app.a1b2c3d4.css"} or
//     {"app.css":{"file":"…","integrity":"…"}}), emitted by a build tool;
//   - runtime fingerprinting — walk and hash the fs.FS at startup, no build step.
//
// A missing manifest.json falls back to runtime fingerprinting; a malformed one
// fails New with ErrManifest. WithDev(true) skips the table entirely: unhashed
// URLs, no-cache, and per-request re-reads so edits to an os.DirFS show live.
//
// Serving resolves each request under the prefix to a fingerprinted file
// (immutable), a plain file (no-cache, revalidated by ETag), an opportunistic
// precompressed sibling (WithPrecompressed, serving a build-emitted app.<h>.css.br
// when present and accepted), an SPA index (WithSPA), or 404.
//
// # Not a bundler
//
// assets never transpiles, minifies, tree-shakes, or resolves an import graph,
// and runtime mode does not rewrite intra-asset references (url(...) in CSS,
// import paths in JS). If assets reference each other by hashed name, use an
// external manifest whose bundler already rewrote them. Dynamic compression is
// web/compress; assets only serves precompressed siblings that already exist.
//
// # Usage
//
//	//go:embed static
//	var staticFS embed.FS
//
//	a, err := assets.New(staticFS, assets.WithSPA("index.html"))
//	if err != nil {
//		log.Fatal(err)
//	}
//	mux.Handle(a.Prefix(), a)
//	tpl.Funcs(a.FuncMap()) // {{ asset "app.css" }} / {{ sri "app.css" }}
package assets
```

- [ ] **Step 4: Delete the roadmap entry**

In `docs/packages.md`, remove the `web/assets` block (the `**web/assets**` heading and its paragraph, plus one adjacent `---` separator) — a shipped package is no longer roadmap.

- [ ] **Step 5: Full verification**

Run: `just fmt ./web/assets/...`
Run: `just lint`
Expected: no vet/build/golangci/betteralign/nilaway errors.
Run: `just test ./web/assets/...`
Expected: PASS with coverage; `-race` clean.

- [ ] **Step 6: Commit**

```bash
git add web/assets/ docs/packages.md
git commit -m "docs(assets): package doc, runnable example; drop roadmap entry"
```

---

## Self-Review

**Spec coverage** (each spec section → task):
- Purpose / idiom / `New(...Option)` → Task 1.
- Content fingerprinting (runtime) + `URL`/`Integrity`/`Lookup`/`FuncMap` → Task 2.
- Content types, Range, ETag/304, immutable vs no-cache, traversal safety, dev serve → Task 3.
- External flat manifest (string + object forms), integrity fill, precedence (Reader > manifest.json > runtime), absent→runtime, malformed/missing-file→`ErrManifest`, `Reader` seam → Task 4.
- Precompressed siblings + `Vary` + Range skip → Task 5.
- SPA fallback (opt-in, smart default predicate, override, index no-cache) → Task 6.
- `doc.go` non-goals, runnable example, roadmap deletion, lint/test gate → Task 7.
- `Config` env tags (`ASSETS_PREFIX/MANIFEST/DEV`), `Validate`, `ErrInvalidConfig` → Task 1.

**Placeholder scan:** no TBD/TODO; every code and test step is concrete.

**Type consistency:** `Entry{Path, Integrity, real}`, `Reader.Read(fs.FS) (map[string]Entry, error)`, `shortHash`/`sri`/`injectHash`, `buildRuntime`, `readFlatManifest`, `finalizeExternal`/`runtime`/`load`, `serveFingerprinted`/`servePlain`/`serveFile`/`serveCompressed`/`serveSPA`, `contentType`, `fileExists`/`statTime`, `DefaultSPAWhen`, and the option set are named identically across every task that references them.

**Note on `load` evolution:** Task 2 introduces `load` as runtime-only; Task 4 deliberately replaces its body with the full precedence switch and extracts `runtime()`. This is the one intentional rewrite — called out so a task-by-task implementer expects it.
