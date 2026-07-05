package id

import "errors"

// ErrMalformed is returned by the Parse functions and by Scan when the input is
// not a valid encoding of the target ID type. Match it with errors.Is.
var ErrMalformed = errors.New("id: malformed")

// ErrWrongPrefix is returned by Prefix.Parse (and ParsePrefixed) when the input
// does not carry the expected "<prefix>_" head. Match it with errors.Is.
var ErrWrongPrefix = errors.New("id: wrong prefix")
