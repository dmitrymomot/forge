package assets

import (
	"bytes"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
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

// Prefix returns the normalized URL mount prefix (always trailing-slashed).
func (a *Assets) Prefix() string { return a.prefix }

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
	defer func() { _ = f.Close() }()
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
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	return err == nil && !info.IsDir()
}

func statTime(fsys fs.FS, name string) time.Time {
	if info, err := fs.Stat(fsys, name); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}
