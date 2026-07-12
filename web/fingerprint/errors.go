package fingerprint

import "errors"

var (
	ErrNoSecret    = errors.New("fingerprint: config secret is required")
	ErrBadVersion  = errors.New("fingerprint: config version must be positive")
	ErrBadTokenTTL = errors.New("fingerprint: config token TTL must be positive")
	ErrBadToken    = errors.New("fingerprint: invalid or expired probe token")
)
