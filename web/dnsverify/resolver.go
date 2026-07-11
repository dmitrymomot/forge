package dnsverify

import (
	"context"
	"net"
	"net/netip"
)

// Resolver is the DNS seam. *net.Resolver satisfies it structurally, so
// net.DefaultResolver is the zero-config default and a custom *net.Resolver
// (own dialer/DNS server) drops in unchanged.
type Resolver interface {
	LookupTXT(ctx context.Context, host string) ([]string, error)
	LookupCNAME(ctx context.Context, host string) (string, error)
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// StaticResolver is an in-memory Resolver for tests. Configure it entirely at
// construction with the With* options; it is immutable afterward and safe to
// share across goroutines. Unknown hosts resolve as not-found (IsNotFound),
// which Verify reports as an unverified Result with a nil error.
type StaticResolver struct {
	txt   map[string][]string
	cname map[string]string
	ips   map[string][]netip.Addr
	errs  map[string]error
}

// StaticOption configures a StaticResolver. It is distinct from the Verifier's
// Option type.
type StaticOption func(*StaticResolver)

// NewStaticResolver builds an in-memory resolver from the given records.
func NewStaticResolver(opts ...StaticOption) *StaticResolver {
	r := &StaticResolver{
		txt:   map[string][]string{},
		cname: map[string]string{},
		ips:   map[string][]netip.Addr{},
		errs:  map[string]error{},
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// WithTXT adds TXT values at host. Repeatable — each call appends, modeling
// multiple TXT records at one host.
func WithTXT(host string, values ...string) StaticOption {
	return func(r *StaticResolver) { r.txt[host] = append(r.txt[host], values...) }
}

// WithCNAME sets the CNAME target at host.
func WithCNAME(host, target string) StaticOption {
	return func(r *StaticResolver) { r.cname[host] = target }
}

// WithIP adds A/AAAA addresses at host; the family is inferred per address at
// lookup time.
func WithIP(host string, ips ...netip.Addr) StaticOption {
	return func(r *StaticResolver) { r.ips[host] = append(r.ips[host], ips...) }
}

// WithLookupError makes every lookup for host return err — used to exercise
// the ErrLookup path.
func WithLookupError(host string, err error) StaticOption {
	return func(r *StaticResolver) { r.errs[host] = err }
}

func notFound(host string) error {
	return &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

// LookupTXT implements Resolver.
func (r *StaticResolver) LookupTXT(_ context.Context, host string) ([]string, error) {
	if err := r.errs[host]; err != nil {
		return nil, err
	}
	v, ok := r.txt[host]
	if !ok {
		return nil, notFound(host)
	}
	return v, nil
}

// LookupCNAME implements Resolver.
func (r *StaticResolver) LookupCNAME(_ context.Context, host string) (string, error) {
	if err := r.errs[host]; err != nil {
		return "", err
	}
	v, ok := r.cname[host]
	if !ok {
		return "", notFound(host)
	}
	return v, nil
}

// LookupNetIP implements Resolver. network is "ip4", "ip6", or "ip".
func (r *StaticResolver) LookupNetIP(_ context.Context, network, host string) ([]netip.Addr, error) {
	if err := r.errs[host]; err != nil {
		return nil, err
	}
	all, ok := r.ips[host]
	if !ok {
		return nil, notFound(host)
	}
	out := make([]netip.Addr, 0, len(all))
	for _, ip := range all {
		switch network {
		case "ip4":
			if ip.Is4() {
				out = append(out, ip)
			}
		case "ip6":
			if !ip.Is4() {
				out = append(out, ip)
			}
		default:
			out = append(out, ip)
		}
	}
	if len(out) == 0 {
		return nil, notFound(host)
	}
	return out, nil
}
