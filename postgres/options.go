package postgres

import (
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// config holds resolved settings for a single Open. The embedded Config carries
// serializable data; the remaining fields are non-serializable code values.
// (PG-6 adds a `migrator Migrator` field here alongside the Migrator type.)
type config struct {
	logger     *slog.Logger
	poolConfig func(*pgxpool.Config)
	errs       []error
	Config
}

// Option configures Open. Invalid values accumulate and are returned by Open.
type Option func(*config)

// WithConfig sets the whole serializable data block at once. Build the argument
// from DefaultConfig() (or an env-parsed copy); a bare Config{} zeroes every limit
// and leaves URL empty (which fails Validate). Options apply in order — place
// WithConfig before any code option you want layered on top of it.
func WithConfig(cfg Config) Option {
	return func(c *config) { c.Config = cfg }
}

// WithLogger sets the slog.Logger used by Close and lifecycle logging. Default
// slog.Default(); a nil logger is rejected (ErrInvalidConfig).
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithLogger received a nil *slog.Logger", ErrInvalidConfig))
			return
		}
		c.logger = l
	}
}

// WithPoolConfig registers an escape hatch invoked inside Open, last, after the
// Config fields have been overlaid onto the parsed *pgxpool.Config. Use it for
// anything Config does not cover: query tracers, AfterConnect hooks, a custom TLS
// config. A nil func is rejected (ErrInvalidConfig).
func WithPoolConfig(fn func(*pgxpool.Config)) Option {
	return func(c *config) {
		if fn == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithPoolConfig received a nil func", ErrInvalidConfig))
			return
		}
		c.poolConfig = fn
	}
}
