package typeconv

import "errors"

// ErrUnsupportedType is returned by Parse when T is not one of the supported
// base kinds.
var ErrUnsupportedType = errors.New("typeconv: unsupported type")

// ErrSyntax is returned when the input cannot be parsed into the target type.
// It wraps the underlying strconv/time error.
var ErrSyntax = errors.New("typeconv: invalid syntax")
