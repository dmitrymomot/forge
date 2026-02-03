package hostrouter

import (
	"net/http"
	"strings"
)

// GetDomain returns the normalized domain from the request Host header.
// Strips port, handles IPv6, and converts to lowercase.
//
// Examples:
//
//	"example.com:8080" -> "example.com"
//	"[::1]:8080" -> "[::1]"
//	"Example.COM" -> "example.com"
func GetDomain(r *http.Request) string {
	return normalizeHost(r.Host)
}

// GetSubdomain extracts the subdomain from a request given a base domain.
// Returns empty string if host doesn't match the base domain or has no subdomain.
//
// Examples:
//
//	GetSubdomain(req, "example.com") // req.Host = "foo.example.com" -> "foo"
//	GetSubdomain(req, "example.com") // req.Host = "bar.foo.example.com" -> "bar.foo"
//	GetSubdomain(req, "example.com") // req.Host = "example.com" -> ""
//	GetSubdomain(req, "example.com") // req.Host = "other.com" -> ""
func GetSubdomain(r *http.Request, baseDomain string) string {
	host := normalizeHost(r.Host)
	base := strings.ToLower(baseDomain)

	// Exact match means no subdomain
	if host == base {
		return ""
	}

	// Check if host ends with ".baseDomain"
	suffix := "." + base
	if !strings.HasSuffix(host, suffix) {
		return ""
	}

	// Extract subdomain (everything before the suffix)
	subdomain := strings.TrimSuffix(host, suffix)
	return subdomain
}
