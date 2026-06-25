package httpserver

import (
	"io"
	"log/slog"
	"net/http"
)

// Server is a single-use HTTP service that satisfies supervisor.Service. After Run
// returns it must not be reused; construct a fresh Server per run.
type Server struct {
	cfg config
}

// New builds a Server. The handler is required and is the only positional argument.
// New does no I/O: the internal config is seeded from DefaultConfig() and each
// option is applied in order, so New(handler) alone is a complete server on every
// default. Binding happens in Run. New never fails; invalid option/Config values
// accumulate and are returned by Run.
func New(handler http.Handler, opts ...Option) *Server {
	cfg := config{
		Config:  DefaultConfig(),
		handler: handler,
		logger:  slog.Default(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Server{cfg: cfg}
}

// Name returns the configured Name, else a name derived from the injected
// listener's address, else "http " + Addr. Satisfies supervisor.Service.
func (s *Server) Name() string {
	if s.cfg.Name != "" {
		return s.cfg.Name
	}
	if s.cfg.listener != nil {
		return "http " + s.cfg.listener.Addr().String()
	}
	return "http " + s.cfg.Addr
}

// resolveLogger returns l, or a discard logger when l is nil.
func resolveLogger(l *slog.Logger) *slog.Logger { //nolint:unused // called by Run in Task 7
	if l == nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return l
}
