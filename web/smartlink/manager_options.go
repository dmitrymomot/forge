package smartlink

import (
	"context"
	"errors"
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
	decorators      []Decorator
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
// be an absolute http(s) URL (scheme and host) without a query or fragment —
// ShortURL appends the code as a path segment, so a base like
// "https://s.example.com/?utm=x" would render unusable URLs such as
// "...?utm=x/promo". Any trailing slashes are trimmed and exactly one is
// added back, so "https://s.example.com//" can't produce a double slash in a
// rendered ShortURL. Without this option, ShortURL always returns "".
func WithBaseURL(u string) ManagerOption {
	return func(c *managerConfig) {
		if err := validateAbsoluteHTTPURL(u); err != nil {
			c.errs = append(c.errs, fmt.Errorf("smartlink: base URL: %w", err))
			return
		}
		// Checked on the raw string, not the parsed URL: a bare trailing "?"
		// or "#" parses to empty RawQuery/Fragment yet would still survive
		// into the rendered ShortURL text.
		if strings.ContainsAny(u, "?#") {
			c.errs = append(c.errs, fmt.Errorf("smartlink: base URL: must not carry a query or fragment: %q", u))
			return
		}
		c.baseURL = strings.TrimRight(u, "/") + "/"
	}
}

// validateAbsoluteHTTPURL requires u to be an absolute URL with a non-empty
// host and an http or https scheme. Shared by [WithBaseURL] and
// [WithFallbackURL], whose values are both rendered as redirect Location
// headers and so must not be relative — a typo'd relative fallback would
// silently redirect relative to the handler's own path.
func validateAbsoluteHTTPURL(u string) error {
	parsed, err := url.Parse(u)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("must be absolute (scheme and host): %q", u)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https: %q", u)
	}
	return nil
}

// WithSchemes replaces the allowed Target URL schemes (default "http",
// "https"). Create rejects a Target whose macro-elided scheme is not in
// this set, and [Manager.Handler] re-checks it before serving a Target-backed
// redirect, so a row written to the Store directly cannot smuggle a
// disallowed scheme past the allowlist. Scheme comparison is
// case-insensitive, since [url.Parse] always lowercases the parsed scheme.
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
// verbatim — single-tenant zero ceremony. A nil f is a NewManager error, not
// a silent fall-back to unscoped: a caller who asked for scoping must never
// run without it.
func WithScope(f func(ctx context.Context) (string, error)) ManagerOption {
	return func(c *managerConfig) {
		if f == nil {
			c.errs = append(c.errs, errors.New("smartlink: nil scope func"))
			return
		}
		c.scope = f
	}
}

// WithDecorators sets the [Decorator] chain [Manager.Handler] wraps around
// every link's Decider — Target- and Ref-backed alike — composed with [Chain]
// (first argument outermost). This is the synchronous seam for Target links,
// which have no [Resolver] to decorate; Ref-specific decorators can still
// live in the Resolver (e.g. [Cache.Resolver]) and run inside this chain,
// since this chain wraps the Decider the Resolver returns.
func WithDecorators(ds ...Decorator) ManagerOption {
	return func(c *managerConfig) { c.decorators = slices.Clone(ds) }
}

// WithLinkParamPolicy sets the [ParamPolicy] applied to Target-backed Links:
// Create validates a Target template against it, and [Manager.Handler]
// compiles every Target-link redirect with it, so it governs how visit
// params (sub-IDs, click IDs) merge into the final URL on every hit. Default
// [ParamsFill] — the zero value [ParamsDrop] would silently strip forwarded
// params at decide time. An out-of-range value is a NewManager error, like
// every other validated option, rather than a per-Create/per-hit failure.
func WithLinkParamPolicy(p ParamPolicy) ManagerOption {
	return func(c *managerConfig) {
		switch p {
		case ParamsDrop, ParamsFill, ParamsOverride:
			c.linkParamPolicy = p
		default:
			c.errs = append(c.errs, fmt.Errorf("smartlink: unknown ParamPolicy %d", p))
		}
	}
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
// goroutines, since a slow callback blocks every click. The callback's
// context is cancellation-detached from the request: a client disconnecting
// right after the redirect cannot cancel hit delivery mid-push.
func WithOnHit(f func(context.Context, Hit)) ManagerOption {
	return func(c *managerConfig) { c.onHit = f }
}

// WithCache configures the read-through Link cache [Manager.Resolve] (and
// so [Manager.Handler]) consults before the Store: cs is the backing
// [cache.Store] and ttl bounds how long a cached record is reused. Cache
// errors degrade to Store reads; lifecycle mutations best-effort evict, so
// a warmed entry is stale for at most ttl. ttl must be positive: [cache.WithTTL]
// treats a non-positive ttl as "never expire", which would let a cached
// entry that survives a failed eviction (see [Manager.invalidateCache])
// serve forever instead of bounding its staleness — a non-positive ttl is a
// NewManager error.
func WithCache(cs cache.Store, ttl time.Duration) ManagerOption {
	return func(c *managerConfig) {
		if ttl <= 0 {
			c.errs = append(c.errs, fmt.Errorf("smartlink: cache ttl must be positive, got %s", ttl))
			return
		}
		c.cacheStore = cs
		c.cacheTTL = ttl
	}
}

// WithFallbackURL sets the destination [Manager.Handler] redirects to for a
// dead link (unknown, expired, or deactivated code, or a Ref the resolver
// reports as [ErrNoTarget]). Without it, dead links answer 404. Like
// [WithBaseURL], it must be an absolute http(s) URL (scheme and host): a
// typo'd relative value would silently redirect relative to the handler's
// own path instead of failing construction.
func WithFallbackURL(u string) ManagerOption {
	return func(c *managerConfig) {
		if err := validateAbsoluteHTTPURL(u); err != nil {
			c.errs = append(c.errs, fmt.Errorf("smartlink: fallback URL: %w", err))
			return
		}
		c.fallbackURL = u
	}
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
