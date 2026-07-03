package password

import "errors"

// ErrMismatch is exported for callers that prefer a typed mismatch; Verify itself
// reports a wrong password via its ok return value, not this error.
var ErrMismatch = errors.New("password: hash mismatch")

// ErrInvalidHash is returned when the encoded hash cannot be parsed.
var ErrInvalidHash = errors.New("password: malformed encoded hash")
