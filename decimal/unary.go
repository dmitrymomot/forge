package decimal

import "math/big"

// Neg returns -d, preserving scale.
func (d Decimal) Neg() Decimal {
	if d.big != nil {
		return Decimal{big: new(big.Int).Neg(d.big), scale: d.scale}
	}
	// -MinInt64 overflows int64; promote to big.
	if d.coef == -9223372036854775808 {
		return Decimal{big: new(big.Int).Neg(big.NewInt(d.coef)), scale: d.scale}
	}
	return Decimal{coef: -d.coef, scale: d.scale}
}

// Abs returns |d|, preserving scale.
func (d Decimal) Abs() Decimal {
	if d.Sign() < 0 {
		return d.Neg()
	}
	return d
}
