package internal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// runtimeConfig holds configuration for running the HTTP server.
type runtimeConfig struct {
	handler         http.Handler
	baseCtx         context.Context
	logger          *slog.Logger
	address         string
	startupHooks    []func(context.Context) error
	shutdownHooks   []func(context.Context) error
	shutdownTimeout time.Duration
}

// runServer starts the HTTP server and blocks until shutdown.
// This is the shared implementation for both app.Run() and forge.Run().
func runServer(cfg runtimeConfig) error {
	if cfg.address == "" {
		cfg.address = ":8080"
	}
	if cfg.shutdownTimeout == 0 {
		cfg.shutdownTimeout = defaultShutdownTimeout
	}

	logger := cfg.logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	server := &http.Server{
		Addr:              cfg.address,
		Handler:           cfg.handler,
		ReadTimeout:       defaultReadTimeout,
		WriteTimeout:      defaultWriteTimeout,
		IdleTimeout:       defaultIdleTimeout,
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		MaxHeaderBytes:    defaultMaxHeaderBytes,
	}

	baseCtx := cfg.baseCtx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	ctx, cancel := signal.NotifyContext(baseCtx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	ln, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return err
	}

	// Execute startup hooks before serving requests
	for _, hook := range cfg.startupHooks {
		if err := hook(ctx); err != nil {
			ln.Close()
			return fmt.Errorf("startup hook failed: %w", err)
		}
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("server starting", slog.String("address", ln.Addr().String()))
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down server")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.shutdownTimeout)
	defer shutdownCancel()

	var errs []error

	if err := server.Shutdown(shutdownCtx); err != nil {
		errs = append(errs, err)
	}

	for _, hook := range cfg.shutdownHooks {
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
