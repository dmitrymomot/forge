package httpserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
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
func resolveLogger(l *slog.Logger) *slog.Logger {
	if l == nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return l
}

// Run serves until ctx is cancelled or serving fails, then drains gracefully.
// It validates first (joined ErrInvalidConfig / ErrNoHandler) and does no I/O on
// failure. On ctx cancel it calls Shutdown within ShutdownTimeout; if that deadline
// passes it cancels the request base context (best-effort "last call") and force-
// closes remaining connections, returning ErrShutdownTimeout. A serve error that
// races with cancellation is always surfaced, never masked as a clean stop.
// http.ErrServerClosed is treated as a clean stop (nil).
func (s *Server) Run(ctx context.Context) error {
	allErrs := s.cfg.errs
	if e := s.cfg.Validate(); e != nil {
		allErrs = append(allErrs, e)
	}
	if s.cfg.handler == nil {
		allErrs = append(allErrs, ErrNoHandler)
	}
	if len(allErrs) > 0 {
		return errors.Join(allErrs...)
	}

	log := resolveLogger(s.cfg.logger)

	srv := &http.Server{
		Handler:           s.cfg.handler,
		ReadHeaderTimeout: s.cfg.ReadHeaderTimeout,
		ReadTimeout:       s.cfg.ReadTimeout,
		WriteTimeout:      s.cfg.WriteTimeout,
		IdleTimeout:       s.cfg.IdleTimeout,
		MaxHeaderBytes:    s.cfg.MaxHeaderBytes,
		TLSConfig:         s.cfg.tlsConfig,
		ConnState:         s.cfg.connState,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelError),
	}

	// Base context for every request, rooted at the caller's base (or Background) —
	// NOT at ctx, so requests are not aborted when shutdown begins. Cancelled only
	// at the force-close step.
	base := context.Background()
	if s.cfg.baseContext != nil {
		base = s.cfg.baseContext()
	}
	baseCtx, baseCancel := context.WithCancel(base)
	defer baseCancel()
	srv.BaseContext = func(net.Listener) context.Context { return baseCtx }

	ln := s.cfg.listener
	if ln == nil {
		var err error
		ln, err = net.Listen("tcp", s.cfg.Addr)
		if err != nil {
			return err
		}
	}
	log.Info("http server listening", slog.String("addr", ln.Addr().String()))

	serveErr := make(chan error, 1)
	go func() {
		switch {
		case s.cfg.tlsConfig != nil:
			// In-memory config wins; pass empty paths so ServeTLS keeps its certs.
			serveErr <- srv.ServeTLS(ln, "", "")
		case s.cfg.TLSCertFile != "" && s.cfg.TLSKeyFile != "":
			serveErr <- srv.ServeTLS(ln, s.cfg.TLSCertFile, s.cfg.TLSKeyFile)
		default:
			serveErr <- srv.Serve(ln)
		}
	}()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	return s.drain(srv, serveErr, baseCancel, log)
}

// drain performs graceful shutdown: it waits for in-flight requests up to
// ShutdownTimeout, then on timeout cancels the request base context (best-effort
// "last call") and force-closes remaining connections. It always reads the serve
// result so a real (non-ErrServerClosed) error that raced with cancellation is
// surfaced rather than masked as a clean stop.
func (s *Server) drain(srv *http.Server, serveErr <-chan error, baseCancel context.CancelFunc, log *slog.Logger) error {
	log.Info("http server shutting down")
	shutCtx := context.Background()
	if s.cfg.ShutdownTimeout > 0 {
		var cancel context.CancelFunc
		shutCtx, cancel = context.WithTimeout(shutCtx, s.cfg.ShutdownTimeout)
		defer cancel()
	}
	shutErr := srv.Shutdown(shutCtx)
	if shutErr != nil {
		log.Error("graceful shutdown timed out, forcing close")
		baseCancel()
		_ = srv.Close()
	}

	// Always read the serve result; a real (non-ErrServerClosed) error wins.
	serveResult := <-serveErr
	if serveResult != nil && !errors.Is(serveResult, http.ErrServerClosed) {
		return serveResult
	}
	if shutErr != nil {
		return ErrShutdownTimeout
	}
	log.Info("http server stopped")
	return nil
}
