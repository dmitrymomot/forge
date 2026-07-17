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

// VisitFunc builds or decorates a [Visit] from the inbound request. A
// Handler (a later task) calls it before Decide; the Visit passed in is the
// zero value on the first call in a chain.
type VisitFunc func(*http.Request, Visit) Visit

// Hit is delivered to a [WithOnHit] callback (a later task) after a redirect
// decision has been made: the resolved Link, the Visit built for the
// request, and the Decision Decide returned.
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

// WithVisitFunc sets the [VisitFunc] a Handler (a later task) uses to build
// the [Visit] for each request.
func WithVisitFunc(f VisitFunc) ManagerOption {
	return func(c *managerConfig) { c.visitFunc = f }
}

// WithOnHit registers a callback a Handler (a later task) invokes after each
// redirect decision, for click logging or analytics.
func WithOnHit(f func(context.Context, Hit)) ManagerOption {
	return func(c *managerConfig) { c.onHit = f }
}

// WithCache configures the read-through compile cache a Handler (a later
// task) uses when resolving Ref-backed Links: cs is the backing
// [cache.Store] and ttl bounds how long a resolved entry is reused.
func WithCache(cs cache.Store, ttl time.Duration) ManagerOption {
	return func(c *managerConfig) {
		c.cacheStore = cs
		c.cacheTTL = ttl
	}
}

// WithFallbackURL sets the destination a Handler (a later task) redirects to
// when a code cannot be resolved to a live decision.
func WithFallbackURL(u string) ManagerOption {
	return func(c *managerConfig) { c.fallbackURL = u }
}

// WithRedirectStatus sets the HTTP status a Handler (a later task) uses for
// a successful redirect. Only 302 (Found, the default) and 307 (Temporary
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
