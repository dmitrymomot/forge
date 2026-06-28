package token

import "errors"

// ErrExpired is returned by Parse when the token's expiry has passed.
var ErrExpired = errors.New("token: expired")

// ErrBadSignature is returned by Parse when the signature or encryption fails to verify.
var ErrBadSignature = errors.New("token: signature mismatch")

// ErrMalformed is returned by Parse when the token cannot be decoded.
var ErrMalformed = errors.New("token: malformed")

// ErrWrongPurpose is returned by Parse when the token's purpose does not match the codec.
var ErrWrongPurpose = errors.New("token: wrong purpose")
