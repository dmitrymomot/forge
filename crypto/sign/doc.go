// Package sign produces and verifies HMAC tags with constant-time verification, for
// opaque values that must not be tampered with: unsubscribe links, signed download URLs,
// integrity checks. It is lower-level than token.
//
//	s, _ := sign.New(key)
//	tag := s.SignString("user@example.com") // "0.<base64url-mac>"
//	ok := s.VerifyString("user@example.com", tag)
//
// Sign/Verify are the raw single-key primitive; SignString/VerifyString carry the key
// version, so a signer built with FromKeyset verifies tags produced under retired keys
// during rotation. Verification always runs in constant time via consttime.
package sign
