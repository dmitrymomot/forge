package country

import "errors"

// ErrUnknownCode is returned by NewSetFromCodes when a supplied alpha-2 code is
// not present in the bundled ISO-3166-1 table.
var ErrUnknownCode = errors.New("country: unknown code")
