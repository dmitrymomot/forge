package migration

import "log/slog"

// DefaultTable is the goose version table name used when WithTable is not supplied.
const DefaultTable = "schema_migrations"

// config holds the resolved Migrator settings. table always carries DefaultTable
// unless WithTable overrides it; logger is optional.
type config struct {
	logger *slog.Logger
	table  string
}

// Option configures a Migrator built by New.
type Option func(*config)

// WithTable sets the goose version table name. An empty name is ignored, leaving
// DefaultTable ("schema_migrations") in place.
func WithTable(name string) Option {
	return func(c *config) {
		if name != "" {
			c.table = name
		}
	}
}

// WithLogger sets an slog.Logger for migration progress lines. A nil logger is
// ignored (goose's output is suppressed via a no-op adapter).
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l != nil {
			c.logger = l
		}
	}
}
