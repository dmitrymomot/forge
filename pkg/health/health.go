package health

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"
)

const (
	defaultTimeout = 5 * time.Second

	// StatusHealthy indicates all checks passed.
	StatusHealthy = "healthy"
	// StatusUnhealthy indicates one or more checks failed.
	StatusUnhealthy = "unhealthy"
)

// CheckFunc is the standard health check function signature.
// This matches existing healthcheck closures in pg, redis, cqrs, and jobs packages.
type CheckFunc func(ctx context.Context) error

// Checks is a map of named health check functions.
type Checks map[string]CheckFunc

// Response represents a health check response.
type Response struct {
	Checks map[string]Check `json:"checks,omitempty"`
	Status string           `json:"status"`
}

// Check represents the status of a single health check.
type Check struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// config holds health check configuration.
type config struct {
	logger  *slog.Logger
	timeout time.Duration
}

// Option configures health check behavior.
type Option func(*config)

// WithTimeout sets the timeout for all checks.
func WithTimeout(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.timeout = d
		}
	}
}

// WithLogger sets the logger for error logging.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l != nil {
			c.logger = l
		}
	}
}

// newConfig creates a config with defaults, modified by options.
func newConfig(opts ...Option) *config {
	cfg := &config{
		timeout: defaultTimeout,
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

// runChecks executes all checks in parallel and returns the aggregated result.
func runChecks(ctx context.Context, checks Checks, cfg *config) *Response {
	if len(checks) == 0 {
		return &Response{Status: StatusHealthy}
	}

	ctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		results  = make(map[string]Check, len(checks))
		hasError bool
	)

	for name, check := range checks {
		wg.Add(1)
		go func(name string, check CheckFunc) {
			defer wg.Done()

			result := Check{Status: StatusHealthy}
			if err := check(ctx); err != nil {
				result.Status = StatusUnhealthy
				result.Error = err.Error()
				cfg.logger.WarnContext(ctx, "health check failed",
					slog.String("check", name),
					slog.String("error", err.Error()),
				)
				mu.Lock()
				hasError = true
				mu.Unlock()
			}

			mu.Lock()
			results[name] = result
			mu.Unlock()
		}(name, check)
	}

	wg.Wait()

	status := StatusHealthy
	if hasError {
		status = StatusUnhealthy
	}

	return &Response{
		Status: status,
		Checks: results,
	}
}
