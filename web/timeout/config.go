package timeout

import (
	"fmt"
	"time"
)

// Config is the env-loadable deadline policy.
type Config struct {
	Timeout time.Duration `env:"TIMEOUT_DURATION"`
}

// DefaultConfig returns a 30-second request deadline.
func DefaultConfig() Config { return Config{Timeout: 30 * time.Second} }

// Validate requires a positive Timeout.
func (c Config) Validate() error {
	if c.Timeout <= 0 {
		return fmt.Errorf("%w: Timeout must be > 0, got %v", ErrInvalidConfig, c.Timeout)
	}
	return nil
}
