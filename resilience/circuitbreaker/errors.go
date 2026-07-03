package circuitbreaker

import "errors"

// ErrOpen is returned by Do when the circuit is open and the call is rejected.
var ErrOpen = errors.New("circuitbreaker: circuit open")
