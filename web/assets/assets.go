package assets

import (
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
	return a, nil
}

// Prefix returns the normalized URL mount prefix (always trailing-slashed).
func (a *Assets) Prefix() string { return a.prefix }
