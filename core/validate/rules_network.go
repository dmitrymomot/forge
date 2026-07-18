package validate

import (
	"net"
	"regexp"
	"strings"
)

// reDomain matches labels of letters/digits with internal hyphens, 2+ labels, alphabetic TLD.
var reDomain = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,63}$`)

// IP accepts any valid IPv4 or IPv6 address.
func IP(s string) Violation {
	if net.ParseIP(s) == nil {
		return Violation{Key: "validation.ip"}
	}
	return Violation{}
}

// IPv4 accepts a dotted-quad IPv4 address only.
func IPv4(s string) Violation {
	ip := net.ParseIP(s)
	if ip == nil || ip.To4() == nil || !strings.Contains(s, ".") {
		return Violation{Key: "validation.ipv4"}
	}
	return Violation{}
}

// IPv6 accepts an IPv6 address only.
func IPv6(s string) Violation {
	ip := net.ParseIP(s)
	if ip == nil || strings.Contains(s, ".") || ip.To4() != nil {
		return Violation{Key: "validation.ipv6"}
	}
	return Violation{}
}

// MAC accepts a hardware address (net.ParseMAC).
func MAC(s string) Violation {
	if _, err := net.ParseMAC(s); err != nil {
		return Violation{Key: "validation.mac"}
	}
	return Violation{}
}

// Domain accepts a hostname of at most 253 characters with at least two labels and an alphabetic TLD.
func Domain(s string) Violation {
	if len(s) > 253 || !reDomain.MatchString(s) {
		return Violation{Key: "validation.domain"}
	}
	return Violation{}
}
