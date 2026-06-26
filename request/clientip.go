package request

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type clientIPConfig struct {
	headers    []string
	trusted    []netip.Prefix
	useTrusted bool
}

// ClientIPOption configures ClientIP.
type ClientIPOption func(*clientIPConfig)

// WithClientIPHeaders replaces the header priority list scanned in best-effort
// mode. The secure pattern for a known CDN is to pin the single header the edge
// always sets and strips on ingress, e.g. WithClientIPHeaders("CF-Connecting-IP").
func WithClientIPHeaders(names ...string) ClientIPOption {
	return func(c *clientIPConfig) { c.headers = names }
}

// WithTrustedProxies switches to spoof-resistant X-Forwarded-For resolution: the
// chain (XFF entries plus RemoteAddr) is walked right-to-left, trusted hops are
// skipped, and the first untrusted address is returned.
func WithTrustedProxies(prefixes ...netip.Prefix) ClientIPOption {
	return func(c *clientIPConfig) { c.trusted = prefixes; c.useTrusted = true }
}

// ClientIP returns the best guess at the originating client IP, or "" if nothing
// parses. By default it scans well-known CDN/proxy headers in priority order and
// falls back to RemoteAddr. The default mode trusts client-supplied headers and is
// spoofable; use WithTrustedProxies or a pinned header for auth/rate-limiting.
func ClientIP(r *http.Request, opts ...ClientIPOption) string {
	// Best-effort scan order. Trusting these is spoofable unless the service sits
	// behind a proxy that overwrites them.
	c := clientIPConfig{headers: []string{
		"CF-Connecting-IP",
		"True-Client-IP",
		"Fastly-Client-IP",
		"X-Real-IP",
		"Forwarded",
		"X-Forwarded-For",
	}}
	for _, o := range opts {
		o(&c)
	}

	if c.useTrusted {
		return clientIPTrusted(r, c.trusted)
	}
	for _, name := range c.headers {
		if ip := headerIP(r, name); ip != "" {
			return ip
		}
	}
	return remoteHost(r.RemoteAddr)
}

// headerIP extracts the first valid IP from header name. Forwarded uses RFC 7239
// for= parsing; every other header is treated as a comma list, first valid wins.
func headerIP(r *http.Request, name string) string {
	raw := r.Header.Get(name)
	if raw == "" {
		return ""
	}
	if strings.EqualFold(name, "Forwarded") {
		return forwardedFor(raw)
	}
	for part := range strings.SplitSeq(raw, ",") {
		if ip := validIP(part); ip != "" {
			return ip
		}
	}
	return ""
}

// clientIPTrusted walks the XFF chain (then RemoteAddr) right-to-left and returns
// the first address not inside a trusted prefix. If all hops are trusted, the
// left-most chain entry (closest to the originating client) is returned.
func clientIPTrusted(r *http.Request, trusted []netip.Prefix) string {
	var chain []string
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		chain = strings.Split(xff, ",")
	}
	chain = append(chain, r.RemoteAddr)

	for i := len(chain) - 1; i >= 0; i-- {
		ip := validIP(chain[i])
		if ip == "" {
			continue
		}
		addr, err := netip.ParseAddr(ip)
		if err != nil {
			continue
		}
		if isTrusted(addr, trusted) {
			continue
		}
		return ip
	}
	if len(chain) > 0 {
		if ip := validIP(chain[0]); ip != "" {
			return ip
		}
	}
	return remoteHost(r.RemoteAddr)
}

func isTrusted(addr netip.Addr, trusted []netip.Prefix) bool {
	for _, p := range trusted {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// forwardedFor returns the IP from the first "for=" directive of a Forwarded header.
func forwardedFor(v string) string {
	first, _, _ := strings.Cut(v, ",")
	for kv := range strings.SplitSeq(first, ";") {
		key, val, ok := strings.Cut(strings.TrimSpace(kv), "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "for") {
			continue
		}
		return validIP(strings.Trim(strings.TrimSpace(val), `"`))
	}
	return ""
}

// validIP normalizes s (which may carry a port or brackets) to a bare IP string,
// or "" if it is not a valid address.
func validIP(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(s); err == nil {
		s = host
	}
	s = strings.Trim(s, "[]")
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return ""
	}
	return addr.String()
}

// remoteHost returns the host portion of a RemoteAddr ("ip:port" or bare ip).
func remoteHost(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		if a, err := netip.ParseAddr(host); err == nil {
			return a.String()
		}
		return host
	}
	if a, err := netip.ParseAddr(addr); err == nil {
		return a.String()
	}
	return addr
}
