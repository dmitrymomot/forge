package jwt

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// JWT header constants required by RFC 7519
const (
	HeaderType      = "JWT"
	HeaderAlgorithm = "HS256" // HMAC-SHA256 chosen for security/performance balance
)

// Header represents the JWT header as defined in RFC 7515
type Header struct {
	Type      string `json:"typ"`
	Algorithm string `json:"alg"`
}

// minSigningKeyBytes is the minimum signing-key length enforced by New.
// HMAC-SHA256 needs a 256-bit (32-byte) key for adequate security.
const minSigningKeyBytes = 32

// StandardClaims represents the registered JWT claims defined in RFC 7519 Section 4.1.
// All fields use Unix timestamps for temporal claims to ensure consistent validation.
//
// Note on Audience: RFC 7519 permits the "aud" claim to be either a single string or
// an array of strings. This package models it as a single string only. Tokens emitted
// by this package are RFC-valid, but parsing a third-party token whose "aud" is a JSON
// array will fail to unmarshal. If multi-audience interop is required, use a custom
// claims type with an "aud" field of your own type (e.g. []string) rather than relying
// on the embedded StandardClaims.Audience.
type StandardClaims struct {
	ID        string `json:"jti,omitempty"` // JWT ID - unique identifier for preventing token reuse
	Subject   string `json:"sub,omitempty"` // Subject - typically user ID or entity identifier
	Issuer    string `json:"iss,omitempty"` // Issuer - identifies who issued the token
	Audience  string `json:"aud,omitempty"` // Audience - intended recipient (single string only; see type doc)
	ExpiresAt int64  `json:"exp,omitempty"` // Expiration time - Unix timestamp when token expires
	NotBefore int64  `json:"nbf,omitempty"` // Not before - Unix timestamp when token becomes valid
	IssuedAt  int64  `json:"iat,omitempty"` // Issued at - Unix timestamp when token was created
}

// Valid validates the temporal claims against the current time with no leeway.
// Zero values are treated as unset (per RFC 7519) and are ignored during validation.
//
// Parse always enforces exp/nbf on the embedded StandardClaims regardless of whether
// the claims type implements this method, so a custom claims type does not need to
// implement Valid to get temporal validation. Implement Valid only to add extra
// application-specific checks (e.g. issuer or audience validation).
func (c StandardClaims) Valid() error {
	return c.validate(0)
}

// validate checks exp/nbf against the current time, allowing the given clock-skew
// leeway. A token is expired only once now is past exp+leeway, and not-yet-valid
// only while now is before nbf-leeway.
func (c StandardClaims) validate(leeway time.Duration) error {
	now := time.Now().Unix()
	skew := int64(leeway.Seconds())

	if c.ExpiresAt > 0 && now > c.ExpiresAt+skew {
		return ErrExpiredToken
	}

	if c.NotBefore > 0 && now < c.NotBefore-skew {
		return ErrTokenNotYetValid
	}

	return nil
}

// Config holds JWT service configuration.
type Config struct {
	// SigningKey is the HMAC-SHA256 secret. It must be at least 32 bytes.
	SigningKey string `env:"SIGNING_KEY,required"`
	// Leeway is the clock-skew tolerance applied to exp/nbf validation during Parse.
	// Defaults to 0 (strict). A small value (e.g. 30-60s) is recommended when token
	// issuers and verifiers may have slightly skewed clocks.
	Leeway time.Duration `env:"LEEWAY"`
}

// Service handles JWT token generation and validation using HMAC-SHA256.
// The signing key is kept in memory only and should be cryptographically secure.
type Service struct {
	signingKey []byte
	leeway     time.Duration
}

// New creates a new JWT service with the provided configuration.
// The signing key must be at least 32 bytes for adequate security with HMAC-SHA256;
// a shorter key is rejected with ErrInvalidSigningKey.
func New(cfg Config) (*Service, error) {
	if cfg.SigningKey == "" {
		return nil, ErrMissingSigningKey
	}

	if len(cfg.SigningKey) < minSigningKeyBytes {
		return nil, ErrInvalidSigningKey
	}

	return &Service{
		signingKey: []byte(cfg.SigningKey),
		leeway:     cfg.Leeway,
	}, nil
}

// Generate creates a JWT token with the given claims.
// Accepts any JSON-serializable claims structure and returns a signed JWT string.
func (s *Service) Generate(claims any) (string, error) {
	if claims == nil {
		return "", ErrMissingClaims
	}

	header := Header{
		Type:      HeaderType,
		Algorithm: HeaderAlgorithm,
	}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", fmt.Errorf("failed to marshal header: %w", err)
	}

	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("failed to marshal claims: %w", err)
	}

	// Build JWT payload: base64url(header).base64url(claims)
	headerEncoded := base64URLEncode(headerJSON)
	claimsEncoded := base64URLEncode(claimsJSON)
	payload := headerEncoded + "." + claimsEncoded

	signature := s.sign(payload)
	token := payload + "." + signature

	return token, nil
}

// Parse validates a JWT token and unmarshals its claims into the provided structure.
// Performs cryptographic verification, algorithm validation, and temporal claim checks.
func (s *Service) Parse(tokenString string, claims any) error {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return ErrInvalidToken
	}

	headerEncoded := parts[0]
	claimsEncoded := parts[1]
	signatureEncoded := parts[2]

	// Verify signature using constant-time comparison to prevent timing attacks
	payload := headerEncoded + "." + claimsEncoded
	expectedSignature := s.sign(payload)
	if subtle.ConstantTimeCompare([]byte(signatureEncoded), []byte(expectedSignature)) != 1 {
		return ErrInvalidSignature
	}

	headerJSON, err := base64URLDecode(headerEncoded)
	if err != nil {
		return fmt.Errorf("failed to decode header: %w", err)
	}

	var header Header
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return fmt.Errorf("failed to unmarshal header: %w", err)
	}

	// Reject tokens using unexpected algorithms to prevent algorithm confusion attacks
	if header.Algorithm != HeaderAlgorithm {
		return ErrUnexpectedSigningMethod
	}

	claimsJSON, err := base64URLDecode(claimsEncoded)
	if err != nil {
		return fmt.Errorf("failed to decode claims: %w", err)
	}

	if err := json.Unmarshal(claimsJSON, claims); err != nil {
		return fmt.Errorf("failed to unmarshal claims: %w", err)
	}

	// Always enforce the registered temporal claims (exp/nbf) by decoding the
	// standard fields separately. This guarantees expired or not-yet-valid tokens
	// are rejected even when the caller's claims type does not implement Valid(),
	// closing the footgun where temporal validation could be silently skipped.
	var temporal StandardClaims
	if err := json.Unmarshal(claimsJSON, &temporal); err != nil {
		return fmt.Errorf("failed to unmarshal temporal claims: %w", err)
	}
	if err := temporal.validate(s.leeway); err != nil {
		return err
	}

	// Run any additional application-specific validation declared by the claims type.
	// The registered temporal claims are already validated above with the configured
	// leeway, so a temporal error surfaced here (e.g. from the promoted, zero-leeway
	// StandardClaims.Valid on an embedder) is ignored when our leeway-aware check
	// already accepted the token; non-temporal errors are always propagated.
	if validator, ok := claims.(interface{ Valid() error }); ok {
		if err := validator.Valid(); err != nil && !isTemporalError(err) {
			return err
		}
	}

	return nil
}

// isTemporalError reports whether err is one of the registered temporal-claim errors
// (exp/nbf), which Parse validates separately with the configured leeway.
func isTemporalError(err error) bool {
	return errors.Is(err, ErrExpiredToken) || errors.Is(err, ErrTokenNotYetValid)
}

// sign creates an HMAC-SHA256 signature for the given payload.
// Returns base64url-encoded signature as required by RFC 7515.
func (s *Service) sign(payload string) string {
	h := hmac.New(sha256.New, s.signingKey)
	h.Write([]byte(payload))
	return base64URLEncode(h.Sum(nil))
}

// base64URLEncode encodes data using base64url encoding without padding (RFC 7515).
func base64URLEncode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

// base64URLDecode decodes unpadded base64url-encoded data (RFC 7515).
func base64URLDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}
