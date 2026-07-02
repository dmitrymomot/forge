package decimal

import "errors"

// ErrSyntax is returned by Parse for input that is not a valid decimal literal.
var ErrSyntax = errors.New("decimal: invalid syntax")

// ErrDivByZero is returned by Div when the divisor is zero.
var ErrDivByZero = errors.New("decimal: division by zero")

// ErrScan is returned by Scan when the SQL source value cannot be converted to a
// Decimal (a nil source, an unsupported Go type, or a float64 — which is rejected
// to avoid importing binary-float rounding error into an exact value).
var ErrScan = errors.New("decimal: unsupported Scan source")
