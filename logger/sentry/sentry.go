package sentry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	sentry "github.com/getsentry/sentry-go"
	sentryslog "github.com/getsentry/sentry-go/slog"

	"github.com/dmitrymomot/forge/logger"
)

// Config carries both the primary-logger settings (embedded logger.Config) and the
// Sentry-specific settings, so the whole thing env-loads in one shot.
type Config struct {
	DSN         string `env:"DSN"`
	Environment string `env:"ENVIRONMENT"`
	// MinLevel is Sentry's OWN minimum level — the lowest level forwarded to Sentry,
	// independent of the primary destination's level. "debug"|"info"|"warn"/"warning"|"error".
	MinLevel string `env:"MIN_LEVEL"`
	logger.Config

	EnableLogs bool `env:"ENABLE_LOGS"`
}

// DefaultConfig returns the optimal defaults, including the embedded logger defaults.
func DefaultConfig() Config {
	return Config{
		Config:      logger.DefaultConfig(),
		Environment: "production",
		MinLevel:    "warn",
	}
}

// Validate validates the embedded logger.Config and the MinLevel. It wraps with
// double-%w so a bad primary Level/Format matches both sentry.ErrInvalidConfig and
// logger.ErrInvalidConfig. An empty DSN is valid (Sentry is then disabled in New).
func (c Config) Validate() error {
	if err := c.Config.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	if _, ok := levelByName(c.MinLevel); !ok {
		return fmt.Errorf("%w: unknown MinLevel %q", ErrInvalidConfig, c.MinLevel)
	}
	return nil
}

// Flush flushes buffered Sentry events; call it before the program exits. The timeout is
// derived from ctx.Deadline() (fallback defaultFlushTimeout). Returns ErrSentryFlushTimeout
// if not all events were delivered in time. A no-op when Sentry is not active.
type Flush func(ctx context.Context) error

const defaultFlushTimeout = 2 * time.Second

// noopFlush is returned whenever Sentry is inactive (empty DSN or init failure).
func noopFlush(context.Context) error { return nil }

// flush flushes the global Sentry client within the ctx deadline (or the 2s default).
func flush(ctx context.Context) error {
	timeout := defaultFlushTimeout
	if dl, ok := ctx.Deadline(); ok {
		timeout = time.Until(dl)
	}
	if timeout <= 0 { // deadline already passed (ctx may not be Done yet)
		if err := ctx.Err(); err != nil {
			return err
		}
		return ErrSentryFlushTimeout
	}
	if !sentry.Flush(timeout) {
		return ErrSentryFlushTimeout
	}
	return nil
}

// parseLevel maps a validated MinLevel name to its slog.Level (assumes Validate passed).
func parseLevel(s string) slog.Level {
	lvl, _ := levelByName(s)
	return lvl
}

// levelsFrom returns the standard slog levels at or above min, low to high.
func levelsFrom(min slog.Level) []slog.Level {
	all := []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError}
	out := make([]slog.Level, 0, len(all))
	for _, l := range all {
		if l >= min {
			out = append(out, l)
		}
	}
	return out
}

// realSentryHandler initializes the Sentry SDK and builds the slog handler. Confirmed
// against sentry-go v0.47.0.
func realSentryHandler(cfg Config) (slog.Handler, error) {
	// v0.47.0: ClientOptions has no EnableLogs; logs are on by default, gated by
	// DisableLogs — so our opt-in EnableLogs inverts.
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:         cfg.DSN,
		Environment: cfg.Environment,
		DisableLogs: !cfg.EnableLogs,
	}); err != nil {
		return nil, err
	}
	min := parseLevel(cfg.MinLevel)
	return sentryslog.Option{
		EventLevel: []slog.Level{slog.LevelError}, // errors → Issues (DEPRECATED, removed in v0.48.0)
		LogLevel:   levelsFrom(min),               // min..error → Logs
	}.NewSentryHandler(context.Background()), nil
}

// levelByName maps a level name to a slog.Level, reporting whether it is known.
func levelByName(s string) (slog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	default:
		return 0, false
	}
}

// Option configures New. Invalid values accumulate and are returned by New.
type Option func(*config)

// config holds resolved settings for a single New call.
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

// New builds a logger that writes to the primary destination and, when DSN is non-empty,
// also to Sentry in parallel at MinLevel. Empty DSN returns a plain logger and a no-op
// Flush; an init failure returns a usable plain logger plus an ErrSentryInit-wrapped
// error. Call New once per process (it initializes the global Sentry hub).
func New(opts ...Option) (*slog.Logger, Flush, error) {
	return newWith(realSentryHandler, opts...)
}

// newWith is the test seam: New passes realSentryHandler; tests pass a fake builder.
func newWith(buildHandler func(Config) (slog.Handler, error), opts ...Option) (*slog.Logger, Flush, error) {
	c := defaultConfig()
	for _, opt := range opts {
		opt(&c)
	}
	if len(c.errs) > 0 {
		return nil, nil, errors.Join(c.errs...)
	}
	if err := c.Validate(); err != nil {
		return nil, nil, err
	}

	loggerOpts := []logger.Option{logger.WithConfig(c.Config.Config)} // embedded logger.Config
	if len(c.extractors) > 0 {
		loggerOpts = append(loggerOpts, logger.WithContextExtractors(c.extractors...))
	}
	if c.output != nil {
		loggerOpts = append(loggerOpts, logger.WithOutput(c.output))
	}

	if c.DSN == "" { // Sentry disabled — plain logger, no-op flush
		l, err := logger.New(loggerOpts...)
		return l, noopFlush, err
	}

	sh, err := buildHandler(c.Config)
	if err != nil { // graceful: keep logging, surface the error
		l, lerr := logger.New(loggerOpts...)
		if lerr != nil {
			return nil, nil, lerr
		}
		return l, noopFlush, fmt.Errorf("%w: %v", ErrSentryInit, err)
	}

	l, err := logger.New(append(loggerOpts, logger.WithHandler(sh))...)
	if err != nil {
		return nil, nil, err
	}
	return l, flush, nil
}
