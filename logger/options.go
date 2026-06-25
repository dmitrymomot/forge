package logger

import (
	"fmt"
	"io"
	"log/slog"
)

// Option configures New. Invalid values accumulate and are returned by New.
type Option func(*config)

// config holds resolved settings for a single New call. The embedded Config carries
// serializable data; the remaining fields are non-serializable code values.
type config struct {
	Config
	outputOverride io.Writer // WithOutput; nil means use Config.File or stdout
	levelOverride  *slog.Level
	formatOverride *Format
	extractors     []ContextExtractor
	extraHandlers  []slog.Handler
	errs           []error
}

func defaultConfig() config {
	return config{Config: DefaultConfig()}
}

// WithConfig sets the whole serializable data block at once. Options apply in order — a
// later WithConfig replaces the block, so place it before any WithFile you want to keep.
// WithLevel and WithFormat are separate code-level overrides, not part of this block:
// they always win over Config.Level/Config.Format regardless of WithConfig ordering.
func WithConfig(cfg Config) Option {
	return func(c *config) { c.Config = cfg }
}

// WithLevel sets an explicit code-level minimum that always wins over Config.Level.
func WithLevel(level slog.Level) Option {
	return func(c *config) { lv := level; c.levelOverride = &lv }
}

// WithFormat sets an explicit code format override that always wins over Config.Format.
func WithFormat(f Format) Option {
	return func(c *config) { ff := f; c.formatOverride = &ff }
}

// WithFile routes the primary destination to a file at path INSTEAD of stdout. An empty
// path is rejected.
func WithFile(path string) Option {
	return func(c *config) {
		if path == "" {
			c.errs = append(c.errs, fmt.Errorf("%w: WithFile received an empty path", ErrInvalidConfig))
			return
		}
		c.File = path
	}
}

// WithOutput sets the primary destination's writer directly. It takes precedence over
// Config.File. A nil writer is rejected.
func WithOutput(w io.Writer) Option {
	return func(c *config) {
		if w == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithOutput received a nil io.Writer", ErrInvalidConfig))
			return
		}
		c.outputOverride = w
	}
}

// WithHandler adds an extra parallel destination that runs alongside the primary one,
// beneath context extraction. A nil handler is rejected.
func WithHandler(h slog.Handler) Option {
	return func(c *config) {
		if h == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithHandler received a nil slog.Handler", ErrInvalidConfig))
			return
		}
		c.extraHandlers = append(c.extraHandlers, h)
	}
}

// WithContextExtractors registers ContextExtractor funcs applied on every log call.
// Nil entries are filtered; order is preserved.
func WithContextExtractors(ex ...ContextExtractor) Option {
	return func(c *config) {
		for _, e := range ex {
			if e != nil {
				c.extractors = append(c.extractors, e)
			}
		}
	}
}
