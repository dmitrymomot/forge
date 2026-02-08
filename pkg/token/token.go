package token

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
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

	// Verify signature before processing payload (security-first)
	expected := sign(encoded, secret)
	if subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) != 1 {
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
