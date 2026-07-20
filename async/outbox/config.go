package outbox

import (
	"fmt"
	"time"
)

// Config holds the env-loadable relay knobs.
type Config struct {
	BatchSize    int           `env:"OUTBOX_BATCH_SIZE"`
	PollInterval time.Duration `env:"OUTBOX_POLL_INTERVAL"`
	Lease        time.Duration `env:"OUTBOX_LEASE"`
}

// DefaultConfig returns production defaults: 100-row batches, 1s poll, 30s
// claim lease.
func DefaultConfig() Config {
	return Config{BatchSize: 100, PollInterval: time.Second, Lease: 30 * time.Second}
}

// Validate checks the configuration invariants.
func (c Config) Validate() error {
	if c.BatchSize <= 0 {
		return fmt.Errorf("%w: BatchSize must be > 0, got %d", ErrInvalidConfig, c.BatchSize)
	}
	if c.PollInterval <= 0 {
		return fmt.Errorf("%w: PollInterval must be > 0, got %v", ErrInvalidConfig, c.PollInterval)
	}
	if c.Lease <= 0 {
		return fmt.Errorf("%w: Lease must be > 0, got %v", ErrInvalidConfig, c.Lease)
	}
	return nil
}
