package hostrouter

import (
	"fmt"
	"net/http"
	"strings"
)

// Option configures a Router. Options apply in order and panic on invalid input.
type Option func(*Router)

// WithHost registers handler h for pattern, an exact host ("api.example.com") or a
// single-label wildcard ("*.example.com"). The pattern is normalized identically to
// incoming hosts, so casing/port/trailing-dot never cause a mismatch. It panics
// (ErrNilHandler / ErrInvalidPattern / ErrDuplicateHost) on invalid input.
func WithHost(pattern string, h http.Handler) Option {
	return func(r *Router) {
		if h == nil {
			panic(fmt.Errorf("%w: %q", ErrNilHandler, pattern))
		}
		if strings.HasPrefix(pattern, "*.") {
			parent := normalizeHost(pattern[2:])
			if parent == "" || strings.ContainsRune(parent, '*') {
				panic(fmt.Errorf("%w: %q", ErrInvalidPattern, pattern))
			}
			if _, dup := r.wildcard[parent]; dup {
				panic(fmt.Errorf("%w: %q", ErrDuplicateHost, pattern))
			}
			r.wildcard[parent] = wildcardEntry{handler: h, pattern: "*." + parent}
			return
		}
		host := normalizeHost(pattern)
		if host == "" || strings.ContainsRune(host, '*') {
			panic(fmt.Errorf("%w: %q", ErrInvalidPattern, pattern))
		}
		if _, dup := r.exact[host]; dup {
			panic(fmt.Errorf("%w: %q", ErrDuplicateHost, pattern))
		}
		r.exact[host] = h
	}
}

// WithFallback sets the handler for unmatched hosts. The default is
// http.NotFoundHandler() (404). It panics (ErrNilHandler) if h is nil. Last wins.
func WithFallback(h http.Handler) Option {
	return func(r *Router) {
		if h == nil {
			panic(fmt.Errorf("%w: fallback handler", ErrNilHandler))
		}
		r.fallback = h
	}
}

// WithoutMatchContext disables match-context injection for the Router, making the
// matched path zero-allocation (no http.Request copy). FromContext and the
// Subdomain/Pattern/Host accessors then return zero values.
func WithoutMatchContext() Option {
	return func(r *Router) { r.injectCtx = false }
}
