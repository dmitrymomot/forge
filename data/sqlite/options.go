package sqlite

import (
	"fmt"
	"log/slog"
)

// config holds resolved settings for a single Open. The embedded Config carries
// serializable data; the remaining fields are non-serializable code values.
type config struct {
	logger   *slog.Logger
	migrator Migrator
	pragmas  []pragma
	errs     []error
	Config
}

// Option configures Open. Invalid values accumulate and are returned by Open.
type Option func(*config)

// WithConfig sets the whole serializable data block at once. Build the argument from
// DefaultConfig() (or an env-parsed copy); a bare Config{} leaves Path empty (which
// fails Validate). Options apply in order — place WithConfig before code options you
// want layered on top of it.
func WithConfig(cfg Config) Option {
	return func(c *config) { c.Config = cfg }
}

// WithLogger sets the slog.Logger used by Close. Default slog.Default(); a nil logger
// is rejected (ErrInvalidConfig).
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithLogger received a nil *slog.Logger", ErrInvalidConfig))
			return
		}
		c.logger = l
	}
}

// WithPragma appends an extra PRAGMA applied per connection to both pools. For a
// pragma name Config already sets (e.g. cache_size), the value given here replaces
// it (buildDSN dedupes by name, last value wins — DSN order plays no part, since
// modernc.org/sqlite re-sorts _pragma params before applying them). For a new pragma
// name, it is simply added. Use it for anything Config does not cover. An empty name
// is rejected (ErrInvalidConfig). Values must be simple pragma tokens (they are not
// escaped).
func WithPragma(name, value string) Option {
	return func(c *config) {
		if name == "" {
			c.errs = append(c.errs, fmt.Errorf("%w: WithPragma received an empty name", ErrInvalidConfig))
			return
		}
		c.pragmas = append(c.pragmas, pragma{name: name, value: value})
	}
}

// WithMigrator registers a Migrator that Open runs against the writer pool after both
// pools are live and pinged, before Open returns. A failed migration fails Open. A nil
// Migrator is rejected (ErrInvalidConfig). Pass migration.New(fsys,
// migration.WithDialect(migration.SQLite)) — *migration.Migrator satisfies Migrator.
func WithMigrator(m Migrator) Option {
	return func(c *config) {
		if m == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithMigrator received a nil Migrator", ErrInvalidConfig))
			return
		}
		c.migrator = m
	}
}
