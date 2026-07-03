package mongo

import "errors"

// Sentinel errors returned (often wrapped) by this package. Match with errors.Is.
// They are single-line and carry no embedded blobs; failures wrap them with the
// underlying driver error via fmt.Errorf("%w: %v", …) or errors.Join.
var (
	// ErrInvalidConfig is returned (joined) when an option or Config field has an invalid value.
	ErrInvalidConfig = errors.New("mongo: invalid config")
	// ErrConnect is returned when Open exhausts its connect/ping retries.
	ErrConnect = errors.New("mongo: connect failed")
	// ErrHealthcheck is returned by the Healthcheck closure when a ping fails.
	ErrHealthcheck = errors.New("mongo: healthcheck failed")
)
