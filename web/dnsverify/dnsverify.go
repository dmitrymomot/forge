package dnsverify

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// Verifier performs single-shot DNS verification against a Resolver. Build it
// with New; it is safe for concurrent use.
type Verifier struct {
	resolver Resolver
	cfg      Config
}

// New builds a Verifier. It applies DefaultConfig, then the options, then
// returns an error if the resulting Config is invalid. The resolver defaults
// to net.DefaultResolver.
func New(opts ...Option) (*Verifier, error) {
	cf := config{cfg: DefaultConfig()}
	for _, o := range opts {
		o(&cf)
	}
	if err := cf.cfg.Validate(); err != nil {
		return nil, err
	}
	r := cf.resolver
	if r == nil {
		r = net.DefaultResolver
	}
	return &Verifier{resolver: r, cfg: cf.cfg}, nil
}

// Result reports the outcome of a Verify call. Verified is whether observed
// DNS satisfied the Challenge; Found lists the observed values at Host (for
// display/debug). With a nil error there are three states: verified
// (Verified), pending (!Verified && len(Found) == 0 — nothing published yet),
// and misconfigured (!Verified && len(Found) > 0 — published but wrong).
type Result struct {
	Found    []string
	Verified bool
}

// Verify performs one lookup-and-compare for c. A record that is not published
// yet (NXDOMAIN / empty) yields an unverified Result with a nil error; a
// genuine resolver failure returns ErrLookup; a malformed Challenge returns
// ErrInvalidChallenge. The Verifier's Timeout bounds each lookup and the
// caller's ctx cancellation is honored.
func (v *Verifier) Verify(ctx context.Context, c Challenge) (Result, error) {
	if c.Host == "" || len(c.Expect) == 0 {
		return Result{}, ErrInvalidChallenge
	}
	ctx, cancel := context.WithTimeout(ctx, v.cfg.Timeout)
	defer cancel()

	switch c.Record {
	case TXT:
		return v.verifyTXT(ctx, c)
	case CNAME:
		return v.verifyCNAME(ctx, c)
	case A:
		return v.verifyIP(ctx, c, "ip4")
	case AAAA:
		return v.verifyIP(ctx, c, "ip6")
	default:
		return Result{}, ErrInvalidChallenge
	}
}

func (v *Verifier) verifyTXT(ctx context.Context, c Challenge) (Result, error) {
	records, err := v.resolver.LookupTXT(ctx, c.Host)
	if err != nil {
		return errResult(err)
	}
	for _, rec := range records {
		for _, want := range c.Expect {
			if rec == want {
				return Result{Verified: true, Found: records}, nil
			}
		}
	}
	return Result{Found: records}, nil
}

func (v *Verifier) verifyCNAME(ctx context.Context, c Challenge) (Result, error) {
	cname, err := v.resolver.LookupCNAME(ctx, c.Host)
	if err != nil {
		return errResult(err)
	}
	got := canonicalHost(cname)
	// LookupCNAME returns the queried host itself when there is no CNAME.
	if got == canonicalHost(c.Host) {
		return Result{}, nil
	}
	for _, want := range c.Expect {
		if got == canonicalHost(want) {
			return Result{Verified: true, Found: []string{cname}}, nil
		}
	}
	return Result{Found: []string{cname}}, nil
}

func (v *Verifier) verifyIP(ctx context.Context, c Challenge, network string) (Result, error) {
	got, err := v.resolver.LookupNetIP(ctx, network, c.Host)
	if err != nil {
		return errResult(err)
	}
	want := make(map[netip.Addr]struct{}, len(c.Expect))
	for _, s := range c.Expect {
		if addr, perr := netip.ParseAddr(s); perr == nil {
			want[addr.Unmap()] = struct{}{}
		}
	}
	found := make([]string, 0, len(got))
	verified := false
	for _, ip := range got {
		found = append(found, ip.String())
		if _, ok := want[ip.Unmap()]; ok {
			verified = true
		}
	}
	if len(found) == 0 {
		return Result{}, nil // nothing resolved yet → pending
	}
	return Result{Verified: verified, Found: found}, nil
}

// errResult routes a resolver error: "not published yet" (NXDOMAIN) becomes an
// unverified Result with a nil error; anything else becomes ErrLookup.
func errResult(err error) (Result, error) {
	var d *net.DNSError
	if errors.As(err, &d) && d.IsNotFound {
		return Result{}, nil
	}
	return Result{}, fmt.Errorf("%w: %v", ErrLookup, err)
}

// canonicalHost lowercases and strips a trailing dot for CNAME comparison.
func canonicalHost(h string) string {
	return strings.ToLower(strings.TrimSuffix(h, "."))
}
