package redis

import (
	"fmt"
	"log/slog"

	goredis "github.com/redis/go-redis/v9"
)

// config holds the resolved settings for one Open call. The embedded Config carries
// serializable data; the remaining fields are non-serializable code values.
type config struct {
	logger           *slog.Logger
	universalOptions func(*goredis.UniversalOptions)
	errs             []error
	Config
}

// Option configures Open. Invalid values accumulate in the config and are returned
// (joined, ErrInvalidConfig-wrapped) by Open before any network I/O.
type Option func(*config)

// WithConfig sets the whole serializable data block at once. Build the argument from
// DefaultConfig() (or an env-parsed copy); a bare Config{} zeroes the timeouts and
// fails Validate. Options apply in order — place WithConfig first if later
// convenience options should take precedence.
func WithConfig(cfg Config) Option {
	return func(c *config) { c.Config = cfg }
}

// WithLogger sets the slog.Logger used by Close for its lifecycle line. A nil logger
// is rejected (ErrInvalidConfig); pass a discard logger to silence logging instead.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithLogger received a nil *slog.Logger", ErrInvalidConfig))
			return
		}
		c.logger = l
	}
}

// WithUniversalOptions registers an escape hatch that runs LAST in Open, after the
// Config overlay, on the fully-built *goredis.UniversalOptions. Use it for anything
// the serializable fields don't cover — TLSConfig, OnConnect, a custom Dialer. A nil
// func is rejected (ErrInvalidConfig).
func WithUniversalOptions(fn func(*goredis.UniversalOptions)) Option {
	return func(c *config) {
		if fn == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithUniversalOptions received a nil func", ErrInvalidConfig))
			return
		}
		c.universalOptions = fn
	}
}

// buildOptions maps a validated Config onto a *goredis.UniversalOptions. It is a pure
// function (no I/O), so the Config -> options mapping is unit-testable without a
// server. goredis.NewUniversalClient then selects the topology from the result:
// a single Addr with no MasterName -> standalone, multiple Addrs -> cluster, a
// non-empty MasterName -> sentinel/failover.
func buildOptions(cfg Config) *goredis.UniversalOptions {
	return &goredis.UniversalOptions{
		Addrs:           cfg.Addresses,
		MasterName:      cfg.MasterName,
		Username:        cfg.Username,
		Password:        cfg.Password,
		DB:              cfg.DB,
		PoolSize:        cfg.PoolSize,
		MinIdleConns:    cfg.MinIdleConns,
		DialTimeout:     cfg.DialTimeout,
		ReadTimeout:     cfg.ReadTimeout,
		WriteTimeout:    cfg.WriteTimeout,
		ConnMaxIdleTime: cfg.ConnMaxIdleTime,
	}
}
