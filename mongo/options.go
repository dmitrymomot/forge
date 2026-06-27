package mongo

import (
	"fmt"
	"log/slog"

	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// config holds resolved settings for a single Open. The embedded Config carries
// serializable data; the remaining fields are non-serializable code values.
type config struct {
	logger        *slog.Logger
	clientOptions func(*options.ClientOptions)
	errs          []error
	Config
}

// Option configures Open. Invalid values accumulate and are returned by Open.
type Option func(*config)

// WithConfig sets the whole serializable data block at once. Build the argument
// from DefaultConfig() (or an env-parsed copy); a bare Config{} has an empty URI
// and fails Validate. Options apply in order — place WithConfig before any option
// you want to take precedence.
func WithConfig(cfg Config) Option {
	return func(c *config) { c.Config = cfg }
}

// WithLogger sets the slog.Logger used by Close for its lifecycle line. A nil
// logger is rejected (ErrInvalidConfig); pass slog.New(slog.DiscardHandler) to
// silence logging instead.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithLogger received a nil *slog.Logger", ErrInvalidConfig))
			return
		}
		c.logger = l
	}
}

// WithClientOptions is the native-driver escape hatch: it runs LAST in Open, after
// the Config-derived *options.ClientOptions have been built, so anything Config
// does not cover (TLS, monitors, custom dialer, auth) stays reachable. A nil func
// is rejected (ErrInvalidConfig).
func WithClientOptions(fn func(*options.ClientOptions)) Option {
	return func(c *config) {
		if fn == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithClientOptions received a nil func", ErrInvalidConfig))
			return
		}
		c.clientOptions = fn
	}
}
