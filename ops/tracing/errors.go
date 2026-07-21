package tracing

import "errors"

// ErrInvalidTraceparent reports a malformed W3C traceparent header value.
var ErrInvalidTraceparent = errors.New("tracing: invalid traceparent")
