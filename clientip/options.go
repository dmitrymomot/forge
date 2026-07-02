package clientip

import (
	"net/netip"
	"strconv"
)

type mode int

const (
	modeRemoteAddr mode = iota
	modeSingleHeader
	modeTrustedRanges
	modeTrustedHopCount
	modeLeftmostNonPrivate
)

type config struct {
	header   string
	trusted  []netip.Prefix
	hopCount int
	mode     mode
}

// Option configures client-IP resolution. Strategy options are last-wins.
type Option func(*config)

func newConfig(opts ...Option) config {
	c := config{mode: modeRemoteAddr}
	for _, o := range opts {
		o(&c)
	}
	return c
}

// RemoteAddrOnly ignores all headers and uses RemoteAddr. This is the default.
func RemoteAddrOnly() Option { return func(c *config) { c.mode = modeRemoteAddr } }

// SingleHeader trusts one edge header (first valid IP, port stripped), falling
// back to RemoteAddr when the header is absent.
func SingleHeader(name string) Option {
	return func(c *config) { c.mode = modeSingleHeader; c.header = name }
}

// TrustedRanges enables spoof-resistant resolution: the XFF+Forwarded chain is
// walked right-to-left and the first address outside the trusted CIDRs is
// returned. It PANICS on an invalid CIDR string — trusted-proxy ranges are static
// boot config for a security control, so a typo must fail loudly at startup
// rather than silently mis-scope trust.
func TrustedRanges(cidrs ...string) Option {
	prefixes := make([]netip.Prefix, 0, len(cidrs))
	for _, s := range cidrs {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			panic("clientip: TrustedRanges: invalid CIDR " + strconv.Quote(s) + ": " + err.Error())
		}
		prefixes = append(prefixes, p)
	}
	return func(c *config) { c.mode = modeTrustedRanges; c.trusted = prefixes }
}

// TrustedHopCount returns the address n hops from the right of the XFF+Forwarded
// chain (n = number of trusted proxies in front of the app).
func TrustedHopCount(n int) Option {
	return func(c *config) { c.mode = modeTrustedHopCount; c.hopCount = n }
}

// LeftmostNonPrivate returns the leftmost public address in the chain. Best-effort
// and spoofable — for logging only, never for auth/rate-limiting.
func LeftmostNonPrivate() Option { return func(c *config) { c.mode = modeLeftmostNonPrivate } }
