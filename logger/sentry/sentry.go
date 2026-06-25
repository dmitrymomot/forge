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

// Flush flushes buffered Sentry events; call it before the program exits. The wait honors
// ctx's cancellation and deadline (fallback defaultFlushTimeout). Returns the context's
// error if ctx is done, or ErrSentryFlushTimeout if events remain unsent. A no-op when
// Sentry is not active. New always returns a non-nil Flush, so `defer flush(ctx)` is safe
// even when New returns an error.
type Flush func(ctx context.Context) error

const defaultFlushTimeout = 2 * time.Second

// noopFlush is returned whenever Sentry is inactive (empty DSN, init failure, or a New that
// errored before activating Sentry). Keeping it non-nil makes deferring Flush always safe.
func noopFlush(context.Context) error { return nil }

// flush flushes the global Sentry client, honoring ctx's cancellation and deadline. When
// ctx carries no deadline the wait is bounded to defaultFlushTimeout so a stuck transport
// cannot block forever. Returns the context's error if ctx is (or becomes) done, or
// ErrSentryFlushTimeout if events remain unsent within the window.
func flush(ctx context.Context) error {
	if err := ctx.Err(); err != nil { // already canceled or past deadline
		return err
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultFlushTimeout)
		defer cancel()
	}
	if !sentry.FlushWithContext(ctx) {
		if err := ctx.Err(); err != nil {
			return err
		}
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

// issueLevel is the minimum slog level reported to Sentry as an exception (Issue).
const issueLevel = slog.LevelError

// sentryOption builds the sentryslog handler options from cfg — the Sentry Logs side only.
// EventLevel is set empty (not nil) to DISABLE sentryslog's deprecated log->event (Issue)
// conversion; errors become Issues via captureHandler/sentry.CaptureException instead.
// Drop the EventLevel line once sentry-go removes the field (slated for v0.48.0); leaving
// it nil would make the SDK re-enable the deprecated path with its [Error,Fatal] default.
// Factored out so the level/AddSource mapping is unit-testable without the global hub.
func sentryOption(cfg Config) sentryslog.Option {
	return sentryslog.Option{
		EventLevel: []slog.Level{},                       // disable deprecated log->Issue conversion
		LogLevel:   levelsFrom(parseLevel(cfg.MinLevel)), // MinLevel..error → Sentry Logs
		AddSource:  cfg.AddSource,                        // mirror the primary's AddSource
	}
}

// sentryCapturer reports a record to Sentry as an exception. It is a seam so the capture
// path is unit-testable without initializing the global Sentry hub.
type sentryCapturer func(ctx context.Context, rec slog.Record, attrs []slog.Attr)

// captureHandler reports records at or above issueLevel to Sentry as exceptions (Issues)
// via sentry.CaptureException — the non-deprecated replacement for sentryslog's removed
// EventLevel log->event conversion — then delegates every record to next (the Logs handler).
type captureHandler struct {
	next    slog.Handler
	capture sentryCapturer
	attrs   []slog.Attr
	level   slog.Level
}

func (h *captureHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level || h.next.Enabled(ctx, level)
}

func (h *captureHandler) Handle(ctx context.Context, rec slog.Record) error {
	if rec.Level >= h.level {
		h.capture(ctx, rec, h.attrs)
	}
	return h.next.Handle(ctx, rec)
}

func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	merged := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	merged = append(merged, h.attrs...)
	merged = append(merged, attrs...)
	return &captureHandler{next: h.next.WithAttrs(attrs), capture: h.capture, attrs: merged, level: h.level}
}

func (h *captureHandler) WithGroup(name string) slog.Handler {
	return &captureHandler{next: h.next.WithGroup(name), capture: h.capture, attrs: h.attrs, level: h.level}
}

// errorFromRecord builds the error passed to CaptureException: the first error-valued
// attribute wrapped with the record message (preserving the original for grouping and any
// stack trace), or the message alone when no error attribute is present.
func errorFromRecord(rec slog.Record) error {
	var inner error
	rec.Attrs(func(a slog.Attr) bool {
		if e, ok := a.Value.Any().(error); ok {
			inner = e
			return false
		}
		return true
	})
	if inner != nil {
		return fmt.Errorf("%s: %w", rec.Message, inner)
	}
	return errors.New(rec.Message)
}

// captureException is the production sentryCapturer: it reports the record to the context's
// hub (or the current hub) as an exception, attaching the handler's accumulated attributes
// and the record's own attributes under a "log" context for triage.
func captureException(ctx context.Context, rec slog.Record, attrs []slog.Attr) {
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub()
	}
	hub.WithScope(func(scope *sentry.Scope) {
		data := sentry.Context{}
		for _, a := range attrs {
			data[a.Key] = a.Value.Any()
		}
		rec.Attrs(func(a slog.Attr) bool {
			data[a.Key] = a.Value.Any()
			return true
		})
		if len(data) > 0 {
			scope.SetContext("log", data)
		}
		hub.CaptureException(errorFromRecord(rec))
	})
}

// realSentryHandler initializes the Sentry SDK and builds the handler: a Sentry Logs
// handler (sentryslog) wrapped by captureHandler, which sends Error+ records to Sentry as
// Issues via CaptureException. Confirmed against sentry-go v0.47.0. The thin glue here
// mutates the global hub via sentry.Init; the testable logic lives in sentryOption,
// captureHandler, and errorFromRecord.
func realSentryHandler(cfg Config) (slog.Handler, error) {
	// v0.47.0: ClientOptions has no EnableLogs; logs are on by default, gated by
	// DisableLogs — so our opt-in EnableLogs inverts. (DisableLogs gates Sentry Logs only;
	// Issues via CaptureException are unaffected, so errors are reported regardless.)
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:         cfg.DSN,
		Environment: cfg.Environment,
		DisableLogs: !cfg.EnableLogs,
	}); err != nil {
		return nil, err
	}
	logs := sentryOption(cfg).NewSentryHandler(context.Background())
	return &captureHandler{next: logs, capture: captureException, level: issueLevel}, nil
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
// also reports to Sentry: records at Error and above become Issues (via
// sentry.CaptureException), and records from MinLevel up to error become Sentry Logs when
// EnableLogs is set. Empty DSN returns a plain logger and a no-op
// Flush; an init failure returns a usable plain logger plus an ErrSentryInit-wrapped
// error. The returned Flush is always non-nil (a no-op when Sentry is inactive), so it is
// safe to defer regardless of the error; on a fatal config error the logger is nil and the
// error is set — check it before logging. Call New once per process (it initializes the
// global Sentry hub).
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
		return nil, noopFlush, errors.Join(c.errs...)
	}
	if err := c.Validate(); err != nil {
		return nil, noopFlush, err
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
			return nil, noopFlush, lerr
		}
		return l, noopFlush, fmt.Errorf("%w: %v", ErrSentryInit, err)
	}

	l, err := logger.New(append(loggerOpts, logger.WithHandler(sh))...)
	if err != nil {
		return nil, noopFlush, err
	}
	return l, flush, nil
}
