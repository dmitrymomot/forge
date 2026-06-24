package jwt

import "errors"

var (
	ErrInvalidToken            = errors.New("jwt: invalid token")
	ErrExpiredToken            = errors.New("jwt: token is expired")
	ErrTokenNotYetValid        = errors.New("jwt: token is not valid yet")
	ErrMissingSigningKey       = errors.New("jwt: missing signing key")
	ErrInvalidSigningKey       = errors.New("jwt: signing key too short (minimum 32 bytes)")
	ErrMissingClaims           = errors.New("jwt: missing claims")
	ErrInvalidSignature        = errors.New("jwt: invalid signature")
	ErrUnexpectedSigningMethod = errors.New("jwt: unexpected signing method")
)
