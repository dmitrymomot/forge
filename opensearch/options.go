package opensearch

import (
	"fmt"
	"log/slog"

	osgo "github.com/opensearch-project/opensearch-go/v4"
)

// config holds resolved settings for a single Open call. The embedded Config
// carries serializable data; the remaining fields are non-serializable code values.
type config struct {
	logger       *slog.Logger
	clientConfig func(*osgo.Config)
	errs         []error
	Config
}

// Option configures Open. Invalid values accumulate and are returned by Open.
type Option func(*config)

// WithConfig sets the whole serializable data block at once. Build the argument
// from DefaultConfig() (or an env-parsed copy); a bare Config{} fails Validate.
// Options apply in order — place WithConfig before any code options it should not
// clobber.
func WithConfig(cfg Config) Option {
	return func(c *config) { c.Config = cfg }
}

// WithLogger sets the slog.Logger for lifecycle logging. Default slog.Default();
// nil installs a discard handler at Open time (it is not a validation error).
func WithLogger(l *slog.Logger) Option {
	return func(c *config) { c.logger = l }
}

// WithClientConfig is the native-config escape hatch: it mutates the driver's
// osgo.Config after the Config overlay and runs LAST in Open, so anything Config
// does not cover (a custom Transport/TLS, a Signer, a Logger) stays reachable. A
// nil func is rejected (ErrInvalidConfig).
func WithClientConfig(fn func(*osgo.Config)) Option {
	return func(c *config) {
		if fn == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithClientConfig received a nil func", ErrInvalidConfig))
			return
		}
		c.clientConfig = fn
	}
}
