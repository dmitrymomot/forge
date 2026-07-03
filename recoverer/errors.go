package recoverer

import "errors"

// ErrPanic wraps a recovered panic value passed to the Responder, so responders
// and logs can match it with errors.Is.
var ErrPanic = errors.New("recoverer: handler panicked")
