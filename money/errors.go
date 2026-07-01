package money

import "errors"

// ErrCurrencyMismatch is returned by Add, Sub, and Cmp when the two operands
// have different currencies.
var ErrCurrencyMismatch = errors.New("money: currency mismatch")

// ErrUnknownCurrency is returned when a currency code is not present in the
// bundled ISO-4217 table.
var ErrUnknownCurrency = errors.New("money: unknown currency")

// ErrInvalidAllocation is returned by Allocate when the ratios sum to zero or
// less (nothing to allocate proportionally), and by Split for a non-positive
// part count.
var ErrInvalidAllocation = errors.New("money: invalid allocation ratios")
