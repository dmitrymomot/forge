package hostrouter

import (
	"errors"
	"net/http"
	"strings"
)

// wildcardEntry stores a wildcard handler together with its pre-built "*."+parent
// pattern, so ServeHTTP never concatenates on the hot path.
type wildcardEntry struct {
	handler http.Handler
	pattern string
}

// Router dispatches by Host header. It is immutable after New and safe for
// concurrent use. It implements http.Handler.
type Router struct {
	exact     map[string]http.Handler
	wildcard  map[string]wildcardEntry
	lookup    LookupFunc
	lookupErr LookupErrorHandler
	fallback  http.Handler
	injectCtx bool
}

// LookupErrorHandler answers a request whose LookupFunc failed. WithLookupErrorHandler
// replaces the default, which answers 503 and writes no detail of the error.
type LookupErrorHandler func(w http.ResponseWriter, r *http.Request, err error)

func serviceUnavailable(w http.ResponseWriter, _ *http.Request, _ error) {
	http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
}

// New builds a Router from options applied in order. With no WithHost options every
// request is served by the fallback (HTTP 404 unless WithFallback overrides it).
// The default 404-for-unmatched-hosts is a deliberate default-deny that protects
// against DNS rebinding. New reports every invalid registration as a joined error
// (ErrNilHandler, ErrInvalidPattern, ErrDuplicateHost, ErrNilLookup). It does no I/O.
func New(opts ...Option) (*Router, error) {
	c := config{
		exact:     make(map[string]http.Handler),
		wildcard:  make(map[string]wildcardEntry),
		fallback:  http.NotFoundHandler(),
		lookupErr: serviceUnavailable,
		injectCtx: true,
	}
	for _, opt := range opts {
		opt(&c)
	}
	if err := errors.Join(c.errs...); err != nil {
		return nil, err
	}
	return &Router{
		exact:     c.exact,
		wildcard:  c.wildcard,
		lookup:    c.lookup,
		lookupErr: c.lookupErr,
		fallback:  c.fallback,
		injectCtx: c.injectCtx,
	}, nil
}

// ServeHTTP routes by Host: exact match first, then a single-label wildcard, then the
// WithLookup resolver, then the fallback. An unmatched host reaches the fallback with
// its original request and no injected context. Matched routes carry a Match in the
// request context unless the Router was built with WithoutMatchContext.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	host := normalizeHost(req.Host)
	if h, ok := r.exact[host]; ok {
		r.serve(w, req, h, Match{Host: host, Pattern: host})
		return
	}
	if label, parent, ok := splitFirstLabel(host); ok {
		if e, found := r.wildcard[parent]; found {
			r.serve(w, req, e.handler, Match{Host: host, Pattern: e.pattern, Subdomain: label})
			return
		}
	}
	if r.lookup != nil && host != "" {
		h, err := r.lookup(req.Context(), host)
		if err != nil && !errors.Is(err, ErrHostNotFound) {
			r.lookupErr(w, req, err)
			return
		}
		if err == nil && h != nil {
			r.serve(w, req, h, Match{Host: host})
			return
		}
	}
	r.fallback.ServeHTTP(w, req)
}

// serve dispatches to h, injecting m into the request context unless the Router was
// built with WithoutMatchContext.
func (r *Router) serve(w http.ResponseWriter, req *http.Request, h http.Handler, m Match) {
	if r.injectCtx {
		req = req.WithContext(&matchCtx{Context: req.Context(), m: m})
	}
	h.ServeHTTP(w, req)
}

// normalizeHost lowercases the host, strips any port, removes IPv6 brackets, and
// trims a trailing FQDN dot. It allocates only on strings.ToLower's slow path
// (uppercase input); an already-lowercase host returns sub-slices with no copy.
//
// net.SplitHostPort is deliberately avoided: it allocates an *AddrError whenever
// the host has no port (the common proxied/HTTP2 case).
func normalizeHost(host string) string {
	if host == "" {
		return ""
	}
	if host[0] == '[' {
		return trimIPv6Brackets(host)
	}
	if i := strings.LastIndexByte(host, ':'); i >= 0 && strings.IndexByte(host, ':') == i {
		host = host[:i]
	}
	return strings.ToLower(strings.TrimSuffix(host, "."))
}

// trimIPv6Brackets returns the address inside a bracketed IPv6 literal such as
// "[::1]" or "[::1]:8080", dropping the closing bracket and any port after it. A
// literal with no closing bracket is malformed and returns "".
func trimIPv6Brackets(host string) string {
	i := strings.IndexByte(host, ']')
	if i < 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(host[1:i], "."))
}

// splitFirstLabel splits "foo.example.com" into ("foo", "example.com", true).
// ok is false when there is no dot, a leading dot, or a trailing dot.
func splitFirstLabel(host string) (label, parent string, ok bool) {
	i := strings.IndexByte(host, '.')
	if i <= 0 || i == len(host)-1 {
		return "", "", false
	}
	return host[:i], host[i+1:], true
}
