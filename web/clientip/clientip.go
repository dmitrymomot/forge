package clientip

import (
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

// Resolve returns the client IP for r per opts, or "" if nothing parses. With no
// options it is safe-by-default: RemoteAddr only. Header-trusting strategies must
// be requested explicitly (SingleHeader, TrustedRanges, a provider preset, ...).
func Resolve(r *http.Request, opts ...Option) string {
	return newConfig(opts...).resolve(r)
}

func (c config) resolve(r *http.Request) string {
	switch c.mode {
	case modeSingleHeader:
		if ip := firstHeaderIP(r, c.header); ip != "" {
			return ip
		}
		return remoteHost(r.RemoteAddr)
	case modeTrustedRanges:
		return rightmostUntrusted(c.buildChain(r), c.trusted)
	case modeTrustedHopCount:
		return nthFromRight(c.buildChain(r), c.hopCount, r)
	case modeLeftmostNonPrivate:
		return leftmostNonPrivate(c.buildChain(r), r)
	default: // modeRemoteAddr
		return remoteHost(r.RemoteAddr)
	}
}

// buildChain returns the ordered forwarding chain: every X-Forwarded-For entry
// (across repeated header lines), then — only when TrustForwardedHeader is set —
// every RFC 7239 Forwarded "for=" entry, then RemoteAddr. Left is closest to the
// client; right is the nearest proxy. Forwarded is opt-in because most edges manage
// only XFF, and mixing an unmanaged (client-forgeable) header into the trust chain
// would defeat the spoof-resistance of the trusted modes.
func (c config) buildChain(r *http.Request) []string {
	var chain []string
	for _, line := range r.Header.Values("X-Forwarded-For") {
		for part := range strings.SplitSeq(line, ",") {
			chain = append(chain, part)
		}
	}
	if c.trustForwarded {
		for _, line := range r.Header.Values("Forwarded") {
			chain = append(chain, forwardedFors(line)...)
		}
	}
	return append(chain, r.RemoteAddr)
}

// forwardedFors extracts every for= value from one RFC 7239 Forwarded header line.
func forwardedFors(v string) []string {
	var out []string
	for elem := range strings.SplitSeq(v, ",") {
		for kv := range strings.SplitSeq(elem, ";") {
			key, val, ok := strings.Cut(strings.TrimSpace(kv), "=")
			if !ok || !strings.EqualFold(strings.TrimSpace(key), "for") {
				continue
			}
			out = append(out, strings.Trim(strings.TrimSpace(val), `"`))
		}
	}
	return out
}

func firstHeaderIP(r *http.Request, name string) string {
	for _, line := range r.Header.Values(name) {
		for part := range strings.SplitSeq(line, ",") {
			if addr, ok := parseAddr(part); ok {
				return addr.String()
			}
		}
	}
	return ""
}

func rightmostUntrusted(chain []string, trusted []netip.Prefix) string {
	for _, hop := range slices.Backward(chain) {
		addr, ok := parseAddr(hop)
		if !ok || inPrefixes(addr, trusted) {
			continue
		}
		return addr.String()
	}
	// Every hop trusted/unparsable: leftmost non-private, else the last hop (RemoteAddr).
	if ip := firstNonPrivate(chain); ip != "" {
		return ip
	}
	if len(chain) > 0 {
		return remoteHost(chain[len(chain)-1])
	}
	return ""
}

// firstNonPrivate returns the first (leftmost) parseable non-private address in
// chain, or "" when none exists.
func firstNonPrivate(chain []string) string {
	for _, hop := range chain {
		if addr, ok := parseAddr(hop); ok && !isPrivate(addr) {
			return addr.String()
		}
	}
	return ""
}

func nthFromRight(chain []string, n int, r *http.Request) string {
	valid := make([]netip.Addr, 0, len(chain))
	for _, hop := range chain {
		if addr, ok := parseAddr(hop); ok {
			valid = append(valid, addr)
		}
	}
	idx := len(valid) - 1 - n
	if idx < 0 || idx >= len(valid) {
		return remoteHost(r.RemoteAddr)
	}
	return valid[idx].String()
}

func leftmostNonPrivate(chain []string, r *http.Request) string {
	if ip := firstNonPrivate(chain); ip != "" {
		return ip
	}
	return remoteHost(r.RemoteAddr)
}

// parseAddr normalizes a chain token (which may carry a port or brackets) to a
// netip.Addr, unmapping IPv4-in-IPv6. ok is false when it is not a valid address.
func parseAddr(s string) (netip.Addr, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return netip.Addr{}, false
	}
	if host, _, err := net.SplitHostPort(s); err == nil {
		s = host
	}
	s = strings.Trim(s, "[]")
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

// remoteHost returns the bare IP from a RemoteAddr ("ip:port" or "ip"), or "".
func remoteHost(addr string) string {
	if a, ok := parseAddr(addr); ok {
		return a.String()
	}
	return ""
}
