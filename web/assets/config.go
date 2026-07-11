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
