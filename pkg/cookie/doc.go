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
//		m, _ := cookie.New(cookie.Config{})
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
//	m, err := cookie.New(cookie.Config{
//		Secret:   "your-32+-byte-secret-key-here!!",
//		Secure:   true,
//		HTTPOnly: true,
//	})
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
// # Security Model
//
// Signed and encrypted values are bound to their cookie name: the name is mixed
// into the HMAC for signed cookies and used as AES-GCM additional authenticated
// data for encrypted cookies, so a value cannot be moved between cookie names.
//
// Each signed/encrypted value also embeds an expiry derived from the maxAge
// passed to Set: a positive maxAge is enforced on read (returning [ErrExpired]
// once it has passed), bounding the replay window. A zero or negative maxAge
// embeds no expiry, so such values can be replayed indefinitely until the
// browser drops the cookie; pair those with server-side state if replay matters.
//
// # Configuration
//
// Use [Config] to configure cookie attributes:
//   - Secret: Set the secret for signing/encryption (32+ bytes)
//   - Domain: Set the cookie domain
//   - Path: Set the cookie path (default: "/")
//   - SameSite: Set the SameSite attribute as string (default: "lax").
//     "none" forces Secure on, since browsers reject SameSite=None without it.
//   - Secure: Set the Secure flag (HTTPS only)
//   - HTTPOnly: Set the HttpOnly flag. Defaults to true; the Manager always
//     emits HttpOnly cookies regardless of this value, since every cookie it
//     manages is server-side and never needs client-side JS access.
//
// # Errors
//
// The package defines these sentinel errors:
//   - [ErrNotFound]: Cookie does not exist
//   - [ErrNoSecret]: Secret required for signed/encrypted operations
//   - [ErrBadSecret]: Secret must be at least 32 bytes
//   - [ErrBadSig]: Signature verification failed (tampering detected)
//   - [ErrDecrypt]: Decryption failed (tampering or corruption detected)
//   - [ErrExpired]: Embedded expiry has passed
package cookie
