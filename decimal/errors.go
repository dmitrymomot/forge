package decimal

import "errors"

// ErrSyntax is returned by Parse for input that is not a valid decimal literal.
var ErrSyntax = errors.New("decimal: invalid syntax")

// ErrDivByZero is returned by Div when the divisor is zero.
var ErrDivByZero = errors.New("decimal: division by zero")
