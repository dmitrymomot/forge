package supervisor

import (
	"context"
	"log/slog"
	"time"
)

const defaultShutdownTimeout = 30 * time.Second

// config holds resolved settings for a single Run call.
type config struct {
	logger          *slog.Logger
	services        []Service
	shutdownTimeout time.Duration
	recover         bool
}

func defaultConfig() config {
	return config{
		shutdownTimeout: defaultShutdownTimeout,
		logger:          slog.Default(),
		recover:         true,
	}
}

// Option configures a Run call: it registers services and tunes behavior.
type Option func(*config)

// WithService registers a Service to be supervised.
func WithService(svc Service) Option {
	return func(c *config) { c.services = append(c.services, svc) }
}

// WithServiceFunc registers a named function as a service. name must be non-empty.
func WithServiceFunc(name string, fn func(ctx context.Context) error) Option {
	return func(c *config) {
		c.services = append(c.services, serviceFunc{name: name, fn: fn})
	}
}

// WithShutdownTimeout bounds how long Run waits for services to drain after
// shutdown begins. Default 30s. A value of 0 means wait indefinitely.
func WithShutdownTimeout(d time.Duration) Option {
	return func(c *config) { c.shutdownTimeout = d }
}

// WithLogger sets the slog.Logger used for lifecycle logging. Default
// slog.Default(); passing nil installs a discard handler at Run time.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) { c.logger = l }
}

// WithRecover toggles panic recovery in each service's Run. Default true: a
// panic is converted to an ErrPanic-wrapped error (which triggers shutdown so
// siblings still drain) instead of crashing the process.
func WithRecover(enabled bool) Option {
	return func(c *config) { c.recover = enabled }
}
