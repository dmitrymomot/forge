// Package token provides compact URL-safe signed tokens with truncated HMAC-SHA256.
//
// Tokens encode any JSON-serializable payload into a short, URL-safe string suitable for
// email verification links, invite tokens, magic links, and password reset URLs. Unlike JWTs,
// tokens have no header or registered claims — just a signed payload.
//
// # Token Format
//
// A token consists of two base64url-encoded segments separated by a dot:
//
//	base64url(json(payload)).base64url(hmac-sha256[:8])
//
// The signature is a truncated HMAC-SHA256 (first 8 bytes / 64 bits), providing adequate
// brute-force resistance for short-lived, single-use tokens. The entire token is URL-safe
// with no padding characters.
//
// # Features
//
//   - Generic functions for any JSON-serializable payload type
//   - Truncated HMAC-SHA256 signature (compact, 8 bytes)
//   - Constant-time signature verification
//   - URL-safe output (no padding, no special characters)
//   - Zero external dependencies (stdlib only)
//
// # Usage
//
// Generating a token:
//
//	type EmailVerification struct {
//	    UserID string `json:"user_id"`
//	    Email  string `json:"email"`
//	    Action string `json:"action"`
//	}
//
//	payload := EmailVerification{
//	    UserID: "usr_abc123",
//	    Email:  "user@example.com",
//	    Action: "verify_email",
//	}
//
//	tok, err := token.GenerateToken(payload, []byte("your-secret-key"))
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	verifyURL := fmt.Sprintf("https://example.com/verify?token=%s", tok)
//
// Parsing and verifying a token:
//
//	result, err := token.ParseToken[EmailVerification](tok, []byte("your-secret-key"))
//	if err != nil {
//	    switch {
//	    case errors.Is(err, token.ErrSignatureInvalid):
//	        log.Println("Token has been tampered with")
//	    case errors.Is(err, token.ErrInvalidToken):
//	        log.Println("Token is malformed")
//	    default:
//	        log.Printf("Token parsing failed: %v", err)
//	    }
//	    return
//	}
//
//	log.Printf("Verified email: %s for user: %s", result.Email, result.UserID)
//
// # Use Cases
//
//   - Email verification: encode user ID + email + action, embed in verification link
//   - Invite tokens: encode workspace ID + inviter + role, send in invitation email
//   - Magic links: encode user ID + timestamp, send passwordless login link
//   - Password reset: encode user ID + nonce, embed in reset URL
//
// # When to Use Token vs JWT
//
// Use this package for short-lived, single-use tokens embedded in URLs or emails where
// compact size matters and there are no temporal claims (expiration, not-before). Use the
// jwt package for API authentication tokens that require standard claims, longer signatures,
// and header metadata.
//
// # Security Considerations
//
// Secret key requirements:
//   - Use at least 32 bytes (256 bits) for HMAC-SHA256
//   - Generate using a cryptographically secure random source
//   - Store securely (environment variables, key management service)
//
// Truncated signature trade-offs:
//   - 8-byte (64-bit) signature provides ~2^64 brute-force resistance
//   - Adequate for short-lived, single-use tokens with server-side state
//   - Not suitable for long-lived bearer tokens — use JWT for those
//
// Token lifecycle:
//   - Always pair with server-side state (database flag, one-time nonce)
//   - Mark tokens as consumed after use to prevent replay
//   - Enforce expiration at the application level if needed
//
// # Error Handling
//
// The package provides specific error types for different failure modes:
//   - ErrInvalidToken: malformed token structure or invalid base64url encoding
//   - ErrSignatureInvalid: HMAC signature does not match (tampered or wrong secret)
//   - ErrEmptySecret: nil or empty secret provided
package token
