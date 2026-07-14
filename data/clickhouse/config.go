package clickhouse

import (
	"errors"
	"fmt"
	"time"
)

// Config holds the serializable settings for a connection. The env struct tags are
// inert strings — this package imports no config loader. Populate Config with any
// loader that reads env struct tags, typically by seeding from DefaultConfig and
// parsing the environment over it. Field order is subject to the repo's betteralign
// tooling. Everything ClickHouse-specific not listed here (TLS, the Settings map,
// block buffer size, custom auth, HTTP vs native protocol) rides the DSN query params
// or the WithOptions escape hatch.
type Config struct {
	DSN             string        `env:"CLICKHOUSE_DSN"`               // clickhouse://user:pass@host:9000/db?param=value (required)
	ConnMaxLifetime time.Duration `env:"CLICKHOUSE_CONN_MAX_LIFETIME"` // close a conn this long after creation
	DialTimeout     time.Duration `env:"CLICKHOUSE_DIAL_TIMEOUT"`      // per-attempt dial+handshake bound
	RetryInterval   time.Duration `env:"CLICKHOUSE_RETRY_INTERVAL"`    // base backoff between connect attempts
	MaxOpenConns    int           `env:"CLICKHOUSE_MAX_OPEN_CONNS"`    // pool ceiling
	MaxIdleConns    int           `env:"CLICKHOUSE_MAX_IDLE_CONNS"`    // idle pool size
	RetryAttempts   int           `env:"CLICKHOUSE_RETRY_ATTEMPTS"`    // total connect attempts; <=1 means one, no wait
}

// DefaultConfig returns production-sane pool/timeout defaults and is the single source
// of truth for them (there are no envDefault tags to drift from it). DSN is left empty
// and must be supplied; DefaultConfig alone therefore fails Validate.
func DefaultConfig() Config {
	return Config{
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: 30 * time.Minute,
		DialTimeout:     5 * time.Second,
		RetryAttempts:   3,
		RetryInterval:   time.Second,
	}
}

// Validate reports whether the field values are usable, returning an
// ErrInvalidConfig-wrapped, single-line joined error otherwise. Callers may call it
// after loading from env (zero-trust); Open/OpenDB also call it defensively.
func (c Config) Validate() error {
	var errs []error
	if c.DSN == "" {
		errs = append(errs, fmt.Errorf("%w: DSN must not be empty", ErrInvalidConfig))
	}
	if c.MaxOpenConns < 0 {
		errs = append(errs, fmt.Errorf("%w: MaxOpenConns must be >= 0", ErrInvalidConfig))
	}
	if c.MaxIdleConns < 0 {
		errs = append(errs, fmt.Errorf("%w: MaxIdleConns must be >= 0", ErrInvalidConfig))
	}
	if c.MaxOpenConns > 0 && c.MaxIdleConns > c.MaxOpenConns {
		errs = append(errs, fmt.Errorf("%w: MaxIdleConns must be <= MaxOpenConns", ErrInvalidConfig))
	}
	if c.RetryAttempts < 0 {
		errs = append(errs, fmt.Errorf("%w: RetryAttempts must be >= 0", ErrInvalidConfig))
	}
	for _, f := range []struct {
		name string
		d    time.Duration
	}{
		{"ConnMaxLifetime", c.ConnMaxLifetime},
		{"DialTimeout", c.DialTimeout},
		{"RetryInterval", c.RetryInterval},
	} {
		if f.d < 0 {
			errs = append(errs, fmt.Errorf("%w: %s must be >= 0", ErrInvalidConfig, f.name))
		}
	}
	return errors.Join(errs...)
}
