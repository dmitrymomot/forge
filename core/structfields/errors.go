package structfields

import "errors"

// ErrNotStruct is returned by Walk when v is neither a struct nor a non-nil
// pointer to a struct.
var ErrNotStruct = errors.New("structfields: not a struct")

// ErrNotSettable is returned by a Field's Set when the target value cannot be
// assigned — either because Walk received a non-pointer struct (read-only) or
// the field is otherwise unsettable.
var ErrNotSettable = errors.New("structfields: field not settable")

// ErrUnsupportedKind is returned by SetString when the field's kind cannot be
// parsed from a string.
var ErrUnsupportedKind = errors.New("structfields: unsupported kind")
