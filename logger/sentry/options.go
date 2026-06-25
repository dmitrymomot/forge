package sentry

import (
	"fmt"
	"io"

	"github.com/dmitrymomot/forge/logger"
)

// Option configures New. Invalid values accumulate and are returned by New.
type Option func(*config)

// config holds resolved settings for a single New call. The embedded Config carries
// serializable data; the remaining fields are non-serializable code values.
type config struct {
	output     io.Writer
	extractors []logger.ContextExtractor
	errs       []error
	Config
}

func defaultConfig() config {
	return config{Config: DefaultConfig()}
}

// WithConfig sets the whole serializable data block (primary-logger + Sentry settings).
// Build the argument from DefaultConfig().
func WithConfig(cfg Config) Option {
	return func(c *config) { c.Config = cfg }
}

// WithContextExtractors registers ContextExtractor funcs for the primary logger AND the
// Sentry destination (they sit beneath one decorator). Nil entries are filtered.
func WithContextExtractors(ex ...logger.ContextExtractor) Option {
	return func(c *config) {
		for _, e := range ex {
			if e != nil {
				c.extractors = append(c.extractors, e)
			}
		}
	}
}

// WithOutput overrides the primary destination's writer (tests). A nil writer is rejected.
func WithOutput(w io.Writer) Option {
	return func(c *config) {
		if w == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithOutput received a nil io.Writer", ErrInvalidConfig))
			return
		}
		c.output = w
	}
}
