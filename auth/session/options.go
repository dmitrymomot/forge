package session

import (
	"log/slog"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

type options struct {
	store  Store
	clock  clock.Clock
	logger *slog.Logger
	cfg    Config
}

// Option configures the Manager. Options are applied over the Config passed to
// New, so an explicit option always wins over an env-loaded value.
type Option func(*options)

// WithStore sets the backing store. Required.
func WithStore(s Store) Option { return func(o *options) { o.store = s } }

// WithIdle sets the sliding idle timeout.
func WithIdle(d time.Duration) Option { return func(o *options) { o.cfg.Idle = d } }

// WithMaxTTL sets the absolute lifetime no activity extends. Zero means no cap.
func WithMaxTTL(d time.Duration) Option { return func(o *options) { o.cfg.MaxTTL = d } }

// WithRememberIdle sets the sliding idle timeout for remembered sessions.
func WithRememberIdle(d time.Duration) Option { return func(o *options) { o.cfg.RememberIdle = d } }

// WithRememberMaxTTL sets the absolute lifetime for remembered sessions. Zero means no cap.
func WithRememberMaxTTL(d time.Duration) Option { return func(o *options) { o.cfg.RememberMax = d } }

// WithTouch sets the metadata-only refresh interval: a request whose deadline
// moved by less than this does not write to the store. Zero disables touching,
// which makes every request a full save.
func WithTouch(d time.Duration) Option { return func(o *options) { o.cfg.Touch = d } }

// WithClock injects a clock for deterministic tests.
func WithClock(c clock.Clock) Option { return func(o *options) { o.clock = c } }

// WithLogger sets the logger. Defaults to logger.NewNope().
func WithLogger(l *slog.Logger) Option { return func(o *options) { o.logger = l } }
