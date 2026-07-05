package hostrouter

import (
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
	fallback  http.Handler
	injectCtx bool
}

// New builds a Router from options applied in order. With no WithHost options every
// request is served by the fallback (HTTP 404 unless WithFallback overrides it).
// The default 404-for-unmatched-hosts is a deliberate default-deny that protects
// against DNS rebinding. New panics on any invalid registration. It does no I/O.
func New(opts ...Option) *Router {
	r := &Router{
		exact:     make(map[string]http.Handler),
		wildcard:  make(map[string]wildcardEntry),
		fallback:  http.NotFoundHandler(),
		injectCtx: true,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// ServeHTTP routes by Host: exact match first, then a single-label wildcard, then
// the fallback. Matched routes carry a Match in the request context unless the
// Router was built with WithoutMatchContext.
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
	r.fallback.ServeHTTP(w, req) // no match: no context injected, original request
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
	if host[0] == '[' { // IPv6 literal: "[::1]" or "[::1]:8080"
		i := strings.IndexByte(host, ']')
		if i < 0 {
			return "" // malformed: opening '[' with no closing ']'
		}
		host = host[1:i] // inside brackets; drops "]" and any ":port" after it
	} else if i := strings.LastIndexByte(host, ':'); i >= 0 &&
		strings.IndexByte(host, ':') == i {
		host = host[:i] // exactly one colon => host:port (not bracketless IPv6)
	}
	host = strings.TrimSuffix(host, ".") // rooted FQDN "example.com."
	return strings.ToLower(host)
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
