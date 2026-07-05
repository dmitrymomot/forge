package ratelimit

import "errors"

// ErrLimited indicates the subject exceeded its rate; used by the middleware's
// responder and available to non-HTTP callers.
var ErrLimited = errors.New("ratelimit: limit exceeded")
