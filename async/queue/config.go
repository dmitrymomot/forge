package queue

import (
	"fmt"
	"time"
)

// Config holds the env-loadable worker knobs. ClaimBatch == 0 derives the
// per-poll claim budget from free worker slots.
type Config struct {
	Concurrency  int           `env:"QUEUE_CONCURRENCY"`
	PollInterval time.Duration `env:"QUEUE_POLL_INTERVAL"`
	Lease        time.Duration `env:"QUEUE_LEASE"`
	MaxAttempts  int           `env:"QUEUE_MAX_ATTEMPTS"`
	ClaimBatch   int           `env:"QUEUE_CLAIM_BATCH"`
}

// DefaultConfig returns production defaults: 10 workers, 1s poll, 30s lease,
// 25 attempts, claim batch derived from free slots.
func DefaultConfig() Config {
	return Config{Concurrency: 10, PollInterval: time.Second, Lease: 30 * time.Second, MaxAttempts: 25}
}

// Validate checks the configuration invariants.
func (c Config) Validate() error {
	if c.Concurrency <= 0 {
		return fmt.Errorf("%w: Concurrency must be > 0, got %d", ErrInvalidConfig, c.Concurrency)
	}
	if c.PollInterval <= 0 {
		return fmt.Errorf("%w: PollInterval must be > 0, got %v", ErrInvalidConfig, c.PollInterval)
	}
	if c.Lease <= 0 {
		return fmt.Errorf("%w: Lease must be > 0, got %v", ErrInvalidConfig, c.Lease)
	}
	if c.MaxAttempts <= 0 {
		return fmt.Errorf("%w: MaxAttempts must be > 0, got %d", ErrInvalidConfig, c.MaxAttempts)
	}
	if c.ClaimBatch < 0 {
		return fmt.Errorf("%w: ClaimBatch must be >= 0, got %d", ErrInvalidConfig, c.ClaimBatch)
	}
	return nil
}
