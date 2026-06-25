package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// config holds resolved settings for a single Run call.
type config struct {
	logger   *slog.Logger
	services []Service
	errs     []error
	Config
}

func defaultConfig() config {
	return config{
		Config: DefaultConfig(),
		logger: slog.Default(),
	}
}

// Option configures a Run call: it registers services and tunes behavior.
type Option func(*config)

// WithService registers a Service to be supervised. A nil Service is rejected and
// surfaced by Run as ErrInvalidConfig.
func WithService(svc Service) Option {
	return func(c *config) {
		if svc == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithService received a nil Service", ErrInvalidConfig))
			return
		}
		c.services = append(c.services, svc)
	}
}

// WithServiceFunc registers a named function as a service. name must be non-empty;
// a nil func is rejected and surfaced by Run as ErrInvalidConfig.
func WithServiceFunc(name string, fn func(ctx context.Context) error) Option {
	return func(c *config) {
		if fn == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithServiceFunc(%q) received a nil func", ErrInvalidConfig, name))
			return
		}
		c.services = append(c.services, serviceFunc{name: name, fn: fn})
	}
}

// WithConfig sets the whole serializable data block at once. Build the argument
// from DefaultConfig(); a bare Config{} sets ShutdownTimeout=0 (wait indefinitely)
// and Recover=false (disables panic recovery).
func WithConfig(cfg Config) Option {
	return func(c *config) { c.Config = cfg }
}

// WithShutdownTimeout bounds how long Run waits for services to drain after
// shutdown begins. Default 30s. A value of 0 means wait indefinitely.
func WithShutdownTimeout(d time.Duration) Option {
	return func(c *config) { c.ShutdownTimeout = d }
}

// WithLogger sets the slog.Logger used for lifecycle logging. Default
// slog.Default(); passing nil installs a discard handler at Run time.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) { c.logger = l }
}

// WithRecover toggles panic recovery in each service's Run. Default true: a panic
// is converted to an ErrPanic-wrapped error (which triggers shutdown so siblings
// still drain) instead of crashing the process.
func WithRecover(enabled bool) Option {
	return func(c *config) { c.Recover = enabled }
}
