package debug

import "errors"

// Sentinel errors returned (often joined) by Run. Match with errors.Is.
var (
	// ErrInvalidConfig is returned (joined) by Run when an option or Config field has an invalid value.
	ErrInvalidConfig = errors.New("debug: invalid config")
	// ErrAuthRequired is returned by Run when the server would bind a non-loopback
	// address with no auth middleware configured and no explicit WithoutAuth opt-out.
	ErrAuthRequired = errors.New("debug: non-loopback address requires auth (WithBasicAuth/WithMiddleware) or an explicit WithoutAuth")
)
