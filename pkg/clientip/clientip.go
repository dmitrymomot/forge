package clientip

import (
	"net"
	"net/http"
	"strings"
)

// GetIP extracts the client IP address from an HTTP request.
//
// It checks proxy headers in priority order (CF-Connecting-IP, DO-Connecting-IP,
// X-Forwarded-For, X-Real-IP) before falling back to r.RemoteAddr. The returned
// value is a validated, normalized IP string; the function never panics and
// always returns a string.
//
// TRUST BOUNDARY: GetIP unconditionally trusts the proxy headers above. Any
// client that can reach the application directly can set these headers to
// arbitrary values, so the returned IP is SPOOFABLE unless every request is
// guaranteed to pass through a trusted reverse proxy / load balancer / CDN that
// overwrites them. Only use GetIP when the application is deployed strictly
// behind such trusted infrastructure. If clients can connect directly (or reach
// the app without traversing the trusted proxy), do NOT use the header-derived
// IP for any security decision (authentication, authorization, rate limiting,
// audit logging, IP allow/deny lists); fall back to r.RemoteAddr instead.
func GetIP(r *http.Request) string {
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
		if parsed := parseIP(ip); parsed != "" {
			return parsed
		}
	}

	if ip := r.Header.Get("DO-Connecting-IP"); ip != "" {
		if parsed := parseIP(ip); parsed != "" {
			return parsed
		}
	}

	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		// X-Forwarded-For contains comma-separated IPs; use the leftmost (client origin)
		for ip := range strings.SplitSeq(forwarded, ",") {
			if parsed := parseIP(strings.TrimSpace(ip)); parsed != "" {
				return parsed
			}
		}
	}

	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		if parsed := parseIP(ip); parsed != "" {
			return parsed
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// RemoteAddr might be just IP without port in some environments
		if parsed := parseIP(r.RemoteAddr); parsed != "" {
			return parsed
		}
		return r.RemoteAddr
	}
	if parsed := parseIP(host); parsed != "" {
		return parsed
	}
	return host
}

// parseIP validates and normalizes an IP address string.
func parseIP(ipStr string) string {
	ipStr = strings.TrimSpace(ipStr)
	if ipStr == "" {
		return ""
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return ""
	}

	// Reject 0.0.0.0 which indicates no valid client IP was provided
	if ip.Equal(net.IPv4zero) {
		return ""
	}

	return ip.String()
}
