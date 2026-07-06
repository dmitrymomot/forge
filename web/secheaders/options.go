package secheaders

type config struct {
	policy *Policy
	cfg    Config
	nonce  bool
}

// Option configures the middleware.
type Option func(*config)

// WithConfig applies an env-loaded Config. Place it before other options so
// they can override.
func WithConfig(c Config) Option { return func(cf *config) { cf.cfg = c } }

// WithCSP enables the Content-Security-Policy header with the given policy.
func WithCSP(p Policy) Option { return func(cf *config) { cf.policy = &p } }

// WithNonce generates a per-request CSP nonce, appends it to script-src and
// style-src, and exposes it via Nonce(ctx).
func WithNonce() Option { return func(cf *config) { cf.nonce = true } }
