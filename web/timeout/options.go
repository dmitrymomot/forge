package timeout

import "github.com/dmitrymomot/forge/web/problem"

type config struct {
	responder problem.Responder
	cfg       Config
}

// Option configures the middleware.
type Option func(*config)

// WithConfig applies an env-loaded Config. Place it first; later options
// override.
func WithConfig(c Config) Option { return func(cf *config) { cf.cfg = c } }

// WithResponder overrides the 504 response (default problem.JSON).
func WithResponder(r problem.Responder) Option {
	return func(cf *config) {
		if r != nil {
			cf.responder = r
		}
	}
}
