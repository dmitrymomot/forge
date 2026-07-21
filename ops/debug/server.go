package debug

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"

	"github.com/dmitrymomot/forge/web/httpserver"
)

// Server serves the diagnostics surface on a dedicated port and satisfies
// supervisor.Service. Each Run builds a fresh underlying HTTP server, but a
// listener injected via WithListener is consumed by the first Run — construct a
// fresh Server per run when using one.
type Server struct {
	handler http.Handler
	cfg     config
}

// NewServer builds a Server serving Handler's surface. New does no I/O; binding
// happens in Run. Invalid option/Config values accumulate and are returned by
// Run — except an invalid WithBasicAuth credentials map, which panics (see
// WithBasicAuth).
func NewServer(opts ...Option) *Server {
	c := newConfig(opts...)
	return &Server{cfg: c, handler: buildHandler(&c)}
}

// Name returns the configured Name, else "debug". Satisfies supervisor.Service.
func (s *Server) Name() string {
	if s.cfg.Name != "" {
		return s.cfg.Name
	}
	return "debug"
}

// Run serves until ctx is cancelled or serving fails, then drains gracefully
// (delegating lifecycle to web/httpserver). It validates first and does no I/O
// on failure; a non-loopback bind with no auth middleware and no WithoutAuth
// fails closed with ErrAuthRequired. WriteTimeout is intentionally disabled on
// the underlying server: CPU profiles and traces stream for ?seconds=N.
func (s *Server) Run(ctx context.Context) error {
	allErrs := slices.Clone(s.cfg.errs)
	if err := s.cfg.Validate(); err != nil {
		allErrs = append(allErrs, err)
	}
	if !s.cfg.noAuth && len(s.cfg.guards) == 0 && !s.loopbackBound() {
		allErrs = append(allErrs, fmt.Errorf("%w: addr %q", ErrAuthRequired, s.bindAddr()))
	}
	if len(allErrs) > 0 {
		return errors.Join(allErrs...)
	}

	cfg := httpserver.DefaultConfig()
	cfg.Addr = s.cfg.Addr
	cfg.Name = s.Name()
	cfg.ReadHeaderTimeout = s.cfg.ReadHeaderTimeout
	cfg.ShutdownTimeout = s.cfg.ShutdownTimeout
	cfg.WriteTimeout = 0

	srvOpts := []httpserver.Option{
		httpserver.WithConfig(cfg),
		httpserver.WithLogger(s.cfg.logger),
	}
	if s.cfg.listener != nil {
		srvOpts = append(srvOpts, httpserver.WithListener(s.cfg.listener))
	}
	return httpserver.New(s.handler, srvOpts...).Run(ctx)
}

// bindAddr reports the address Run would bind, for error messages.
func (s *Server) bindAddr() string {
	if s.cfg.listener != nil {
		return s.cfg.listener.Addr().String()
	}
	return s.cfg.Addr
}

// loopbackBound reports whether the server would bind a loopback-only address.
// Only literal loopback forms pass ("localhost", 127.0.0.0/8, ::1, unix
// sockets); hostnames that merely resolve to loopback fail closed.
func (s *Server) loopbackBound() bool {
	if s.cfg.listener != nil {
		switch a := s.cfg.listener.Addr().(type) {
		case *net.TCPAddr:
			return a.IP.IsLoopback()
		case *net.UnixAddr:
			return true
		default:
			return false
		}
	}
	host, _, err := net.SplitHostPort(s.cfg.Addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
