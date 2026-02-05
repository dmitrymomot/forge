package middlewares

import (
	"runtime"

	"github.com/dmitrymomot/forge/internal"
)

// DefaultStackSize is the default maximum stack trace size in bytes.
const DefaultStackSize = 4096

// RecoverConfig configures the recover middleware.
type RecoverConfig struct {
	StackSize         int  // Max stack trace size (default: 4096)
	DisablePrintStack bool // Disable stack trace in logs
}

// RecoverOption configures RecoverConfig.
type RecoverOption func(*RecoverConfig)

// WithRecoverStackSize sets the maximum stack trace size.
func WithRecoverStackSize(size int) RecoverOption {
	return func(cfg *RecoverConfig) {
		cfg.StackSize = size
	}
}

// WithRecoverDisablePrintStack disables including stack trace in logs.
func WithRecoverDisablePrintStack() RecoverOption {
	return func(cfg *RecoverConfig) {
		cfg.DisablePrintStack = true
	}
}

// Recover returns middleware that recovers from panics.
// It logs the panic and returns a PanicError to be handled by the global ErrorHandler.
// Request ID is automatically included via RequestIDExtractor() if configured.
func Recover(opts ...RecoverOption) internal.Middleware {
	cfg := &RecoverConfig{
		StackSize: DefaultStackSize,
	}

	for _, opt := range opts {
		opt(cfg)
	}

	return func(next internal.HandlerFunc) internal.HandlerFunc {
		return func(c internal.Context) (err error) {
			defer func() {
				if r := recover(); r != nil {
					var stack []byte
					// Allocate buffer only if stack traces are enabled to avoid unnecessary memory allocation
					if !cfg.DisablePrintStack {
						stack = make([]byte, cfg.StackSize)
						n := runtime.Stack(stack, false)
						stack = stack[:n]
					}

					if cfg.DisablePrintStack {
						c.LogError("panic recovered", "panic", r)
					} else {
						c.LogError("panic recovered", "panic", r, "stack", string(stack))
					}

					err = &PanicError{
						Value: r,
						Stack: stack,
					}
				}
			}()

			return next(c)
		}
	}
}
