package scheduler

import (
	"fmt"
	"time"
)

// Config holds the env-loadable scheduler knobs. OpTimeout bounds one tick's
// claim/enqueue/release sequence and one sweep pass, so a wedged store or
// broker cannot block graceful shutdown; an aborted sweep resumes at the next
// SweepInterval (the purge is batched and keeps its progress).
type Config struct {
	Retention     time.Duration `env:"SCHEDULER_RETENTION"`
	SweepInterval time.Duration `env:"SCHEDULER_SWEEP_INTERVAL"`
	RetryInterval time.Duration `env:"SCHEDULER_RETRY_INTERVAL"`
	OpTimeout     time.Duration `env:"SCHEDULER_OP_TIMEOUT"`
}

// DefaultConfig returns production defaults: 24h claim retention, hourly
// sweep, 5s retry on a failed enqueue, 30s op timeout.
func DefaultConfig() Config {
	return Config{Retention: 24 * time.Hour, SweepInterval: time.Hour, RetryInterval: 5 * time.Second, OpTimeout: 30 * time.Second}
}

// Validate checks the configuration invariants.
func (c Config) Validate() error {
	if c.Retention <= 0 {
		return fmt.Errorf("%w: Retention must be > 0, got %v", ErrInvalidConfig, c.Retention)
	}
	if c.SweepInterval < 0 {
		return fmt.Errorf("%w: SweepInterval must be >= 0 (0 disables the sweep), got %v", ErrInvalidConfig, c.SweepInterval)
	}
	if c.RetryInterval <= 0 {
		return fmt.Errorf("%w: RetryInterval must be > 0, got %v", ErrInvalidConfig, c.RetryInterval)
	}
	if c.OpTimeout <= 0 {
		return fmt.Errorf("%w: OpTimeout must be > 0, got %v", ErrInvalidConfig, c.OpTimeout)
	}
	return nil
}
