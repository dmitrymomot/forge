// Package cookie provides HTTP cookie management with optional signing and encryption.
//
// The Manager handles plain, signed, and encrypted cookies, plus flash messages.
// Secrets are optional; encrypted and signed operations return [ErrNoSecret] without one.
//
// # Basic Usage
//
// Plain cookies work without a secret:
//
//	import (
//		"net/http"
//
//		"github.com/dmitrymomot/forge/pkg/cookie"
//	)
//
//	func handler(w http.ResponseWriter, r *http.Request) {
//		m := cookie.New()
//		m.Set(w, "theme", "dark", 86400)
//		value, err := m.Get(r, "theme")
//		if err != nil {
//			// handle error
//		}
//	}
//
// # With Secret
//
// Enable signing and encryption with a 32+ byte secret:
//
//	m := cookie.New(
//		cookie.WithSecret("your-32+-byte-secret-key-here!!"),
//		cookie.WithSecure(true),
//		cookie.WithHTTPOnly(true),
//	)
//
// Signed cookies detect tampering with HMAC-SHA256:
//
//	err := m.SetSigned(w, "session", sessionID, 86400)
//	value, err := m.GetSigned(r, "session")
//
// Encrypted cookies use AES-256-GCM:
//
//	err := m.SetEncrypted(w, "prefs", userPrefs, 86400)
//	value, err := m.GetEncrypted(r, "prefs")
//
// # Flash Messages
//
// Flash messages are encrypted, single-read values that auto-delete after reading.
// They are useful for displaying success/error messages after redirects:
//
//	// Set a flash message
//	m.SetFlash(w, "msg", map[string]string{"type": "success", "text": "Saved!"})
//
//	// Read and delete in the same request
//	var msg map[string]string
//	err := m.Flash(w, r, "msg", &msg)
//	// Flash is now deleted (no further reads will return it)
//
// # Configuration
//
// Use options to configure cookie attributes:
//   - [WithSecret]: Set the secret for signing/encryption (32+ bytes)
//   - [WithDomain]: Set the cookie domain
//   - [WithPath]: Set the cookie path (default: "/")
//   - [WithSecure]: Set the Secure flag (HTTPS only)
//   - [WithHTTPOnly]: Set the HttpOnly flag (default: true)
//   - [WithSameSite]: Set the SameSite attribute (default: Lax)
//
// # Errors
//
// The package defines these sentinel errors:
//   - [ErrNotFound]: Cookie does not exist
//   - [ErrNoSecret]: Secret required for signed/encrypted operations
//   - [ErrBadSecret]: Secret must be at least 32 bytes (note: automatically ignored if provided)
//   - [ErrBadSig]: Signature verification failed (tampering detected)
//   - [ErrDecrypt]: Decryption failed (tampering or corruption detected)
package cookie
