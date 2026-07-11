package geoip

import "log/slog"

type config struct {
	logger *slog.Logger
}

// Option configures Middleware.
type Option func(*config)

func newConfig(opts ...Option) config {
	c := config{logger: slog.Default()}
	for _, o := range opts {
		o(&c)
	}
	return c
}

// WithLogger sets the logger used for the Debug message when a Source lookup
// returns an error. Defaults to slog.Default(). A nil logger is ignored.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l != nil {
			c.logger = l
		}
	}
}
