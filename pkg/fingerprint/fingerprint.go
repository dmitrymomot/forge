package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sort"
	"strings"

	"github.com/dmitrymomot/forge/pkg/clientip"
)

const (
	fingerprintVersion = "v1:"
	// fingerprintHashLen uses 16 bytes (128 bits) for balance between uniqueness
	// and storage efficiency. SHA-256 provides 256 bits, but 128 bits is sufficient
	// for fingerprinting and reduces storage by 50%.
	fingerprintHashLen = 16
	// fingerprintTotalLen is the total length of a fingerprint string:
	// 3 bytes ("v1:") + 32 bytes (hex encoding of 16 bytes) = 35 bytes
	fingerprintTotalLen = 35
)

// Generate creates a device fingerprint from the HTTP request.
// Returns a version-prefixed fingerprint string in format: "v1:hash"
//
// By default, excludes IP address to avoid false positives from mobile networks and VPNs.
// Use functional options to customize behavior:
//
//	fp := fingerprint.Generate(r)  // uses defaults
//	fp := fingerprint.Generate(r, WithIP())  // include IP
//	fp := fingerprint.Generate(r, WithIP(), WithoutAcceptHeaders())  // multiple options
func Generate(r *http.Request, opts ...Option) string {
	o := applyOptions(opts...)

	var components []string

	if o.includeUserAgent {
		components = append(components, r.UserAgent())
	}

	if o.includeAcceptHeaders {
		components = append(components,
			r.Header.Get("Accept-Language"),
			r.Header.Get("Accept-Encoding"),
			r.Header.Get("Accept"),
		)
	}

	if o.includeIP {
		components = append(components, clientip.GetIP(r))
	}

	if o.includeHeaderSet {
		components = append(components, getHeaders(r))
	}

	// Filter out empty components to ensure consistent hashing.
	// Empty values could come from missing headers or disabled options.
	filtered := make([]string, 0, len(components))
	for _, comp := range components {
		if comp != "" {
			filtered = append(filtered, comp)
		}
	}

	// Join with pipe delimiter to prevent collision attacks where
	// ["ab", "c"] and ["a", "bc"] would otherwise produce the same hash.
	combined := strings.Join(filtered, "|")
	hash := sha256.Sum256([]byte(combined))

	return fingerprintVersion + hex.EncodeToString(hash[:fingerprintHashLen])
}

// Validate compares the current request fingerprint with a stored fingerprint.
// Returns nil if fingerprints match, or ErrMismatch if they don't.
//
// The stored fingerprint should be in format "v1:hash". Invalid formats return ErrInvalidFingerprint.
//
// IMPORTANT: Use the same options that were used to generate the stored fingerprint.
// For example, if the stored fingerprint was generated with WithIP(), validate with WithIP():
//
//	stored := Generate(r, WithIP())
//	// ... store the fingerprint ...
//	// Later, validate with the same options:
//	if err := Validate(r, stored, WithIP()); err != nil {
//	    // Fingerprint mismatch - potential session hijacking
//	}
//
// For convenience, use the helper functions that match their corresponding generators:
//   - ValidateCookie() matches Cookie()
//   - ValidateJWT() matches JWT()
//   - ValidateStrict() matches Strict()
func Validate(r *http.Request, sessionFingerprint string, opts ...Option) error {
	if !strings.HasPrefix(sessionFingerprint, fingerprintVersion) || len(sessionFingerprint) != fingerprintTotalLen {
		return ErrInvalidFingerprint
	}

	currentFingerprint := Generate(r, opts...)
	if currentFingerprint == sessionFingerprint {
		return nil
	}

	return ErrMismatch
}

// getHeaders creates a fingerprint based on which standard HTTP headers are present.
//
// This function fingerprints the *presence* of common browser headers, not their values.
// Different browsers and HTTP clients send different sets of headers, making this
// a useful signal for device identification:
//   - Chrome sends Sec-Fetch-* headers
//   - Firefox has different Accept defaults
//   - API clients typically send minimal headers
//   - Mobile browsers may omit certain headers
//
// Only stable, commonly-present headers are included. Frequently-changing headers
// (cookies, cache directives, etc.) are excluded to reduce false positives.
func getHeaders(r *http.Request) string {
	var headerNames []string
	for name := range r.Header {
		// Whitelist stable headers that identify browser/client type
		switch strings.ToLower(name) {
		case "user-agent", "accept", "accept-language", "accept-encoding",
			"connection", "upgrade-insecure-requests", "sec-fetch-dest",
			"sec-fetch-mode", "sec-fetch-site", "cache-control":
			headerNames = append(headerNames, strings.ToLower(name))
		}
	}

	sort.Strings(headerNames)
	return strings.Join(headerNames, ",")
}

// Strict generates a fingerprint with all components including IP address.
// Use for high-security scenarios where IP changes should invalidate sessions.
// WARNING: Will cause false positives for mobile users, VPN users, and users
// behind dynamic proxies.
func Strict(r *http.Request) string {
	return Generate(r, WithIP())
}

// Cookie generates a fingerprint suitable for cookie-based sessions.
// Excludes IP address to avoid false positives from mobile networks and VPNs.
// This is the recommended default for most web applications.
func Cookie(r *http.Request) string {
	return Generate(r) // Uses defaults
}

// JWT generates a minimal fingerprint suitable for JWT-based authentication.
// Includes only User-Agent and header set, excluding Accept headers which
// may vary with content negotiation.
func JWT(r *http.Request) string {
	return Generate(r, WithoutAcceptHeaders())
}

// HTMX generates a fingerprint suitable for HTMX-based applications.
// Uses only User-Agent to avoid false positives from HTMX-specific headers
// (HX-Request, HX-Current-URL, etc.) and varying Accept headers.
// This is the recommended fingerprint for applications using HTMX.
func HTMX(r *http.Request) string {
	return Generate(r, WithoutAcceptHeaders(), WithoutHeaderSet())
}

// ValidateStrict validates a fingerprint generated with Strict().
// Use for high-security scenarios where IP changes should invalidate sessions.
// WARNING: Will cause false positives for mobile users, VPN users, and users
// behind dynamic proxies.
func ValidateStrict(r *http.Request, sessionFingerprint string) error {
	return Validate(r, sessionFingerprint, WithIP())
}

// ValidateCookie validates a fingerprint generated with Cookie().
// Excludes IP address to avoid false positives from mobile networks and VPNs.
// This is the recommended default for most web applications.
func ValidateCookie(r *http.Request, sessionFingerprint string) error {
	return Validate(r, sessionFingerprint) // Uses defaults
}

// ValidateJWT validates a fingerprint generated with JWT().
// Includes only User-Agent and header set, excluding Accept headers which
// may vary with content negotiation.
func ValidateJWT(r *http.Request, sessionFingerprint string) error {
	return Validate(r, sessionFingerprint, WithoutAcceptHeaders())
}

// ValidateHTMX validates a fingerprint generated with HTMX().
// Uses only User-Agent to avoid false positives from HTMX-specific headers
// and varying Accept headers. This is the recommended validator for HTMX applications.
func ValidateHTMX(r *http.Request, sessionFingerprint string) error {
	return Validate(r, sessionFingerprint, WithoutAcceptHeaders(), WithoutHeaderSet())
}
