package fingerprint

import "errors"

// options configures fingerprint generation behavior.
type options struct {
	// includeIP includes client IP address in fingerprint.
	// WARNING: IP addresses change frequently (mobile networks, VPNs, corporate proxies).
	// Default: false
	includeIP bool

	// includeUserAgent includes User-Agent header in fingerprint.
	// Default: true
	includeUserAgent bool

	// includeAcceptHeaders includes Accept-* headers in fingerprint.
	// These can change with browser extensions or language settings.
	// Default: true
	includeAcceptHeaders bool

	// includeHeaderSet includes fingerprint of which standard headers are present.
	// Different browsers send different sets of headers, making this useful for identification.
	// Default: true
	includeHeaderSet bool
}

// Option is a functional option for configuring fingerprint generation.
type Option func(*options)

// WithIP includes the client IP address in the fingerprint.
// WARNING: This will cause false positives for mobile users, VPN users, and users behind dynamic proxies.
// Only use this for high-security scenarios where you can handle re-authentication gracefully.
func WithIP() Option {
	return func(o *options) {
		o.includeIP = true
	}
}

// WithoutIP explicitly excludes the client IP address from the fingerprint.
// This is the default behavior, so this option is only needed for clarity.
func WithoutIP() Option {
	return func(o *options) {
		o.includeIP = false
	}
}

// WithoutUserAgent excludes the User-Agent header from the fingerprint.
func WithoutUserAgent() Option {
	return func(o *options) {
		o.includeUserAgent = false
	}
}

// WithoutAcceptHeaders excludes Accept-* headers from the fingerprint.
// Useful when you expect content negotiation to vary.
func WithoutAcceptHeaders() Option {
	return func(o *options) {
		o.includeAcceptHeaders = false
	}
}

// WithoutHeaderSet excludes the header set fingerprint.
func WithoutHeaderSet() Option {
	return func(o *options) {
		o.includeHeaderSet = false
	}
}

// defaultOptions returns the default fingerprint configuration.
// Excludes IP address to avoid false positives from mobile networks, VPNs, and corporate proxies.
func defaultOptions() *options {
	return &options{
		includeIP:            false,
		includeUserAgent:     true,
		includeAcceptHeaders: true,
		includeHeaderSet:     true,
	}
}

func applyOptions(opts ...Option) *options {
	o := defaultOptions()
	for _, opt := range opts {
		opt(o)
	}
	return o
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
