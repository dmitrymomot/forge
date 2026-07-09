package ipfilter

import (
	"fmt"
	"net/netip"

	"github.com/dmitrymomot/forge/web/clientip"
	"github.com/dmitrymomot/forge/web/problem"
)

type config struct {
	responder problem.Responder
	allow     []netip.Prefix
	deny      []netip.Prefix
	ipOpts    []clientip.Option
}

// Option configures New.
type Option func(*config)

// WithAllow adds CIDRs or bare IPs to the allowlist. A bare IP becomes a /32
// (IPv4) or /128 (IPv6). New panics (wrapping ErrInvalidCIDR) on an unparseable
// entry.
func WithAllow(cidrs ...string) Option {
	return func(c *config) { c.allow = append(c.allow, mustParse(cidrs)...) }
}

// WithDeny adds CIDRs or bare IPs to the denylist. Deny always wins over allow.
func WithDeny(cidrs ...string) Option {
	return func(c *config) { c.deny = append(c.deny, mustParse(cidrs)...) }
}

// WithClientIP configures how the client IP is resolved (proxy/trust settings).
// Without it, resolution is safe-by-default (RemoteAddr only).
func WithClientIP(opts ...clientip.Option) Option {
	return func(c *config) { c.ipOpts = append(c.ipOpts, opts...) }
}

// WithResponder overrides the rejection response (default problem.JSON 403).
func WithResponder(r problem.Responder) Option {
	return func(c *config) {
		if r != nil {
			c.responder = r
		}
	}
}

func mustParse(cidrs []string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(cidrs))
	for _, s := range cidrs {
		p, err := parsePrefix(s)
		if err != nil {
			panic(fmt.Errorf("%w: %q", ErrInvalidCIDR, s))
		}
		out = append(out, p)
	}
	return out
}

// parsePrefix accepts a CIDR ("10.0.0.0/8") or a bare address ("10.0.0.1"),
// returning a masked prefix so Contains matches correctly.
func parsePrefix(s string) (netip.Prefix, error) {
	if p, err := netip.ParsePrefix(s); err == nil {
		return p.Masked(), nil
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(addr.Unmap(), addr.Unmap().BitLen()), nil
}
