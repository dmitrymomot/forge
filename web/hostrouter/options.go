package hostrouter

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// config accumulates the registrations and the errors New reports.
type config struct {
	exact     map[string]http.Handler
	wildcard  map[string]wildcardEntry
	lookup    LookupFunc
	lookupErr LookupErrorHandler
	fallback  http.Handler
	errs      []error
	injectCtx bool
}

// Option configures a Router. Options apply in order; invalid values accumulate and
// are returned by New.
type Option func(*config)

// WithHost registers handler h for pattern, an exact host ("api.example.com") or a
// single-label wildcard ("*.example.com"). The pattern is normalized identically to
// incoming hosts, so casing/port/trailing-dot never cause a mismatch. New reports
// ErrNilHandler, ErrInvalidPattern, or ErrDuplicateHost for invalid input.
func WithHost(pattern string, h http.Handler) Option {
	return func(c *config) {
		if h == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: %q", ErrNilHandler, pattern))
			return
		}
		if strings.HasPrefix(pattern, "*.") {
			c.registerWildcard(pattern, h)
			return
		}
		host := normalizeHost(pattern)
		if host == "" || strings.ContainsRune(host, '*') {
			c.errs = append(c.errs, fmt.Errorf("%w: %q", ErrInvalidPattern, pattern))
			return
		}
		if _, dup := c.exact[host]; dup {
			c.errs = append(c.errs, fmt.Errorf("%w: %q", ErrDuplicateHost, pattern))
			return
		}
		c.exact[host] = h
	}
}

func (c *config) registerWildcard(pattern string, h http.Handler) {
	parent := normalizeHost(pattern[2:])
	if parent == "" || strings.ContainsRune(parent, '*') {
		c.errs = append(c.errs, fmt.Errorf("%w: %q", ErrInvalidPattern, pattern))
		return
	}
	if _, dup := c.wildcard[parent]; dup {
		c.errs = append(c.errs, fmt.Errorf("%w: %q", ErrDuplicateHost, pattern))
		return
	}
	c.wildcard[parent] = wildcardEntry{handler: h, pattern: "*." + parent}
}

// LookupFunc resolves a host that no registered pattern matches, which is how a
// customer domain reaches its handler. The Router calls it with the request context
// and the normalized host, and skips it for an empty host. Return the handler for a
// known host, ErrHostNotFound or a nil handler for an unknown one, and any other
// error for a store that failed.
type LookupFunc func(ctx context.Context, host string) (http.Handler, error)

// WithLookup sets the resolver for a host that no registered pattern matches. The
// Router tries the exact hosts first, the wildcard parents second, and the lookup
// third, so a customer domain never shadows a platform pattern. A hit carries a Match
// with the host and no pattern, which tells a handler the request arrived on a
// customer domain. An unknown host reaches the fallback; a failed lookup reaches
// WithLookupErrorHandler. New reports a nil lookup as ErrNilLookup. Last wins.
//
//	router, err := hostrouter.New(
//		hostrouter.WithHost("*.example.com", tenantMux),
//		hostrouter.WithLookup(func(ctx context.Context, host string) (http.Handler, error) {
//			if _, err := domains.Get(ctx, host); err != nil {
//				if errors.Is(err, sql.ErrNoRows) {
//					return nil, hostrouter.ErrHostNotFound
//				}
//				return nil, err
//			}
//			return tenantMux, nil
//		}),
//	)
func WithLookup(lookup LookupFunc) Option {
	return func(c *config) {
		if lookup == nil {
			c.errs = append(c.errs, ErrNilLookup)
			return
		}
		c.lookup = lookup
	}
}

// WithLookupErrorHandler sets the handler for a lookup that failed. The default
// answers 503 and writes no detail of the error. A failed lookup must never read as
// an unknown host: a store that is down would then 404 every customer domain off the
// platform. New reports a nil handler as ErrNilHandler. Last wins.
func WithLookupErrorHandler(h LookupErrorHandler) Option {
	return func(c *config) {
		if h == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: lookup error handler", ErrNilHandler))
			return
		}
		c.lookupErr = h
	}
}

// WithFallback sets the handler for unmatched hosts. The default is
// http.NotFoundHandler() (404), a default-deny that guards against DNS rebinding;
// overriding it to serve real content means unmatched (possibly rebound) hosts reach
// h, so validate the Host inside h if it exposes anything sensitive. New reports a
// nil handler as ErrNilHandler. Last wins.
func WithFallback(h http.Handler) Option {
	return func(c *config) {
		if h == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: fallback handler", ErrNilHandler))
			return
		}
		c.fallback = h
	}
}

// WithoutMatchContext disables match-context injection for the Router, making the
// matched path zero-allocation (no http.Request copy). FromContext and the
// Subdomain/Pattern/Host accessors then return zero values.
func WithoutMatchContext() Option {
	return func(c *config) { c.injectCtx = false }
}
