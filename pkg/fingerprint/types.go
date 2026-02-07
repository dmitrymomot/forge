package fingerprint

import "errors"

// Config configures fingerprint generation behavior.
type Config struct {
	// IncludeIP includes client IP address in fingerprint.
	// WARNING: IP addresses change frequently (mobile networks, VPNs, corporate proxies).
	IncludeIP bool `env:"INCLUDE_IP" envDefault:"false"`

	// IncludeUserAgent includes User-Agent header in fingerprint.
	IncludeUserAgent bool `env:"INCLUDE_USER_AGENT" envDefault:"true"`

	// IncludeAcceptHeaders includes Accept-* headers in fingerprint.
	// These can change with browser extensions or language settings.
	IncludeAcceptHeaders bool `env:"INCLUDE_ACCEPT_HEADERS" envDefault:"true"`

	// IncludeHeaderSet includes fingerprint of which standard headers are present.
	// Different browsers send different sets of headers, making this useful for identification.
	IncludeHeaderSet bool `env:"INCLUDE_HEADER_SET" envDefault:"true"`
}

// DefaultConfig returns the default fingerprint configuration.
// Excludes IP address to avoid false positives from mobile networks, VPNs, and corporate proxies.
func DefaultConfig() Config {
	return Config{
		IncludeIP:            false,
		IncludeUserAgent:     true,
		IncludeAcceptHeaders: true,
		IncludeHeaderSet:     true,
	}
}

// Validation errors that can be checked with errors.Is()
var (
	// ErrInvalidFingerprint indicates the stored fingerprint has invalid format.
	ErrInvalidFingerprint = errors.New("invalid fingerprint format")

	// ErrMismatch indicates the fingerprint doesn't match the current request.
	// This could indicate a session hijacking attempt or legitimate changes to
	// the client's browser/network configuration.
	ErrMismatch = errors.New("fingerprint mismatch")
)
