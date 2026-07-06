package cors

type config struct {
	originFn func(origin string) bool
	cfg      Config
}

// Option configures the middleware.
type Option func(*config)

// WithConfig applies an env-loaded Config. Place it first; later options
// override.
func WithConfig(c Config) Option { return func(cf *config) { cf.cfg = c } }

// WithOriginFunc replaces AllowedOrigins matching entirely — use it for
// dynamic origins such as DB-backed tenant custom domains.
func WithOriginFunc(fn func(origin string) bool) Option {
	return func(cf *config) { cf.originFn = fn }
}
