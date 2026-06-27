package redis

import (
	"errors"
	"fmt"
	"time"
)

// Config holds the serializable settings for a Redis (or Valkey) client. The env
// struct tags are inert strings — this package imports no config loader. Seed from
// DefaultConfig and parse the environment over it with whatever loader reads env
// struct tags. Field order is subject to the repo's betteralign tooling.
//
// Topology is selected from these fields by go-redis's NewUniversalClient: a single
// Addresses entry with an empty MasterName is standalone, multiple entries are a
// cluster, and a non-empty MasterName is sentinel/failover (Addresses then lists the
// sentinels). DB applies to standalone/sentinel only; cluster ignores it.
type Config struct {
	MasterName      string        `env:"MASTER_NAME"` // set -> sentinel/failover mode
	Username        string        `env:"USERNAME"`    // ACL username (Redis 6+); empty for legacy auth
	Password        string        `env:"PASSWORD"`    // empty when the server requires no auth
	Addresses       []string      `env:"ADDRESSES"`   // 1 = standalone, many = cluster; sentinels when MasterName set
	DB              int           `env:"DB"`          // standalone/sentinel only (cluster ignores it)
	PoolSize        int           `env:"POOL_SIZE"`   // max connections per node
	MinIdleConns    int           `env:"MIN_IDLE_CONNS"`
	DialTimeout     time.Duration `env:"DIAL_TIMEOUT"`
	ReadTimeout     time.Duration `env:"READ_TIMEOUT"`
	WriteTimeout    time.Duration `env:"WRITE_TIMEOUT"`
	ConnMaxIdleTime time.Duration `env:"CONN_MAX_IDLE_TIME"`
	RetryAttempts   int           `env:"RETRY_ATTEMPTS"` // bounded connect-retry attempts in Open
	RetryInterval   time.Duration `env:"RETRY_INTERVAL"` // base backoff between connect attempts
}

// DefaultConfig returns production-sane defaults and is the single source of truth
// for them (there are no envDefault tags to drift from it). Addresses is left empty
// on purpose — it has no universal default and must be supplied; DefaultConfig alone
// therefore fails Validate.
func DefaultConfig() Config {
	return Config{
		PoolSize:      10,
		DialTimeout:   5 * time.Second,
		ReadTimeout:   3 * time.Second,
		WriteTimeout:  3 * time.Second,
		RetryAttempts: 3,
		RetryInterval: 1 * time.Second,
	}
}

// Validate reports whether the field values are usable, returning an
// ErrInvalidConfig-wrapped, single-line joined error otherwise. Open calls it
// defensively; callers may call it after env-loading (zero-trust).
func (c Config) Validate() error {
	var errs []error
	if len(c.Addresses) == 0 {
		errs = append(errs, fmt.Errorf("%w: Addresses must not be empty", ErrInvalidConfig))
	}
	for _, f := range []struct {
		name string
		d    time.Duration
	}{
		{"DialTimeout", c.DialTimeout},
		{"ReadTimeout", c.ReadTimeout},
		{"WriteTimeout", c.WriteTimeout},
		{"ConnMaxIdleTime", c.ConnMaxIdleTime},
		{"RetryInterval", c.RetryInterval},
	} {
		if f.d < 0 {
			errs = append(errs, fmt.Errorf("%w: %s must be >= 0", ErrInvalidConfig, f.name))
		}
	}
	if c.RetryAttempts < 0 {
		errs = append(errs, fmt.Errorf("%w: RetryAttempts must be >= 0", ErrInvalidConfig))
	}
	return errors.Join(errs...)
}
