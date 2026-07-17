package shortlink

import (
	"context"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/resilience/cache"
)

type config struct {
	scope          func(context.Context) (string, error)
	cache          cache.Store
	onHit          func(context.Context, Link)
	reserved       map[string]struct{}
	schemes        map[string]struct{}
	fallbackURL    string
	cacheTTL       time.Duration
	codeLength     int
	redirectStatus int
}

// Option configures New.
type Option func(*config)

// WithScope derives the tenant from context for every management operation:
// Create stamps it, List is confined to it, and Get/Deactivate/Activate/
// Delete report ErrNotFound for other tenants' links. Fail-closed: a hook
// error or empty tenant fails the operation with ErrScope. Resolve and the
// redirect Handler are unaffected — a short code is a public URL. A nil fn
// leaves the manager unscoped.
func WithScope(fn func(context.Context) (string, error)) Option {
	return func(c *config) { c.scope = fn }
}

// WithCache enables read-through caching of Resolve lookups: hits are
// served from the cache store, misses fall through to the Store and are
// cached for the WithCacheTTL duration. Deactivate, Activate, and Delete
// invalidate the cached entry. A nil store disables caching.
func WithCache(store cache.Store) Option {
	return func(c *config) { c.cache = store }
}

// WithCacheTTL bounds how long a resolved link is cached (default 5m).
// Only meaningful together with WithCache. New panics on a non-positive d.
func WithCacheTTL(d time.Duration) Option {
	return func(c *config) { c.cacheTTL = d }
}

// WithCodeLength sets the generated code length (default 7, giving 58^7 ≈
// 2.2e12 codes). New panics outside [4, 32]. Vanity codes are unaffected.
func WithCodeLength(n int) Option {
	return func(c *config) { c.codeLength = n }
}

// WithSchemes replaces the destination scheme allowlist (default http and
// https). Schemes are matched case-insensitively. New panics on an empty
// list.
func WithSchemes(schemes ...string) Option {
	return func(c *config) {
		c.schemes = make(map[string]struct{}, len(schemes))
		for _, s := range schemes {
			c.schemes[strings.ToLower(s)] = struct{}{}
		}
	}
}

// WithReservedCodes extends the default reserved-word blocklist that vanity
// codes are checked against (case-insensitively).
func WithReservedCodes(codes ...string) Option {
	return func(c *config) {
		for _, code := range codes {
			c.reserved[strings.ToLower(code)] = struct{}{}
		}
	}
}

// WithFallbackURL redirects failed resolves (unknown, expired, or
// deactivated codes) in the Handler to u — a branded "link gone" page —
// instead of responding 404. Relative paths are allowed.
func WithFallbackURL(u string) Option {
	return func(c *config) { c.fallbackURL = u }
}

// WithRedirectStatus sets the Handler's redirect status (default
// http.StatusFound, 302). New panics on anything but 302 or 307 — a
// cacheable 301 would freeze the destination in browser caches and kill
// hit counting forever.
func WithRedirectStatus(status int) Option {
	return func(c *config) { c.redirectStatus = status }
}

// WithOnHit registers a hook fired synchronously on every successful
// Resolve with the resolved link. It is the click-counting seam — the hook
// emits, counting stays the caller's job (bump a counter, push to a queue).
// Keep it fast or offload; it runs on the redirect hot path.
func WithOnHit(fn func(context.Context, Link)) Option {
	return func(c *config) { c.onHit = fn }
}
