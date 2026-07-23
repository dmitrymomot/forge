package session

import (
	"context"
	"log/slog"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

// config holds resolved settings for a single New call. The embedded Config carries
// serializable data; the remaining fields are non-serializable code values.
type config struct {
	store  Store
	clock  clock.Clock
	logger *slog.Logger
	scope  func(context.Context) (string, error)
	Config
}

// Option configures the Manager. Options are applied over the Config passed to
// New, so an explicit option always wins over an env-loaded value.
type Option func(*config)

// WithStore sets the backing store. Required.
func WithStore(s Store) Option { return func(c *config) { c.store = s } }

// WithIdle sets the sliding idle timeout.
func WithIdle(d time.Duration) Option { return func(c *config) { c.Idle = d } }

// WithMaxTTL sets the absolute lifetime no activity extends. Zero means no cap.
func WithMaxTTL(d time.Duration) Option { return func(c *config) { c.MaxTTL = d } }

// WithRememberIdle sets the sliding idle timeout for remembered sessions.
func WithRememberIdle(d time.Duration) Option { return func(c *config) { c.RememberIdle = d } }

// WithRememberMaxTTL sets the absolute lifetime for remembered sessions. Zero means no cap.
func WithRememberMaxTTL(d time.Duration) Option { return func(c *config) { c.RememberMax = d } }

// WithTouch sets the metadata-only refresh interval: a request whose deadline
// moved by less than this does not write to the store. Zero disables touching,
// which makes every request a full save.
func WithTouch(d time.Duration) Option { return func(c *config) { c.Touch = d } }

// WithScope enables tenant scoping. Without it, session is single-tenant and
// every record's Tenant is empty. With it, the hook resolves the tenant for the
// current request from context; every save stamps it, every load and every
// device-management call is confined to it, and a hook error or empty scope
// fails the operation closed rather than crossing tenants. The signature matches
// the WithScope option of every other forge package, so web/tenant's Scope hook
// plugs in directly: session.New(cfg, session.WithStore(st), session.WithScope(tenant.Scope)).
func WithScope(fn func(context.Context) (string, error)) Option {
	return func(c *config) { c.scope = fn }
}

// WithClock injects a clock for deterministic tests.
func WithClock(cl clock.Clock) Option { return func(c *config) { c.clock = cl } }

// WithLogger sets the logger. Defaults to logger.NewNope().
func WithLogger(l *slog.Logger) Option { return func(c *config) { c.logger = l } }
