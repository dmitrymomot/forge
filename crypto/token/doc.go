// Package token issues and parses opaque, signed, optionally-encrypted, expiring tokens
// that carry a small typed payload — for email-verify, password-reset, magic-link, and
// invite flows. It is deliberately not JWT: tokens are app-internal and opaque, with no
// algorithm negotiation.
//
// The wire form is base64url(payload-envelope) signed with package sign; WithEncrypt adds
// payload encryption via package secret. Each token carries a random nonce, so identical
// payloads produce distinct tokens. Parse verifies the signature first (constant time),
// then checks expiry against the injected clock, then purpose. Errors are ErrExpired,
// ErrBadSignature, ErrMalformed, and ErrWrongPurpose.
//
// # Usage
//
//	type Reset struct{ UserID string `json:"uid"` }
//	codec, _ := token.New[Reset](key, token.WithTTL(15*time.Minute), token.WithPurpose("pwreset"))
//	tok, _   := codec.Issue(Reset{UserID: "u_123"})
//	got, err := codec.Parse(tok) // got.UserID == "u_123"
package token
