// Package fingerprint provides device fingerprinting from HTTP requests for session validation and security.
//
// This package generates unique, version-prefixed fingerprints by combining HTTP request
// characteristics such as User-Agent, Accept headers, client IP (optional), and header
// presence patterns. It serves as an additional security layer for detecting potential
// session hijacking by identifying changes in client characteristics between requests.
//
// # Basic Usage
//
// Generate a fingerprint during login and store it with the session, then validate
// it on subsequent requests:
//
//	import (
//		"errors"
//		"net/http"
//
//		"github.com/dmitrymomot/forge/pkg/fingerprint"
//	)
//
//	func loginHandler(w http.ResponseWriter, r *http.Request) {
//		// Generate fingerprint for new session (uses safe defaults)
//		fp := fingerprint.Generate(r)
//		// Returns: "v1:a1b2c3d4e5f6..." (35 characters)
//
//		// Store with session data
//		session.Set("fingerprint", fp)
//	}
//
//	func protectedHandler(w http.ResponseWriter, r *http.Request) {
//		// Retrieve stored fingerprint
//		storedFP := session.Get("fingerprint")
//
//		// Validate current request against stored fingerprint
//		// IMPORTANT: Use the same options that were used during generation
//		if err := fingerprint.Validate(r, storedFP); err != nil {
//			if errors.Is(err, fingerprint.ErrMismatch) {
//				// Fingerprint mismatch - potential session hijacking
//				session.Destroy()
//				http.Error(w, "Session invalid", http.StatusUnauthorized)
//				return
//			}
//			// Invalid fingerprint format
//			http.Error(w, "Invalid session", http.StatusBadRequest)
//			return
//		}
//
//		// Continue processing authenticated request
//	}
//
// # Convenience Functions
//
// The package provides pre-configured functions for common scenarios.
// Each generation function has a matching validation function:
//
//	// Cookie-based sessions (recommended default)
//	// Excludes IP to avoid false positives from mobile networks and VPNs
//	fp := fingerprint.Cookie(r)
//	err := fingerprint.ValidateCookie(r, storedFP)
//
//	// JWT authentication (minimal fingerprint)
//	// Excludes Accept headers that may vary with content negotiation
//	fp := fingerprint.JWT(r)
//	err := fingerprint.ValidateJWT(r, storedFP)
//
//	// High-security mode (includes IP address)
//	// WARNING: May cause false positives for mobile users and VPN users
//	fp := fingerprint.Strict(r)
//	err := fingerprint.ValidateStrict(r, storedFP)
//
// # Custom Configuration
//
// Use functional options to customize fingerprint generation. When validating,
// use the SAME options that were used during generation:
//
//	// Include IP address (high-security mode)
//	fp := fingerprint.Generate(r, fingerprint.WithIP())
//	err := fingerprint.Validate(r, storedFP, fingerprint.WithIP())
//
//	// Exclude specific components
//	fp := fingerprint.Generate(r, fingerprint.WithoutAcceptHeaders())
//	err := fingerprint.Validate(r, storedFP, fingerprint.WithoutAcceptHeaders())
//
//	// Combine multiple options
//	fp := fingerprint.Generate(r,
//		fingerprint.WithIP(),
//		fingerprint.WithoutAcceptHeaders(),
//	)
//	err := fingerprint.Validate(r, storedFP,
//		fingerprint.WithIP(),
//		fingerprint.WithoutAcceptHeaders(),
//	)
//
//	// Explicitly exclude IP (default behavior, for clarity)
//	fp := fingerprint.Generate(r, fingerprint.WithoutIP())
//	err := fingerprint.Validate(r, storedFP, fingerprint.WithoutIP())
//
// # Error Handling
//
// The Validate function returns errors that can be checked with errors.Is():
//
//	err := fingerprint.Validate(r, storedFP)
//	switch {
//	case err == nil:
//		// Fingerprint matches - request is valid
//
//	case errors.Is(err, fingerprint.ErrInvalidFingerprint):
//		// Stored fingerprint has invalid format (not "v1:hash")
//		// This should not happen in normal operation
//
//	case errors.Is(err, fingerprint.ErrMismatch):
//		// Fingerprint doesn't match current request
//		// Could indicate session hijacking or legitimate client changes
//		// (browser update, network change, privacy settings, etc.)
//		// Consider logging for security monitoring and re-authenticating user
//	}
//
// The simplified validation returns ErrMismatch without identifying which specific
// component changed. If you need to handle different changes differently, use
// separate fingerprints with different options:
//
//	// Generate two fingerprints: one with IP, one without
//	fpWithIP := fingerprint.Generate(r, fingerprint.WithIP())
//	fpWithoutIP := fingerprint.Generate(r)
//
//	// Store both
//	session.Set("fp_strict", fpWithIP)
//	session.Set("fp_relaxed", fpWithoutIP)
//
//	// Later, check both to identify if only IP changed
//	if err := fingerprint.Validate(r, storedStrictFP, fingerprint.WithIP()); err != nil {
//		// Strict check failed, try relaxed
//		if err := fingerprint.Validate(r, storedRelaxedFP); err == nil {
//			// Only IP changed - handle gracefully
//		} else {
//			// Something else changed - potential hijacking
//		}
//	}
//
// # Fingerprint Components
//
// Default configuration includes:
//   - User-Agent header (browser and OS identification)
//   - Accept-Language header (language preferences)
//   - Accept-Encoding header (compression support)
//   - Accept header (content type preferences)
//   - Header set fingerprint (which standard headers are present)
//
// Optional components:
//   - Client IP address (via github.com/dmitrymomot/forge/pkg/clientip)
//
// Components can be excluded using functional options (WithoutUserAgent,
// WithoutAcceptHeaders, WithoutHeaderSet). IP is excluded by default but
// can be included with WithIP().
//
// # Fingerprint Format
//
// Fingerprints are returned in version-prefixed format: "v1:hash"
//   - "v1" indicates the fingerprint algorithm version
//   - "hash" is a 32-character hex-encoded SHA256 hash (first 16 bytes)
//   - Total length is always 35 characters
//
// The version prefix enables future algorithm changes without breaking existing
// sessions. Invalid fingerprints can be detected by format validation.
//
// # Security Considerations
//
// Device fingerprinting has inherent limitations and should supplement, not replace,
// proper session management:
//
// Known limitations:
//   - IP addresses change frequently (mobile networks, VPNs, corporate proxies)
//   - Browser updates modify User-Agent strings
//   - Language settings and browser extensions can change Accept headers
//   - Users can modify or block headers (privacy extensions, developer tools)
//   - Header order is not fingerprinted (varies by HTTP library)
//
// Recommendations:
//   - Use default configuration (excludes IP) for most applications
//   - Use Strict() only for high-security scenarios with graceful error handling
//   - Combine with other security measures (CSRF tokens, rate limiting)
//   - Log fingerprint mismatches for security monitoring
//   - Don't terminate sessions immediately on first mismatch
//   - Consider re-authentication for sensitive operations
//
// The default configuration balances security and usability by avoiding false
// positives from legitimate IP changes while still detecting most session
// hijacking attempts.
package fingerprint
