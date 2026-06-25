package hostrouter

import "strings"

// normalizeHost lowercases the host, strips any port, removes IPv6 brackets, and
// trims a trailing FQDN dot. It allocates only on strings.ToLower's slow path
// (uppercase input); an already-lowercase host returns sub-slices with no copy.
//
// net.SplitHostPort is deliberately avoided: it allocates an *AddrError whenever
// the host has no port (the common proxied/HTTP2 case).
func normalizeHost(host string) string {
	if host == "" {
		return ""
	}
	if host[0] == '[' { // IPv6 literal: "[::1]" or "[::1]:8080"
		if i := strings.IndexByte(host, ']'); i >= 0 {
			host = host[1:i] // inside brackets; drops "]" and any ":port" after it
		}
	} else if i := strings.LastIndexByte(host, ':'); i >= 0 &&
		strings.IndexByte(host, ':') == i {
		host = host[:i] // exactly one colon => host:port (not bracketless IPv6)
	}
	host = strings.TrimSuffix(host, ".") // rooted FQDN "example.com."
	return strings.ToLower(host)
}

// splitFirstLabel splits "foo.example.com" into ("foo", "example.com", true).
// ok is false when there is no dot, a leading dot, or a trailing dot.
func splitFirstLabel(host string) (label, parent string, ok bool) {
	i := strings.IndexByte(host, '.')
	if i <= 0 || i == len(host)-1 {
		return "", "", false
	}
	return host[:i], host[i+1:], true
}
