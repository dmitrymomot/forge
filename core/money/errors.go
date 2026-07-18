package money

import "errors"

// ErrCurrencyMismatch is returned by Add, Sub, and Cmp when the two operands
// have different currencies.
var ErrCurrencyMismatch = errors.New("money: currency mismatch")

// ErrUnknownCurrency is returned when a currency code is not present in the
// bundled ISO-4217 table.
var ErrUnknownCurrency = errors.New("money: unknown currency")

// ErrInvalidAllocation is returned by Allocate when no ratios are given, any
// ratio is negative, or the ratios sum to zero or less (nothing to allocate
// proportionally), and by Split for a non-positive part count.
var ErrInvalidAllocation = errors.New("money: invalid allocation ratios")

// ErrScan is returned by Money.Scan when the source is nil, an unsupported
// type, or a string missing the "<amount> <code>" shape (no separating
// space). A malformed amount or unrecognized currency code within an
// otherwise well-shaped string instead surfaces decimal's parse error or
// ErrUnknownCurrency. ErrScan is also returned by Value and MarshalJSON when
// the Money has no currency code to serialize.
var ErrScan = errors.New("money: unsupported Scan source")

// ErrOverflow is returned by Allocate and Split when the amount's minor-unit
// count or a proportional product exceeds int64 range, or the ratio sum
// overflows int — cases where the largest-remainder split cannot be computed
// without silent wraparound. (The decimal amount itself is arbitrary-precision;
// only these integer intermediates are bounded.)
var ErrOverflow = errors.New("money: integer overflow")
