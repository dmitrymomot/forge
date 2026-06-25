package supervisor

import (
	"fmt"
	"time"
)

// Config holds the serializable settings for Run. The env struct tags are inert
// strings — this package imports no config loader. Populate Config with any loader
// that reads env struct tags, typically by seeding from DefaultConfig and parsing
// the environment over it.
type Config struct {
	// ShutdownTimeout bounds how long Run waits for services to drain after
	// shutdown begins. 0 means wait indefinitely.
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT"`
	// Recover toggles panic recovery in each service's Run.
	Recover bool `env:"RECOVER"`
}

// DefaultConfig returns the optimal defaults and is the single source of truth for
// them (there are no envDefault tags to drift from it).
func DefaultConfig() Config {
	return Config{
		ShutdownTimeout: 30 * time.Second,
		Recover:         true,
	}
}

// Validate reports whether the field values are usable, returning an
// ErrInvalidConfig-wrapped error otherwise. Callers may call it after loading from
// env (zero-trust); Run also calls it defensively.
func (c Config) Validate() error {
	if c.ShutdownTimeout < 0 {
		return fmt.Errorf("%w: ShutdownTimeout must be >= 0", ErrInvalidConfig)
	}
	return nil
}
