package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/dmitrymomot/forge"
)

// tenantKey is the context key for storing tenant information.
type tenantKey struct{}

// reservedSubdomains are subdomains that cannot be used as tenant identifiers.
var reservedSubdomains = map[string]bool{
	"api": true,
	"app": true,
	"www": true,
}

// TenantExtractor extracts the subdomain from the Host header and stores it in the context.
// For example, "acme.lvh.me:8081" becomes "acme".
// Requests with reserved subdomains (api, app, www) are rejected with 400 Bad Request.
func TenantExtractor(next forge.HandlerFunc) forge.HandlerFunc {
	return func(c forge.Context) error {
		subdomain := extractSubdomain(c.Request().Host)

		if subdomain == "" {
			return c.Error(http.StatusBadRequest, "Invalid tenant subdomain")
		}

		if reservedSubdomains[subdomain] {
			return c.Error(http.StatusBadRequest, "Reserved subdomain")
		}

		// Store subdomain in context
		c.Set(tenantKey{}, subdomain)

		return next(c)
	}
}

// TenantFromContext retrieves the tenant subdomain from the context.
// Returns empty string if no tenant is present.
func TenantFromContext(ctx context.Context) string {
	if tenant, ok := ctx.Value(tenantKey{}).(string); ok {
		return tenant
	}
	return ""
}

// extractSubdomain extracts the first subdomain component from a host.
// "acme.lvh.me:8081" -> "acme"
// "lvh.me:8081" -> ""
// "api.lvh.me" -> "api"
func extractSubdomain(host string) string {
	// Strip port if present
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		// Make sure it's not part of an IPv6 address
		if !strings.Contains(host[idx:], "]") {
			host = host[:idx]
		}
	}

	// Split by dots and check if we have a subdomain
	parts := strings.Split(host, ".")
	if len(parts) < 3 {
		// No subdomain (e.g., "lvh.me")
		return ""
	}

	// Return the first part as the subdomain
	return strings.ToLower(parts[0])
}
