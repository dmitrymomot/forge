package webhook

import (
	"fmt"
	"net"
	"net/url"
)

// validateDestination guards against Server-Side Request Forgery (SSRF).
//
// Webhook URLs are frequently supplied by end users (tenants configuring their
// own endpoints), which makes them an SSRF vector: a malicious URL pointing at
// an internal address could be used to reach metadata services, databases, or
// other infrastructure that is only reachable from inside the network.
//
// By default the sender resolves the destination host and rejects any address
// that is private, loopback, link-local, unique-local, or unspecified. Callers
// that legitimately deliver to internal endpoints can opt out via
// WithAllowPrivateNetworks.
//
// resolveHost is injectable so tests can exercise the blocking logic without
// real DNS; production passes net.LookupIP.
func validateDestination(webhookURL string, allowPrivate bool, resolveHost func(host string) ([]net.IP, error)) error {
	if allowPrivate {
		return nil
	}

	u, err := url.Parse(webhookURL)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidURL, err)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("%w: host is required", ErrInvalidURL)
	}

	// A literal IP in the URL is checked directly; a hostname is resolved so we
	// catch names that point at internal addresses (including DNS rebinding to
	// fixed internal IPs).
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return fmt.Errorf("%w: destination %s resolves to a non-public address", ErrBlockedDestination, host)
		}
		return nil
	}

	ips, err := resolveHost(host)
	if err != nil {
		return fmt.Errorf("%w: failed to resolve host %s: %w", ErrBlockedDestination, host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("%w: host %s did not resolve to any address", ErrBlockedDestination, host)
	}

	for _, ip := range ips {
		if isBlockedIP(ip) {
			return fmt.Errorf("%w: destination %s resolves to a non-public address (%s)", ErrBlockedDestination, host, ip)
		}
	}

	return nil
}

// isBlockedIP reports whether an IP must be rejected as an SSRF risk.
// Covers loopback, private (RFC 1918 / RFC 4193 unique-local), link-local,
// unspecified, and other non-globally-routable ranges.
func isBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() ||
		ip.IsInterfaceLocalMulticast()
}
