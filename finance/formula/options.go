package formula

import "github.com/dmitrymomot/forge/core/decimal"

// Func is a registered Go function backing a func stage: it receives the
// resolved arg values in spec order and returns the stage's raw value. It must
// be pure and deterministic — same args, same result — or recomputed
// statements stop byte-matching. Per-deal parameters (rates, thresholds) are
// closed over at registration; only metric values flow through args.
type Func func(args []decimal.Decimal) (decimal.Decimal, error)

type namedFunc struct {
	fn   Func
	name string
}

type config struct {
	funcs []namedFunc
}

// Option configures Compile.
type Option func(*config)

// WithFunc registers fn under name for func stages to reference. Registering
// an empty name, a nil fn, or the same name twice fails Compile with
// ErrInvalidFunc.
func WithFunc(name string, fn Func) Option {
	return func(c *config) {
		c.funcs = append(c.funcs, namedFunc{name: name, fn: fn})
	}
}
