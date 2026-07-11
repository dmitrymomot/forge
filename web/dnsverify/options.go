package dnsverify

import "time"

type config struct {
	resolver Resolver
	cfg      Config
}

// Option configures a Verifier.
type Option func(*config)

// WithConfig applies an env-loaded Config. Place it before other options so
// they can override individual fields.
func WithConfig(c Config) Option { return func(cf *config) { cf.cfg = c } }

// WithResolver sets the DNS resolver seam (default net.DefaultResolver).
func WithResolver(r Resolver) Option { return func(cf *config) { cf.resolver = r } }

// WithTimeout sets the per-lookup deadline.
func WithTimeout(d time.Duration) Option { return func(cf *config) { cf.cfg.Timeout = d } }

// WithLabel sets the TXT ownership host prefix.
func WithLabel(s string) Option { return func(cf *config) { cf.cfg.Label = s } }

// WithTokenBytes sets the entropy (in bytes) of minted tokens.
func WithTokenBytes(n int) Option { return func(cf *config) { cf.cfg.TokenBytes = n } }
