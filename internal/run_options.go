package internal

import (
	"context"
	"log/slog"
	"time"
)

// RunConfig holds externally configurable runtime settings.
type RunConfig struct {
	Address         string        `env:"ADDRESS"          envDefault:":8080"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"30s"`
}

// RunOption configures the server runtime.
type RunOption func(*runConfig)

// runConfig holds runtime configuration for the server.
type runConfig struct {
	baseCtx         context.Context
	logger          *slog.Logger
	domains         map[string]*App
	fallback        *App
	address         string
	startupHooks    []func(context.Context) error
	shutdownHooks   []func(context.Context) error
	shutdownTimeout time.Duration
}

// buildRunConfig creates a runConfig from the provided RunConfig and options.
func buildRunConfig(cfg RunConfig, opts ...RunOption) *runConfig {
	if cfg.Address == "" {
		cfg.Address = ":8080"
	}
	if cfg.ShutdownTimeout == 0 {
		cfg.ShutdownTimeout = defaultShutdownTimeout
	}

	rc := &runConfig{
		domains:         make(map[string]*App),
		address:         cfg.Address,
		shutdownTimeout: cfg.ShutdownTimeout,
	}
	for _, opt := range opts {
		opt(rc)
	}
	return rc
}

// WithRunLogger sets the application logger for the runtime.
// If nil, logging is disabled.
func WithRunLogger(l *slog.Logger) RunOption {
	return func(c *runConfig) {
		if l != nil {
			c.logger = l
		}
	}
}

// WithStartupHook registers a function to run during server startup.
// Hooks are called in the order they were registered, after the port is bound
// but before serving requests. If any hook fails, the server stops and
// returns the error.
func WithStartupHook(fn func(context.Context) error) RunOption {
	return func(c *runConfig) {
		if fn != nil {
			c.startupHooks = append(c.startupHooks, fn)
		}
	}
}

// WithShutdownHook registers a cleanup function to run during shutdown.
// Hooks are called in the order they were registered.
// Each hook receives a context with the shutdown timeout.
func WithShutdownHook(fn func(context.Context) error) RunOption {
	return func(c *runConfig) {
		if fn != nil {
			c.shutdownHooks = append(c.shutdownHooks, fn)
		}
	}
}

// WithDomain maps a host pattern to an App.
// Patterns: "api.example.com" (exact) or "*.example.com" (wildcard)
func WithDomain(pattern string, app *App) RunOption {
	return func(c *runConfig) {
		if pattern != "" && app != nil {
			c.domains[pattern] = app
		}
	}
}

// WithFallback sets the default App for requests that don't match any domain.
// If no domains are configured, the fallback becomes the main handler.
func WithFallback(app *App) RunOption {
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
