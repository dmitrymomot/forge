package hostrouter

import (
	"net/http"
	"strings"
)

// Routes maps host patterns to HTTP handlers.
// Exact: "api.example.com"
// Wildcard: "*.example.com"
type Routes map[string]http.Handler

// Router routes requests based on the Host header.
// It supports exact matches and wildcard patterns.
type Router struct {
	exact    map[string]http.Handler // "api.example.com" -> handler
	wildcard map[string]http.Handler // "example.com" -> handler (for *.example.com)
	fallback http.Handler            // default handler
}

// New creates a host router from the given routes.
// The fallback handler is used for requests that don't match any host pattern.
func New(routes Routes, fallback http.Handler) *Router {
	r := &Router{
		exact:    make(map[string]http.Handler),
		wildcard: make(map[string]http.Handler),
		fallback: fallback,
	}

	for pattern, handler := range routes {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		if strings.HasPrefix(pattern, "*.") {
			// Wildcard: "*.example.com" stored as "example.com"
			r.wildcard[pattern[2:]] = handler
		} else {
			r.exact[pattern] = handler
		}
	}

	return r
}

// ServeHTTP routes requests based on the Host header.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	host := normalizeHost(req.Host)

	// Check exact match
	if h, ok := r.exact[host]; ok {
		h.ServeHTTP(w, req)
		return
	}

	// Check wildcard (*.example.com matches foo.example.com)
	if _, domain, ok := strings.Cut(host, "."); ok {
		if h, ok := r.wildcard[domain]; ok {
			h.ServeHTTP(w, req)
			return
		}
	}

	// Fallback to default handler
	r.fallback.ServeHTTP(w, req)
}

// normalizeHost extracts and normalizes the host from the request.
// Strips port and converts to lowercase.
func normalizeHost(host string) string {
	// Strip port if present
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		// Check it's not an IPv6 address
		if !strings.Contains(host[idx:], "]") {
			host = host[:idx]
		}
	}
	return strings.ToLower(host)
}
