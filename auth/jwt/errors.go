package jwt

import "errors"

var (
	// ErrMalformed reports a token that is not a structurally valid compact JWS.
	ErrMalformed = errors.New("jwt: malformed token")
	// ErrSignature reports a signature that does not verify, including alg/key mismatches.
	ErrSignature = errors.New("jwt: signature mismatch")
	// ErrExpired reports a token past its exp claim, or missing exp when expiry is required.
	ErrExpired = errors.New("jwt: token expired")
	// ErrNotYetValid reports a token whose nbf claim is in the future.
	ErrNotYetValid = errors.New("jwt: token not yet valid")
	// ErrIssuerMismatch reports an iss claim that differs from the configured issuer.
	ErrIssuerMismatch = errors.New("jwt: issuer mismatch")
	// ErrAudienceMismatch reports an aud claim missing the configured audience.
	ErrAudienceMismatch = errors.New("jwt: audience mismatch")
	// ErrUnknownKey reports a kid that resolves to no known key.
	ErrUnknownKey = errors.New("jwt: unknown key")
	// ErrNoKeys reports key resolution over an empty key set.
	ErrNoKeys = errors.New("jwt: no keys available")
	// ErrBadKey reports invalid key material or configuration at construction.
	ErrBadKey = errors.New("jwt: invalid key material")
)
