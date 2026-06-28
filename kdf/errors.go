package kdf

import "errors"

// ErrInvalidParams is returned when Params has a zero field.
var ErrInvalidParams = errors.New("kdf: invalid params")
