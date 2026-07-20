package collector

import (
	"fmt"
	"time"
)

// Config holds the env-loadable buffering and flushing knobs.
type Config struct {
	BufferSize    int           `env:"COLLECTOR_BUFFER_SIZE"`
	BatchSize     int           `env:"COLLECTOR_BATCH_SIZE"`
	FlushInterval time.Duration `env:"COLLECTOR_FLUSH_INTERVAL"`
	FlushTimeout  time.Duration `env:"COLLECTOR_FLUSH_TIMEOUT"`
}

// DefaultConfig returns production defaults: 8192-event buffer, 512-event
// batches, 1s flush interval, 10s flush timeout.
func DefaultConfig() Config {
	return Config{BufferSize: 8192, BatchSize: 512, FlushInterval: time.Second, FlushTimeout: 10 * time.Second}
}

// Validate checks the configuration invariants.
func (c Config) Validate() error {
	if c.BufferSize <= 0 {
		return fmt.Errorf("%w: BufferSize must be > 0, got %d", ErrInvalidConfig, c.BufferSize)
	}
	if c.BatchSize <= 0 {
		return fmt.Errorf("%w: BatchSize must be > 0, got %d", ErrInvalidConfig, c.BatchSize)
	}
	if c.BatchSize > c.BufferSize {
		return fmt.Errorf("%w: BatchSize must be <= BufferSize, got %d > %d", ErrInvalidConfig, c.BatchSize, c.BufferSize)
	}
	if c.FlushInterval <= 0 {
		return fmt.Errorf("%w: FlushInterval must be > 0, got %v", ErrInvalidConfig, c.FlushInterval)
	}
	if c.FlushTimeout < 0 {
		return fmt.Errorf("%w: FlushTimeout must be >= 0 (0 disables the timeout), got %v", ErrInvalidConfig, c.FlushTimeout)
	}
	return nil
}
