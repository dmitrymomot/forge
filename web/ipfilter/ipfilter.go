package ipfilter

import (
	"net/http"
	"net/netip"

	"github.com/dmitrymomot/forge/web/clientip"
	"github.com/dmitrymomot/forge/web/middleware"
	"github.com/dmitrymomot/forge/web/problem"
)

// New returns middleware that allows or blocks requests by client IP using a
// deny-wins model (see WithAllow / WithDeny). The client IP is resolved per
// WithClientIP; a blocked request is answered by the responder (default
// problem.JSON 403). New panics (wrapping ErrInvalidCIDR) on an unparseable
// allow/deny entry — a wiring bug, not a runtime condition.
func New(opts ...Option) middleware.Middleware {
	cfg := config{responder: problem.JSON(problem.WithStatus(http.StatusForbidden))}
	for _, o := range opts {
		o(&cfg)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			addr, resolved := parseAddr(clientip.Resolve(r, cfg.ipOpts...))
			if cfg.allowed(addr, resolved) {
				next.ServeHTTP(w, r)
				return
			}
			cfg.responder(w, r, ErrBlocked)
		})
	}
}

// allowed applies the deny-wins model: deny always blocks; a configured
// allowlist is a default-deny gate; with no allowlist everything not denied
// passes. resolved is false when the client IP could not be parsed.
func (c config) allowed(addr netip.Addr, resolved bool) bool {
	if resolved && contains(c.deny, addr) {
		return false
	}
	if len(c.allow) > 0 {
		return resolved && contains(c.allow, addr)
	}
	return true
}

func contains(prefixes []netip.Prefix, addr netip.Addr) bool {
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func parseAddr(s string) (netip.Addr, bool) {
	if s == "" {
		return netip.Addr{}, false
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, false
	}
	return a.Unmap(), true
}
