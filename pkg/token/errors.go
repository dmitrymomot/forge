package token

import "errors"

var (
	// ErrInvalidToken is returned when the token string is malformed:
	// it lacks the payload/signature separator, has an empty payload or
	// signature segment, or contains invalid base64url encoding.
	ErrInvalidToken = errors.New("token: invalid token")

	// ErrSignatureInvalid is returned when the token's HMAC signature
	// does not match the expected signature for the given payload and secret.
	ErrSignatureInvalid = errors.New("token: invalid signature")

	// ErrEmptySecret is returned when a nil or empty secret key is provided.
	ErrEmptySecret = errors.New("token: empty secret")
)
