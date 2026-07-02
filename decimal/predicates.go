package decimal

import "math/big"

// IsPositive reports whether d is strictly greater than zero.
func (d Decimal) IsPositive() bool { return d.Sign() > 0 }

// IsNegative reports whether d is strictly less than zero.
func (d Decimal) IsNegative() bool { return d.Sign() < 0 }

// IsInteger reports whether d represents a whole number, i.e. it has no nonzero
// fractional digits. 12, 12.0 and 12.00 are all integers; 12.5 is not.
func (d Decimal) IsInteger() bool {
	if d.scale <= 0 || d.IsZero() {
		return true
	}
	if d.big == nil {
		// Integer iff the coefficient is a multiple of 10^scale. When 10^scale
		// overflows int64, |coef| < 10^scale, so a nonzero coef cannot be a
		// multiple and d is therefore not an integer.
		div, ok := scaleInt64(1, d.scale)
		if !ok {
			return false
		}
		return d.coef%div == 0
	}
	return new(big.Int).Rem(d.big, pow10Big(d.scale)).Sign() == 0
}
