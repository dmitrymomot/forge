package cookie

import "errors"

var (
	// ErrInvalidCookie covers absent, tampered, or undecryptable cookies. One
	// error for all three so callers can't build a padding/absence oracle.
	ErrInvalidCookie = errors.New("cookie: invalid cookie")
	// ErrTooLarge means the encoded Set-Cookie header exceeds 4096 bytes,
	// which browsers truncate or drop silently.
	ErrTooLarge = errors.New("cookie: encoded cookie exceeds 4096 bytes")
	// ErrInvalidConfig covers bad construction input and policy violations
	// such as a __Host- name with an incompatible policy.
	ErrInvalidConfig = errors.New("cookie: invalid config")
)
