package fingerprint

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/crypto/sign"
	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/web/cookie"
)

// Collector contributes zero or more named components from a request.
type Collector interface {
	Collect(r *http.Request) ([]Component, error)
}

// CollectorFunc adapts a function to a Collector.
type CollectorFunc func(r *http.Request) ([]Component, error)

func (f CollectorFunc) Collect(r *http.Request) ([]Component, error) { return f(r) }

// Fingerprinter assembles components from its collectors, hashes them into a
// Fingerprint, and derives Signals. Build it with New.
type Fingerprinter struct {
	secret  []byte
	signer  *sign.Signer
	cookies *cookie.Codec
	store   cache.Store
	geo     GeoLookup
	ua      UAFamily
	logger  *slog.Logger
	clock   clock.Clock
	cols    []Collector
	cfg     Config
	version int
}

// New validates cfg, builds the HMAC signer and signed-cookie codec, applies
// options, and returns a ready Fingerprinter.
func New(cfg Config, opts ...Option) (*Fingerprinter, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	signer, err := sign.New([]byte(cfg.Secret))
	if err != nil {
		return nil, err
	}
	// cookie.Config.Keys expects keyset.WithBase64Keys format ("version:base64,...")
	// but Config.Secret is a plain string. Wrap it under a fixed key version: this
	// Config has no rotation support (a single Secret field), so the version only
	// needs to satisfy the codec's parser, not track cfg.Version (the fingerprint
	// schema version, an unrelated concept).
	keys := fmt.Sprintf("1:%s", base64.StdEncoding.EncodeToString([]byte(cfg.Secret)))
	cookies, err := cookie.FromConfig(cookie.Config{Keys: keys, SameSite: "lax", HTTPOnly: true, Secure: true})
	if err != nil {
		return nil, err
	}
	fp := &Fingerprinter{
		secret:  []byte(cfg.Secret),
		signer:  signer,
		cookies: cookies,
		logger:  slog.Default(),
		clock:   clock.System(),
		cfg:     cfg,
		version: cfg.Version,
	}
	for _, o := range opts {
		o(fp)
	}
	return fp, nil
}

// FromRequest runs every collector, concatenates their components, and hashes
// the result. A collector error is remembered (returned) but never aborts the
// others — best-effort fingerprinting.
func (fp *Fingerprinter) FromRequest(r *http.Request) (Fingerprint, error) {
	var comps []Component
	var firstErr error
	for _, c := range fp.cols {
		cc, err := c.Collect(r)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		comps = append(comps, cc...)
	}
	hash, parts := combineHash(fp.secret, fp.version, comps)
	return Fingerprint{Version: fp.version, Components: comps, Hash: hash, parts: parts}, firstErr
}
