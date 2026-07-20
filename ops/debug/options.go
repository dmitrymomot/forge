package debug

import (
	"fmt"
	"log/slog"
	"net"

	"github.com/dmitrymomot/forge/auth/guard"
	"github.com/dmitrymomot/forge/web/middleware"
)

// config holds resolved settings. The embedded Config carries serializable
// data; the remaining fields are non-serializable code values.
type config struct {
	logger   *slog.Logger
	listener net.Listener
	guards   []middleware.Middleware
	errs     []error
	Config
	noAuth bool
}

func newConfig(opts ...Option) config {
	c := config{Config: DefaultConfig()}
	for _, opt := range opts {
		opt(&c)
	}
	return c
}

// Option configures Handler and NewServer. Invalid values accumulate and are
// returned by Run.
type Option func(*config)

// WithConfig sets the whole serializable data block at once. Build the argument
// from DefaultConfig() (or an env-parsed copy); a bare Config{} zeroes the
// timeouts. Options apply in order — place WithConfig before any WithAddr you
// want to take precedence. Handler ignores Config (it has no listen address).
func WithConfig(cfg Config) Option {
	return func(c *config) { c.Config = cfg }
}

// WithAddr sets the listen address (convenience for Config.Addr).
func WithAddr(addr string) Option {
	return func(c *config) { c.Addr = addr }
}

// WithBasicAuth gates the whole surface with guard.BasicAuth against a static
// username→password map (env-sourced via guard.ParseUsers). Panics — via
// guard.BasicAuth — on an empty map or empty username/password entries: a gate
// with no valid credentials is a wiring bug. Accepted guard options: WithRealm,
// WithResponder.
func WithBasicAuth(users map[string]string, opts ...guard.Option) Option {
	return func(c *config) { c.guards = append(c.guards, guard.BasicAuth(users, opts...)) }
}

// WithMiddleware wraps the whole surface with custom middleware (a guard.New
// chain, ipfilter, requestlog, ...). The first middleware is the outermost
// layer. Any middleware registered this way counts as the auth guard for Run's
// non-loopback check — pass only middleware that actually gates access when the
// server binds beyond loopback. A nil middleware is rejected
// (ErrInvalidConfig): Run returns it, Handler panics.
func WithMiddleware(mws ...middleware.Middleware) Option {
	return func(c *config) {
		for _, mw := range mws {
			if mw == nil {
				c.errs = append(c.errs, fmt.Errorf("%w: WithMiddleware received a nil middleware", ErrInvalidConfig))
				continue
			}
			c.guards = append(c.guards, mw)
		}
	}
}

// WithoutAuth explicitly allows serving on a non-loopback address with no auth
// middleware — for deployments where the port is unreachable from outside the
// private network. Without it, Run fails closed with ErrAuthRequired.
func WithoutAuth() Option {
	return func(c *config) { c.noAuth = true }
}

// WithLogger sets the slog.Logger for server lifecycle logging. Default: discard.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) { c.logger = l }
}

// WithListener supplies a pre-bound listener, overriding Addr (for :0 tests,
// socket activation). The non-loopback auth check inspects the listener's
// address. A nil listener is rejected (ErrInvalidConfig).
func WithListener(ln net.Listener) Option {
	return func(c *config) {
		if ln == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithListener received a nil net.Listener", ErrInvalidConfig))
			return
		}
		c.listener = ln
	}
}
