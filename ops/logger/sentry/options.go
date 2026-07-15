package sentry

// Option configures NewHandler. Invalid values accumulate and are returned by NewHandler.
type Option func(*config)

// config holds resolved settings for a single NewHandler call. The embedded Config
// carries serializable data; errs collects invalid option values.
type config struct {
	errs []error
	Config
}

func defaultConfig() config {
	return config{Config: DefaultConfig()}
}

// WithConfig sets the whole serializable data block at once. Build the argument from
// DefaultConfig().
func WithConfig(cfg Config) Option {
	return func(c *config) { c.Config = cfg }
}
