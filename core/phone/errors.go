package phone

import "errors"

// ErrInvalidNumber is returned when input contains non-digit content or its
// digit count is outside E.164 bounds (empty national number, or over 15 total).
var ErrInvalidNumber = errors.New("phone: invalid number")

// ErrMissingCountryCode is returned by Parse when input has no leading + or 00
// and no region context supplies a dial code.
var ErrMissingCountryCode = errors.New("phone: missing country code")

// ErrUnknownDialCode is returned when the leading digits match no country
// calling code in the bundled table.
var ErrUnknownDialCode = errors.New("phone: unknown dial code")

// ErrUnsupportedRegion is returned by a gated Parser when the resolved country
// is provably outside the configured supported-countries Set.
var ErrUnsupportedRegion = errors.New("phone: unsupported region")
