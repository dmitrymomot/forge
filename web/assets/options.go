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

// WithCacheControl overrides the Cache-Control header strings for fingerprinted
// (immutable) and revalidated (plain / index) responses.
func WithCacheControl(immutable, revalidate string) Option {
	return func(cf *config) { cf.immutableCC, cf.revalidateCC = immutable, revalidate }
}
