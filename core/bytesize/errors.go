package bytesize

import "errors"

// ErrInvalidSize is returned when a string cannot be parsed as a byte size.
var ErrInvalidSize = errors.New("bytesize: invalid size")
