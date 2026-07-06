package cors

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Config is the env-loadable CORS policy.
type Config struct {
	AllowedOrigins   []string      `env:"CORS_ALLOWED_ORIGINS"` // exact origin, "*", or "https://*.example.com"
	AllowedMethods   []string      `env:"CORS_ALLOWED_METHODS"`
	AllowedHeaders   []string      `env:"CORS_ALLOWED_HEADERS"` // empty = echo the preflight request headers
	ExposedHeaders   []string      `env:"CORS_EXPOSED_HEADERS"`
	AllowCredentials bool          `env:"CORS_ALLOW_CREDENTIALS"`
	MaxAge           time.Duration `env:"CORS_MAX_AGE"` // preflight cache lifetime
}

// DefaultConfig allows the common simple methods with no origins — CORS is
// effectively off until origins are configured.
func DefaultConfig() Config {
	return Config{
		AllowedMethods: []string{
			http.MethodGet, http.MethodPost, http.MethodPut,
			http.MethodPatch, http.MethodDelete, http.MethodHead,
		},
	}
}

// Validate rejects the bare-wildcard + credentials vulnerability and
// malformed origin patterns.
func (c Config) Validate() error {
	for _, o := range c.AllowedOrigins {
		if o == "*" {
			if c.AllowCredentials {
				return fmt.Errorf("%w: wildcard origin with credentials", ErrInvalidConfig)
			}
			continue
		}
		if _, err := parseOrigin(o); err != nil {
			return err
		}
	}
	if c.MaxAge < 0 {
		return fmt.Errorf("%w: negative MaxAge", ErrInvalidConfig)
	}
	return nil
}

// originRule is one compiled AllowedOrigins entry.
type originRule struct {
	exact  string // non-empty for exact matches
	scheme string // for wildcard rules
	base   string // for wildcard rules: suffix after "*."
}

func parseOrigin(o string) (originRule, error) {
	scheme, host, ok := strings.Cut(o, "://")
	if !ok || scheme == "" || host == "" || strings.ContainsAny(host, "/ ") {
		return originRule{}, fmt.Errorf("%w: origin %q must be scheme://host[:port]", ErrInvalidConfig, o)
	}
	if !strings.Contains(o, "*") {
		return originRule{exact: o}, nil
	}
	base, isWildcard := strings.CutPrefix(host, "*.")
	if !isWildcard || base == "" || strings.Contains(base, "*") {
		return originRule{}, fmt.Errorf("%w: wildcard origin %q must be scheme://*.domain", ErrInvalidConfig, o)
	}
	return originRule{scheme: scheme, base: base}, nil
}

// match reports whether origin satisfies the rule. Wildcards cover exactly
// one label: https://*.example.com matches https://a.example.com only.
func (r originRule) match(origin string) bool {
	if r.exact != "" {
		return origin == r.exact
	}
	scheme, host, ok := strings.Cut(origin, "://")
	if !ok || scheme != r.scheme {
		return false
	}
	label, base, ok := strings.Cut(host, ".")
	return ok && label != "" && base == r.base
}
