package compress

type config struct {
	types []string
	cfg   Config
}

// Option configures the middleware.
type Option func(*config)

// WithConfig applies an env-loaded Config. Place it first; later options
// override.
func WithConfig(c Config) Option { return func(cf *config) { cf.cfg = c } }

// WithContentTypes replaces the compressible content-type allowlist.
// Entries are exact ("application/json") or family wildcards ("text/*").
func WithContentTypes(types ...string) Option { return func(cf *config) { cf.types = types } }
