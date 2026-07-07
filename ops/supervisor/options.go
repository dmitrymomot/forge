package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// config holds resolved settings for a single Run call.
type config struct {
	logger             *slog.Logger
	services           []Service
	errs               []error
	preShutdown        []preHook
	preShutdownTimeout time.Duration
	Config
}

func defaultConfig() config {
	return config{
		Config:             DefaultConfig(),
		logger:             slog.Default(),
		preShutdownTimeout: 30 * time.Second,
	}
}

// preHook is a named pre-shutdown callback.
type preHook struct {
	fn   func(context.Context)
	name string
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

// WithPreShutdown registers a hook run after shutdown begins but BEFORE each
// service's context is cancelled — so readiness can flip and load balancers can
// deregister while services still serve. Hooks run concurrently; Run waits for
// them, bounded by WithPreShutdownTimeout. A nil fn is rejected as ErrInvalidConfig.
func WithPreShutdown(name string, fn func(context.Context)) Option {
	return func(c *config) {
		if fn == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithPreShutdown(%q) received a nil func", ErrInvalidConfig, name))
			return
		}
		c.preShutdown = append(c.preShutdown, preHook{name: name, fn: fn})
	}
}

// WithPreShutdownTimeout bounds the pre-shutdown phase. Default 30s; it must
// exceed the longest grace a hook waits internally, or the hook is cut short.
func WithPreShutdownTimeout(d time.Duration) Option {
	return func(c *config) { c.preShutdownTimeout = d }
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

// ContextOption configures NewContext.
type ContextOption func(*contextConfig)

type contextConfig struct {
	parent    context.Context
	forceQuit bool
}

// WithForceQuit makes the second SIGINT/SIGTERM force an immediate os.Exit(130)
// instead of being ignored. The first signal still cancels the returned context for
// graceful drain; the second is the impatient-operator escape hatch. os.Exit
// bypasses deferred cleanup by design.
func WithForceQuit() ContextOption {
	return func(c *contextConfig) { c.forceQuit = true }
}

// WithContext roots the signal context at parent instead of context.Background,
// so cancelling parent triggers the same graceful shutdown a signal would.
// bootstrap uses it to thread main's context; tests use it to shut down without
// sending real signals. The zero/default parent remains context.Background.
func WithContext(parent context.Context) ContextOption {
	return func(c *contextConfig) { c.parent = parent }
}
