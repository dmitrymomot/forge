package assets

import (
	"html/template"
	"io/fs"
	"net/http"
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
