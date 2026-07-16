package queue

import (
	"fmt"
	"time"
)

// Config holds the env-loadable worker knobs. ClaimBatch == 0 derives the
// per-poll claim budget from free worker slots.
type Config struct {
	Concurrency    int           `env:"QUEUE_CONCURRENCY"`
	PollInterval   time.Duration `env:"QUEUE_POLL_INTERVAL"`
	Lease          time.Duration `env:"QUEUE_LEASE"`
	MaxAttempts    int           `env:"QUEUE_MAX_ATTEMPTS"`
	ClaimBatch     int           `env:"QUEUE_CLAIM_BATCH"`
	HandlerTimeout time.Duration `env:"QUEUE_HANDLER_TIMEOUT"`
	DeadRetention  time.Duration `env:"QUEUE_DEAD_RETENTION"`
}

// DefaultConfig returns production defaults: 10 workers, 1s poll, 30s lease,
// 25 attempts, 10m handler timeout, claim batch derived from free slots, 30d
// dead-letter retention.
func DefaultConfig() Config {
	return Config{Concurrency: 10, PollInterval: time.Second, Lease: 30 * time.Second, MaxAttempts: 25, HandlerTimeout: 10 * time.Minute, DeadRetention: 720 * time.Hour}
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
	if c.HandlerTimeout < 0 {
		return fmt.Errorf("%w: HandlerTimeout must be >= 0 (0 disables the default), got %v", ErrInvalidConfig, c.HandlerTimeout)
	}
	if c.DeadRetention < 0 {
		return fmt.Errorf("%w: DeadRetention must be >= 0 (0 keeps dead jobs forever), got %v", ErrInvalidConfig, c.DeadRetention)
	}
	return nil
}
