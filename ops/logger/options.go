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
	outputOverride  io.Writer // WithOutput; nil means use Config.File or stdout
	levelOverride   *slog.Level
	formatOverride  *Format
	extractors      []ContextExtractor
	extraHandlers   []slog.Handler
	dropHook        func(int64)
	errs            []error
	asyncBufferSize int // WithAsyncBufferSize; 0 means unset (NewAsync uses the default)
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

// WithLeveledHandler adds an extra parallel destination that only receives records at min
// and above, independent of the primary destination's level. A nil handler is rejected.
// Valid for both New and NewAsync.
func WithLeveledHandler(min slog.Level, h slog.Handler) Option {
	return func(c *config) {
		if h == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithLeveledHandler received a nil slog.Handler", ErrInvalidConfig))
			return
		}
		c.extraHandlers = append(c.extraHandlers, &leveledHandler{next: h, min: min})
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

// WithDropHook registers a receiver for the async drop tally. The worker calls it with
// the number of records the full buffer dropped, on the same pass that emits the Warn
// record, so a metrics counter sees drops without parsing log output:
//
//	dropped := rec.Counter("log_dropped_total", "Records the full logger queue dropped.")
//	logger.NewAsync(logger.WithDropHook(func(n int64) { dropped.Add(float64(n)) }))
//
// The hook runs on the worker goroutine, so it must return at once and must not log. A
// nil hook is rejected. Only valid with NewAsync; New returns ErrInvalidConfig if it is
// set. Last wins.
func WithDropHook(fn func(dropped int64)) Option {
	return func(c *config) {
		if fn == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithDropHook received a nil func", ErrInvalidConfig))
			return
		}
		c.dropHook = fn
	}
}

// WithAsyncBufferSize sets the async record buffer capacity (default 8192). n < 1 is
// rejected. Only valid with NewAsync; New returns ErrInvalidConfig if it is set.
func WithAsyncBufferSize(n int) Option {
	return func(c *config) {
		if n < 1 {
			c.errs = append(c.errs, fmt.Errorf("%w: WithAsyncBufferSize requires n >= 1, got %d", ErrInvalidConfig, n))
			return
		}
		c.asyncBufferSize = n
	}
}
