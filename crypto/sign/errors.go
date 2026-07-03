package sign

import "errors"

// ErrInvalidKey is returned when a signer is built with no usable key.
var ErrInvalidKey = errors.New("sign: invalid key")

// ErrBadSignature is exported for callers that surface a typed verification failure; the
// Verify/VerifyString methods themselves return false rather than an error.
var ErrBadSignature = errors.New("sign: signature mismatch")
