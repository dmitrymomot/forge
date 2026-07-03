package clientip

import (
	"net/netip"
	"slices"
)

// privateRanges are the ranges TrustPrivateProxies trusts and LeftmostNonPrivate
// skips: RFC 1918, loopback, link-local, CGNAT (RFC 6598), IPv6 loopback,
// link-local, and ULA (RFC 4193).
var privateRanges = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("fc00::/7"),
}

// PrivateRanges returns a copy of the private/loopback/link-local/CGNAT/ULA
// prefixes, for composing a custom TrustedRanges.
func PrivateRanges() []netip.Prefix { return slices.Clone(privateRanges) }

func inPrefixes(addr netip.Addr, prefixes []netip.Prefix) bool {
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func isPrivate(addr netip.Addr) bool { return inPrefixes(addr, privateRanges) }
