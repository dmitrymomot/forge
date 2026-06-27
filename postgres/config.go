package postgres

import (
	"errors"
	"fmt"
	"time"
)

// Config holds the serializable settings for a connection pool. The env struct
// tags are inert strings — this package imports no config loader. Populate Config
// with any loader that reads env struct tags, typically by seeding from
// DefaultConfig and parsing the environment over it. Field order is subject to the
// repo's betteralign tooling.
type Config struct {
	URL               string        `env:"URL"`                 // postgres://… connection string (required)
	MaxConnLifetime   time.Duration `env:"MAX_CONN_LIFETIME"`   // close a conn this long after creation
	MaxConnIdleTime   time.Duration `env:"MAX_CONN_IDLE_TIME"`  // close an idle conn after this long
	HealthCheckPeriod time.Duration `env:"HEALTH_CHECK_PERIOD"` // pgxpool's own idle-conn check interval
	ConnectTimeout    time.Duration `env:"CONNECT_TIMEOUT"`     // per-attempt dial+handshake bound
	RetryInterval     time.Duration `env:"RETRY_INTERVAL"`      // base backoff between connect attempts
	MinConns          int32         `env:"MIN_CONNS"`           // pool floor
	MaxConns          int32         `env:"MAX_CONNS"`           // pool ceiling
	RetryAttempts     int           `env:"RETRY_ATTEMPTS"`      // total connect attempts; <=1 means one, no wait
}

// DefaultConfig returns production-sane pool defaults and is the single source of
// truth for them (there are no envDefault tags to drift from it). URL is left empty
// and must be supplied; DefaultConfig alone therefore fails Validate.
func DefaultConfig() Config {
	return Config{
		MaxConns:          10,
		MinConns:          2,
		MaxConnLifetime:   30 * time.Minute,
		MaxConnIdleTime:   10 * time.Minute,
		HealthCheckPeriod: time.Minute,
		ConnectTimeout:    5 * time.Second,
		RetryAttempts:     3,
		RetryInterval:     time.Second,
	}
}

// Validate reports whether the field values are usable, returning an
// ErrInvalidConfig-wrapped, single-line joined error otherwise. Callers may call it
// after loading from env (zero-trust); Open also calls it defensively.
func (c Config) Validate() error {
	var errs []error
	if c.URL == "" {
		errs = append(errs, fmt.Errorf("%w: URL must not be empty", ErrInvalidConfig))
	}
	if c.MaxConns < 0 {
		errs = append(errs, fmt.Errorf("%w: MaxConns must be >= 0", ErrInvalidConfig))
	}
	if c.MinConns < 0 {
		errs = append(errs, fmt.Errorf("%w: MinConns must be >= 0", ErrInvalidConfig))
	}
	if c.MaxConns > 0 && c.MinConns > c.MaxConns {
		errs = append(errs, fmt.Errorf("%w: MinConns must be <= MaxConns", ErrInvalidConfig))
	}
	for _, f := range []struct {
		name string
		d    time.Duration
	}{
		{"MaxConnLifetime", c.MaxConnLifetime},
		{"MaxConnIdleTime", c.MaxConnIdleTime},
		{"HealthCheckPeriod", c.HealthCheckPeriod},
		{"ConnectTimeout", c.ConnectTimeout},
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
