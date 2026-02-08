package geolocation

import "errors"

// Sentinel errors for geolocation operations.
var (
	// ErrClosed is returned when an operation is attempted on a closed provider.
	ErrClosed = errors.New("geolocation: provider is closed")

	// ErrInvalidIP is returned when an IP address string cannot be parsed.
	ErrInvalidIP = errors.New("geolocation: invalid IP address")
)
