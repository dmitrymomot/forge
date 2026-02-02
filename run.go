package forge

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/dmitrymomot/forge/pkg/health"
	"github.com/dmitrymomot/forge/pkg/hostrouter"
)

// Run starts the HTTP server and blocks until shutdown.
// It handles SIGINT and SIGTERM for graceful shutdown.
//
// Returns nil on clean shutdown, or an error if the server
// fails to start or shutdown hooks fail.
func (a *App) Run() error {
	logger := a.logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	// Setup routes
	a.setupRoutes()

	// Create signal-aware context
	baseCtx := a.baseCtx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	ctx, cancel := signal.NotifyContext(baseCtx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Listen first to get actual address
	ln, err := net.Listen("tcp", a.server.Addr)
	if err != nil {
		return err
	}
	a.listener = ln

	// Start HTTP server
	errCh := make(chan error, 1)
	go func() {
		logger.Info("server starting", slog.String("address", ln.Addr().String()))
		if err := a.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	// Wait for shutdown signal, Stop() call, or error
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	case <-a.done:
	}

	// Graceful shutdown
	logger.Info("shutting down server")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), a.shutdownTimeout)
	defer shutdownCancel()

	var errs []error

	// 1. Stop HTTP server
	if err := a.server.Shutdown(shutdownCtx); err != nil {
		errs = append(errs, err)
	}

	// 2. Future: Stop River queue workers
	// 3. Future: Stop River periodic jobs

	// 4. Run shutdown hooks (close DB, etc.)
	for _, hook := range a.shutdownHooks {
		if err := hook(shutdownCtx); err != nil {
			errs = append(errs, err)
			logger.Error("shutdown hook failed", slog.Any("error", err))
		}
	}

	if len(errs) > 0 {
		logger.Error("shutdown completed with errors")
		return errors.Join(errs...)
	}

	logger.Info("shutdown completed")
	return nil
}

// Stop triggers graceful shutdown programmatically.
// Useful for testing or when shutdown needs to be initiated from code.
func (a *App) Stop() error {
	select {
	case <-a.done:
		// Already closed
	default:
		close(a.done)
	}
	return nil
}

// setupRoutes configures the router with middleware and handlers.
func (a *App) setupRoutes() {
	// Set custom error handlers on chi router
	if a.notFoundHandler != nil {
		a.router.NotFound(a.wrapHandler(a.notFoundHandler))
	}
	if a.methodNotAllowedHandler != nil {
		a.router.MethodNotAllowed(a.wrapHandler(a.methodNotAllowedHandler))
	}

	// Apply global middleware
	for _, mw := range a.middlewares {
		a.router.Use(a.adaptMiddleware(mw))
	}

	// Mount static file handlers
	for _, sr := range a.staticRoutes {
		a.router.Mount(sr.pattern, sr.handler)
	}

	// Register health check endpoints
	if a.healthConfig != nil {
		a.router.Get(a.healthConfig.livenessPath, health.LivenessHandler())
		a.router.Get(a.healthConfig.readinessPath, health.ReadinessHandler(a.healthConfig.checks))
	}

	// Register handlers
	r := &routerAdapter{router: a.router, app: a}
	for _, h := range a.handlers {
		h.Routes(r)
	}

	// Wrap with host router if configured
	if len(a.hostRoutes) > 0 {
		a.server.Handler = hostrouter.New(a.hostRoutes, a.router)
	}
}

// wrapHandler converts a HandlerFunc to http.HandlerFunc using the app's error handler.
func (a *App) wrapHandler(h HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := newContext(w, r)
		if err := h(c); err != nil {
			a.handleError(c, err)
		}
	}
}

// handleError handles errors from handlers using the configured error handler.
func (a *App) handleError(c Context, err error) {
	// Check if response has already been written
	if c.Written() {
		return
	}
	if a.errorHandler != nil {
		_ = a.errorHandler(c, err)
	} else {
		http.Error(c.Response(), "Internal Server Error", http.StatusInternalServerError)
	}
}
