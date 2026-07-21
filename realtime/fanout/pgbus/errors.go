package pgbus

import "errors"

var (
	// ErrPayloadTooLarge is returned by Publish when the message envelope
	// exceeds the Postgres NOTIFY payload limit (8000 bytes).
	ErrPayloadTooLarge = errors.New("pgbus: payload too large for NOTIFY")
	// ErrPublish wraps a failed pg_notify round trip.
	ErrPublish = errors.New("pgbus: publish failed")
)
