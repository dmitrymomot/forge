package debug

import (
	"errors"
	"fmt"
	"time"
)

// Config holds the serializable settings for a Server. The env struct tags are
// inert strings — this package imports no config loader. Populate Config with any
// loader that reads env struct tags, typically by seeding from DefaultConfig and
// parsing the environment over it.
type Config struct {
	Addr              string        `env:"DEBUG_ADDR"`        // listen address; ignored when WithListener is used
	Name              string        `env:"DEBUG_SERVER_NAME"` // empty -> "debug"
	ReadHeaderTimeout time.Duration `env:"DEBUG_READ_HEADER_TIMEOUT"`
	ShutdownTimeout   time.Duration `env:"DEBUG_SHUTDOWN_TIMEOUT"`
}

// DefaultConfig returns the settings NewServer starts from and is the single
// source of truth for defaults. The default address is loopback-only, so a
// default server needs no auth configuration.
func DefaultConfig() Config {
	return Config{
		Addr:              "localhost:6060",
		ReadHeaderTimeout: 10 * time.Second,
		ShutdownTimeout:   5 * time.Second,
	}
}

// Validate reports whether the field values are usable, returning an
// ErrInvalidConfig-wrapped, single-line joined error otherwise. Run calls it
// defensively; callers may call it after loading from env.
func (c Config) Validate() error {
	var errs []error
	if c.Addr == "" {
		errs = append(errs, fmt.Errorf("%w: Addr must not be empty", ErrInvalidConfig))
	}
	if c.ReadHeaderTimeout < 0 {
		errs = append(errs, fmt.Errorf("%w: ReadHeaderTimeout must be >= 0", ErrInvalidConfig))
	}
	if c.ShutdownTimeout < 0 {
		errs = append(errs, fmt.Errorf("%w: ShutdownTimeout must be >= 0", ErrInvalidConfig))
	}
	return errors.Join(errs...)
}
