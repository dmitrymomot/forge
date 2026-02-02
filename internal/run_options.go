package internal

import (
	"context"
	"log/slog"
	"time"
)

// RunOption configures the server runtime.
type RunOption func(*runConfig)

// runConfig holds runtime configuration for the server.
type runConfig struct {
	address         string
	logger          *slog.Logger
	shutdownTimeout time.Duration
	shutdownHooks   []func(context.Context) error
	domains         map[string]*App
	fallback        *App
	baseCtx         context.Context
}

// buildRunConfig creates a runConfig from the provided options.
func buildRunConfig(opts ...RunOption) *runConfig {
	cfg := &runConfig{
		domains:         make(map[string]*App),
		shutdownTimeout: defaultShutdownTimeout,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

// Address sets the HTTP server address.
// Defaults to ":8080".
func Address(addr string) RunOption {
	return func(c *runConfig) {
		if addr != "" {
			c.address = addr
		}
	}
}

// Logger sets the application logger.
// If nil, logging is disabled.
func Logger(l *slog.Logger) RunOption {
	return func(c *runConfig) {
		if l != nil {
			c.logger = l
		}
	}
}

// ShutdownTimeout sets the timeout for graceful shutdown.
// This applies to both the HTTP server and shutdown hooks.
// Defaults to 30 seconds.
func ShutdownTimeout(d time.Duration) RunOption {
	return func(c *runConfig) {
		if d > 0 {
			c.shutdownTimeout = d
		}
	}
}

// ShutdownHook registers a cleanup function to run during shutdown.
// Hooks are called in the order they were registered.
// Each hook receives a context with the shutdown timeout.
//
// Example:
//
//	forge.ShutdownHook(db.Shutdown(pool))
func ShutdownHook(fn func(context.Context) error) RunOption {
	return func(c *runConfig) {
		if fn != nil {
			c.shutdownHooks = append(c.shutdownHooks, fn)
		}
	}
}

// Domain maps a host pattern to an App.
// Patterns: "api.example.com" (exact) or "*.example.com" (wildcard)
//
// Example:
//
//	forge.Run(
//	    forge.Domain("api.acme.com", apiApp),
//	    forge.Domain("*.acme.com", tenantApp),
//	)
func Domain(pattern string, app *App) RunOption {
	return func(c *runConfig) {
		if pattern != "" && app != nil {
			c.domains[pattern] = app
		}
	}
}

// Fallback sets the default App for requests that don't match any domain.
// If no domains are configured, the fallback becomes the main handler.
//
// Example:
//
//	forge.Run(
//	    forge.Domain("api.acme.com", apiApp),
//	    forge.Fallback(landingApp),
//	)
func Fallback(app *App) RunOption {
	return func(c *runConfig) {
		if app != nil {
			c.fallback = app
		}
	}
}

// WithContext sets a custom base context for signal handling.
// Useful for testing or when integrating with existing context hierarchies.
// Defaults to context.Background() if not set.
func WithContext(ctx context.Context) RunOption {
	return func(c *runConfig) {
		if ctx != nil {
			c.baseCtx = ctx
		}
	}
}
