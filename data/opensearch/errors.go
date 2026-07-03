package opensearch

import "errors"

// Sentinel errors returned (often wrapped) by this package. Match with errors.Is.
var (
	// ErrInvalidConfig is returned (joined) when a Config field or option value is invalid.
	ErrInvalidConfig = errors.New("opensearch: invalid config")
	// ErrConnect is returned by Open when the cluster could not be reached within the retry budget.
	ErrConnect = errors.New("opensearch: connect failed")
	// ErrHealthcheck is returned by the Healthcheck closure when a liveness probe fails.
	ErrHealthcheck = errors.New("opensearch: healthcheck failed")
	// ErrSetup is returned by Setup.Apply when index/template provisioning fails.
	ErrSetup = errors.New("opensearch: setup failed")
)
