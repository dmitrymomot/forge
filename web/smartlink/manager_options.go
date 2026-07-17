package smartlink

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/ops/logger"
	"github.com/dmitrymomot/forge/resilience/cache"
)

// VisitFunc builds or decorates a [Visit] from the inbound request.
// [Manager.Handler] calls it before Decide, passing a Visit whose Params
// are already pre-filled from the query string (first value per key).
type VisitFunc func(*http.Request, Visit) Visit

// Hit is delivered to a [WithOnHit] callback after [Manager.Handler] writes
// a redirect: the resolved Link, the Visit built for the request, and the
// Decision Decide returned.
type Hit struct {
	Link     Link
	Visit    Visit
	Decision Decision
}

// managerConfig holds NewManager's resolved options. A ManagerOption that
// fails validation appends to errs instead of returning an error directly —
// options compose as plain function calls — and NewManager joins and
// surfaces errs once every option has run.
type managerConfig struct {
	codeFunc        func() string
	scope           func(context.Context) (string, error)
	resolver        Resolver
	visitFunc       VisitFunc
	onHit           func(context.Context, Hit)
	cacheStore      cache.Store
	logger          *slog.Logger
	reserved        map[string]struct{}
	baseURL         string
	fallbackURL     string
	schemes         []string
	errs            []error
	cacheTTL        time.Duration
	linkParamPolicy ParamPolicy
	redirectStatus  int
}

// ManagerOption configures [NewManager].
type ManagerOption func(*managerConfig)

// newManagerConfig returns a managerConfig seeded with every documented
// default, ready for ManagerOption values to override.
func newManagerConfig() *managerConfig {
	return &managerConfig{
		codeFunc:        func() string { return id.NewShort().StringLower() },
		schemes:         []string{"http", "https"},
		reserved:        newReservedSet(),
		linkParamPolicy: ParamsFill,
		redirectStatus:  http.StatusFound,
		logger:          logger.NewNope(),
	}
}

// WithCodeFunc overrides the generated-code generator. Default: a lowercase
// Crockford [github.com/dmitrymomot/forge/core/id.Short] via
// id.NewShort().StringLower(). A nil f is ignored.
func WithCodeFunc(f func() string) ManagerOption {
	return func(c *managerConfig) {
		if f != nil {
			c.codeFunc = f
		}
	}
}

// WithBaseURL sets the base [Manager.ShortURL] renders a code onto. It must
// be an absolute http(s) URL (scheme and host); a trailing slash is added if
// missing. Without this option, ShortURL always returns "".
func WithBaseURL(u string) ManagerOption {
	return func(c *managerConfig) {
		parsed, err := url.Parse(u)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			c.errs = append(c.errs, fmt.Errorf("smartlink: base URL must be absolute (scheme and host): %q", u))
			return
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			c.errs = append(c.errs, fmt.Errorf("smartlink: base URL scheme must be http or https: %q", u))
			return
		}
		c.baseURL = strings.TrimSuffix(u, "/") + "/"
	}
}

// WithSchemes replaces the allowed Target URL schemes (default "http",
// "https"). Create rejects a Target whose macro-elided scheme is not in
// this set; scheme comparison is case-insensitive, since [url.Parse] always
// lowercases the parsed scheme.
func WithSchemes(s ...string) ManagerOption {
	return func(c *managerConfig) {
		schemes := slices.Clone(s)
		for i, sch := range schemes {
			schemes[i] = strings.ToLower(sch)
		}
		c.schemes = schemes
	}
}

// WithReservedCodes extends the default vanity-code blocklist.
func WithReservedCodes(codes ...string) ManagerOption {
	return func(c *managerConfig) {
		for _, code := range codes {
			c.reserved[code] = struct{}{}
		}
	}
}

// WithScope derives a tenant scope from the request context on every
// management call, failing closed with [ErrScope] when the hook errors or
// returns an empty string. Without this option, tenant strings pass through
// verbatim — single-tenant zero ceremony.
func WithScope(f func(ctx context.Context) (string, error)) ManagerOption {
	return func(c *managerConfig) { c.scope = f }
}

// WithLinkParamPolicy sets the [ParamPolicy] Create uses only to validate a
// Target template against a degenerate [Spec]. Default [ParamsFill] — the
// zero value [ParamsDrop] would validate a Target that silently drops
// forwarded params at decide time.
func WithLinkParamPolicy(p ParamPolicy) ManagerOption {
	return func(c *managerConfig) { c.linkParamPolicy = p }
}

// WithResolver sets the [Resolver] Create uses to confirm a Ref-backed
// Link's Ref names a resolvable [Spec] before it is stored. Skipped when
// [CreateParams.SkipRefCheck] is set, or when no Resolver is configured.
func WithResolver(r Resolver) ManagerOption {
	return func(c *managerConfig) { c.resolver = r }
}

// WithVisitFunc sets the [VisitFunc] [Manager.Handler] uses to enrich the
// [Visit] for each request.
func WithVisitFunc(f VisitFunc) ManagerOption {
	return func(c *managerConfig) { c.visitFunc = f }
}

// WithOnHit registers a callback [Manager.Handler] invokes synchronously
// after each redirect is written. Hand the [Hit] to a bounded sink (queue
// push, buffered channel) — never do work inline or spawn per-hit
// goroutines, since a slow callback blocks every click.
func WithOnHit(f func(context.Context, Hit)) ManagerOption {
	return func(c *managerConfig) { c.onHit = f }
}

// WithCache configures the read-through Link cache [Manager.Resolve] (and
// so [Manager.Handler]) consults before the Store: cs is the backing
// [cache.Store] and ttl bounds how long a cached record is reused. Cache
// errors degrade to Store reads; lifecycle mutations best-effort evict, so
// a warmed entry is stale for at most ttl.
func WithCache(cs cache.Store, ttl time.Duration) ManagerOption {
	return func(c *managerConfig) {
		c.cacheStore = cs
		c.cacheTTL = ttl
	}
}

// WithFallbackURL sets the destination [Manager.Handler] redirects to for a
// dead link (unknown, expired, or deactivated code, or a Ref the resolver
// reports as [ErrNoTarget]). Without it, dead links answer 404.
func WithFallbackURL(u string) ManagerOption {
	return func(c *managerConfig) { c.fallbackURL = u }
}

// WithRedirectStatus sets the HTTP status [Manager.Handler] uses for a
// successful redirect. Only 302 (Found, the default) and 307 (Temporary
// Redirect) are accepted: a 301 (permanent) would let browsers cache a
// destination that can change on the next rule evaluation. Any other value
// is a NewManager error.
func WithRedirectStatus(code int) ManagerOption {
	return func(c *managerConfig) {
		if code != http.StatusFound && code != http.StatusTemporaryRedirect {
			c.errs = append(c.errs, fmt.Errorf("smartlink: redirect status %d must be 302 or 307", code))
			return
		}
		c.redirectStatus = code
	}
}

// WithLogger sets the diagnostic logger. Default [logger.NewNope] (silent).
// A nil l is ignored.
func WithLogger(l *slog.Logger) ManagerOption {
	return func(c *managerConfig) {
		if l != nil {
			c.logger = l
		}
	}
}
