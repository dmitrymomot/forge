package middlewares

import (
	"runtime"

	"github.com/dmitrymomot/forge/internal"
)

const defaultStackSize = 4096

// RecoverConfig configures the recover middleware.
type RecoverConfig struct {
	StackSize         int  `env:"STACK_SIZE"          envDefault:"4096"`
	DisablePrintStack bool `env:"DISABLE_PRINT_STACK" envDefault:"false"`
}

// Recover returns middleware that recovers from panics.
// It logs the panic and returns a PanicError to be handled by the global ErrorHandler.
// Request ID is automatically included via RequestIDExtractor() if configured.
func Recover(cfg RecoverConfig) internal.Middleware {
	if cfg.StackSize <= 0 {
		cfg.StackSize = defaultStackSize
	}

	return func(next internal.HandlerFunc) internal.HandlerFunc {
		return func(c internal.Context) (err error) {
			defer func() {
				if r := recover(); r != nil {
					var stack []byte
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
