package sse

import "errors"

var (
	// ErrInvalidEvent is returned by Writer.Send for an event that cannot be
	// framed: an ID or event name containing a line break, an ID containing
	// NUL, or a negative Retry.
	ErrInvalidEvent = errors.New("sse: invalid event")
	// ErrStreamingUnsupported is returned by NewWriter when the
	// http.ResponseWriter cannot flush, so events would sit in a buffer
	// instead of reaching the client.
	ErrStreamingUnsupported = errors.New("sse: streaming unsupported")
)
