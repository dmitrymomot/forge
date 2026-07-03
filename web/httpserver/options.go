package httpserver

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
)

// config holds resolved settings for a single Server. The embedded Config carries
// serializable data; the remaining fields are non-serializable code values.
type config struct {
	handler     http.Handler
	listener    net.Listener
	logger      *slog.Logger
	tlsConfig   *tls.Config
	baseContext func() context.Context
	connState   func(net.Conn, http.ConnState)
	errs        []error
	Config
}

// Option configures a Server. Invalid values accumulate and are returned by Run.
type Option func(*config)

// WithConfig sets the whole serializable data block at once. Build the argument
// from DefaultConfig() (or an env-parsed copy); a bare Config{} zeroes the
// timeouts. Options apply in order — place WithConfig before any WithAddr/WithName
// you want to take precedence.
func WithConfig(cfg Config) Option {
	return func(c *config) { c.Config = cfg }
}

// WithAddr sets the listen address (convenience for Config.Addr).
func WithAddr(addr string) Option {
	return func(c *config) { c.Addr = addr }
}

// WithName sets the supervisor.Service name (convenience for Config.Name).
func WithName(name string) Option {
	return func(c *config) { c.Name = name }
}

// WithLogger sets the slog.Logger for lifecycle logging and the net/http ErrorLog
// bridge. Default slog.Default(); nil installs a discard handler at Run time.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) { c.logger = l }
}

// WithListener supplies a pre-bound listener, overriding Addr (for :0 tests, unix
// sockets, socket activation). A nil listener is rejected (ErrInvalidConfig).
func WithListener(ln net.Listener) Option {
	return func(c *config) {
		if ln == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithListener received a nil net.Listener", ErrInvalidConfig))
			return
		}
		c.listener = ln
	}
}

// WithTLSConfig sets an in-memory *tls.Config (mTLS, autocert). It takes precedence
// over Config.TLSCertFile/TLSKeyFile. A nil config is rejected (ErrInvalidConfig).
func WithTLSConfig(tc *tls.Config) Option {
	return func(c *config) {
		if tc == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithTLSConfig received a nil *tls.Config", ErrInvalidConfig))
			return
		}
		c.tlsConfig = tc
	}
}

// WithBaseContext sets the root context for every request. The server layers
// force-close cancellation on top. A nil func is rejected (ErrInvalidConfig).
func WithBaseContext(fn func() context.Context) Option {
	return func(c *config) {
		if fn == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithBaseContext received a nil func", ErrInvalidConfig))
			return
		}
		c.baseContext = fn
	}
}

// WithConnState registers an http.Server.ConnState callback (metrics). A nil func
// is rejected (ErrInvalidConfig).
func WithConnState(fn func(net.Conn, http.ConnState)) Option {
	return func(c *config) {
		if fn == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithConnState received a nil func", ErrInvalidConfig))
			return
		}
		c.connState = fn
	}
}
