package encoding

import "errors"

// ErrInvalidEncoding is returned when input contains characters outside the
// codec's alphabet or otherwise cannot be decoded.
var ErrInvalidEncoding = errors.New("encoding: invalid input")
