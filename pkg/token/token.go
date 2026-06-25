package token

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	// signatureSize is the number of bytes taken from the HMAC-SHA256 digest.
	// 8 bytes (64 bits) provides adequate brute-force resistance for short-lived
	// tokens like email verification links and password resets.
	signatureSize = 8

	// tokenSeparator delimits the encoded payload from the signature.
	tokenSeparator = "."
)

// GenerateToken encodes payload as JSON, signs it with a truncated HMAC-SHA256,
// and returns a compact URL-safe token string.
//
// The token format is: base64url(json(payload)).base64url(hmac-sha256[:8])
//
// The payload must be JSON-serializable. The secret must not be empty.
func GenerateToken[T any](payload T, secret []byte) (string, error) {
	if len(secret) == 0 {
		return "", ErrEmptySecret
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("token: failed to marshal payload: %w", err)
	}

	encoded := base64.RawURLEncoding.EncodeToString(payloadJSON)
	sig := sign(encoded, secret)

	return encoded + tokenSeparator + sig, nil
}

// ParseToken verifies the signature and decodes the payload from a token string.
//
// Returns a pointer to the decoded payload on success, or an error if the token
// is malformed, the signature is invalid, or the payload cannot be decoded.
func ParseToken[T any](tokenStr string, secret []byte) (*T, error) {
	if len(secret) == 0 {
		return nil, ErrEmptySecret
	}

	encoded, sig, ok := strings.Cut(tokenStr, tokenSeparator)
	if !ok || encoded == "" || sig == "" {
		return nil, ErrInvalidToken
	}

	// Verify signature before processing payload (security-first).
	//
	// subtle.ConstantTimeCompare short-circuits (non-constant-time) when the two
	// inputs differ in length, which would leak the attacker-supplied signature
	// length. To keep the comparison uniformly constant-time regardless of the
	// received signature's length, HMAC both values to fixed-size digests under a
	// per-call random key (double-HMAC) before comparing. This avoids leaking any
	// length information about the attacker-controlled sig segment.
	expected := sign(encoded, secret)
	if !constantTimeEqual(sig, expected) {
		return nil, ErrSignatureInvalid
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, ErrInvalidToken
	}

	var result T
	if err := json.Unmarshal(payloadJSON, &result); err != nil {
		return nil, fmt.Errorf("token: failed to unmarshal payload: %w", err)
	}

	return &result, nil
}

// sign computes a truncated HMAC-SHA256 signature and returns it as base64url.
func sign(message string, secret []byte) string {
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(message))
	mac := h.Sum(nil)[:signatureSize]
	return base64.RawURLEncoding.EncodeToString(mac)
}

// constantTimeEqual reports whether a and b are equal without leaking the
// length of a (the attacker-controlled signature segment) through timing.
//
// subtle.ConstantTimeCompare returns immediately when its inputs differ in
// length, so feeding it the raw segments would make the comparison's running
// time depend on the supplied signature length. To avoid that, both values are
// reduced to fixed-size HMAC-SHA256 digests under a fresh per-call random key
// (the double-HMAC construction) before comparison. The digests are always the
// same length, so hmac.Equal runs in constant time regardless of len(a). The
// random key makes the digests unpredictable to an attacker, so they cannot be
// precomputed or used as an oracle.
func constantTimeEqual(a, b string) bool {
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		// crypto/rand failure is catastrophic and effectively never happens;
		// fail closed rather than fall back to a non-constant-time compare.
		return false
	}

	macA := hmac.New(sha256.New, key[:])
	macA.Write([]byte(a))

	macB := hmac.New(sha256.New, key[:])
	macB.Write([]byte(b))

	return hmac.Equal(macA.Sum(nil), macB.Sum(nil))
}
