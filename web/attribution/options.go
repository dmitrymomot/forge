package attribution

import (
	"log/slog"
	"slices"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/ops/logger"
)

const defaultCookieName = "__Host-attribution"

// DefaultParams returns the params captured out of the box: the utm_* set
// plus the click IDs of the major ad platforms (Google, Microsoft, Meta,
// TikTok, X, LinkedIn, Pinterest, Reddit). Append affiliate sub-IDs via
// WithExtraParams or replace the set entirely via WithParams.
func DefaultParams() []string {
	return []string{
		"utm_source", "utm_medium", "utm_campaign", "utm_term", "utm_content", "utm_id",
		"gclid", "gbraid", "wbraid", "dclid",
		"msclkid",
		"fbclid",
		"ttclid",
		"twclid",
		"li_fat_id",
		"epik",
		"rdt_cid",
	}
}

type config struct {
	clk        clock.Clock
	log        *slog.Logger
	cookieName string
	params     []string
	window     time.Duration
	policy     Policy
}

// Option configures the Tracker.
type Option func(*config)

func newConfig(opts ...Option) config {
	cfg := config{
		cookieName: defaultCookieName,
		params:     DefaultParams(),
		window:     30 * 24 * time.Hour,
		policy:     LastTouch,
		clk:        clock.System(),
		log:        logger.NewNope(),
	}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// WithPolicy selects first-touch or last-touch (default LastTouch). Values
// other than FirstTouch behave as LastTouch.
func WithPolicy(p Policy) Option { return func(c *config) { c.policy = p } }

// WithWindow sets the attribution window (default 30 days): both the cookie
// lifetime and the server-side cutoff past which a stored touch no longer
// counts. Non-positive values are ignored.
func WithWindow(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.window = d
		}
	}
}

// WithCookieName overrides the touch cookie name (default
// "__Host-attribution", falling back to "attribution" when the codec policy
// can't satisfy __Host-). A custom __Host- name on a codec that can't
// satisfy the prefix rules panics in New — otherwise every best-effort
// capture would fail silently. Empty names are ignored.
func WithCookieName(name string) Option {
	return func(c *config) {
		if name != "" {
			c.cookieName = name
		}
	}
}

// WithParams replaces the captured param set. The names are copied, so a
// slice expanded into the call can be reused freely. Empty calls are
// ignored.
func WithParams(names ...string) Option {
	return func(c *config) {
		if len(names) > 0 {
			c.params = slices.Clone(names)
		}
	}
}

// WithExtraParams appends names (e.g. affiliate sub-IDs) to the captured
// param set without mutating any caller-owned backing array.
func WithExtraParams(names ...string) Option {
	return func(c *config) { c.params = append(slices.Clip(c.params), names...) }
}

// WithClock overrides the time source (default the system clock). Nil is
// ignored.
func WithClock(clk clock.Clock) Option {
	return func(c *config) {
		if clk != nil {
			c.clk = clk
		}
	}
}

// WithLogger sets the logger for best-effort capture failures, logged at
// Debug (default discards). Nil is ignored.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l != nil {
			c.log = l
		}
	}
}
