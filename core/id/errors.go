package id

import "errors"

// ErrMalformed is returned by the Parse functions and by Scan when the input is
// not a valid encoding of the target ID type. Match it with errors.Is.
var ErrMalformed = errors.New("id: malformed")
