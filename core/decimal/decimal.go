package decimal

import "math/big"

// Decimal is an exact base-10 fixed-point number: value = coef × 10^(−scale),
// with scale ≥ 0. The zero value is 0.
//
// Exactly one representation is active: when big is nil the coefficient lives in
// coef (int64 fast path); when big is non-nil the coefficient lives in big and
// coef is unused. Operations restore this invariant and demote big→int64 whenever
// the result fits.
type Decimal struct {
	big   *big.Int // non-nil ⇒ big mode; holds the coefficient
	coef  int64    // coefficient when big == nil
	scale int32    // number of fractional digits; always ≥ 0
}

// Zero is the decimal 0 (scale 0). It is equivalent to the zero value.
var Zero Decimal

// bigTen is reused for scale adjustments.
var bigTen = big.NewInt(10)

// New returns the decimal coef × 10^(−scale). A negative scale is normalized to
// scale 0 by scaling the coefficient up (New(5, -2) == 500).
func New(coef int64, scale int32) Decimal {
	if scale >= 0 {
		return Decimal{coef: coef, scale: scale}
	}
	// Negative scale: multiply coef by 10^(-scale), promoting to big on overflow.
	pow := -scale
	d := Decimal{coef: coef, scale: 0}
	return d.mulPow10(pow)
}

// FromInt returns the decimal i at scale 0.
func FromInt(i int64) Decimal { return Decimal{coef: i, scale: 0} }

// Scale reports the number of fractional digits stored.
func (d Decimal) Scale() int32 { return d.scale }

// Sign returns -1, 0, or +1 as d is negative, zero, or positive.
func (d Decimal) Sign() int {
	if d.big != nil {
		return d.big.Sign()
	}
	switch {
	case d.coef < 0:
		return -1
	case d.coef > 0:
		return 1
	default:
		return 0
	}
}

// IsZero reports whether d is exactly zero (any scale).
func (d Decimal) IsZero() bool { return d.Sign() == 0 }

// bigCoef returns the coefficient as a *big.Int (a fresh copy; never aliases d).
func (d Decimal) bigCoef() *big.Int {
	if d.big != nil {
		return new(big.Int).Set(d.big)
	}
	return big.NewInt(d.coef)
}

// fromBig builds a normalized Decimal from a big coefficient and scale, demoting
// to the int64 fast path when the coefficient fits.
func fromBig(b *big.Int, scale int32) Decimal {
	if b.IsInt64() {
		return Decimal{coef: b.Int64(), scale: scale}
	}
	return Decimal{big: new(big.Int).Set(b), scale: scale}
}

// mulPow10 returns d with its coefficient multiplied by 10^n (n ≥ 0), keeping the
// same scale. Used for negative-scale normalization and scale alignment. It stays
// on the int64 path while it fits, promoting to big on overflow.
func (d Decimal) mulPow10(n int32) Decimal {
	if n == 0 {
		return d
	}
	if d.big == nil {
		// Try to stay on the int64 path.
		coef := d.coef
		ok := true
		for range n {
			hi := coef * 10
			if coef != 0 && hi/10 != coef {
				ok = false
				break
			}
			coef = hi
		}
		if ok {
			return Decimal{coef: coef, scale: d.scale}
		}
	}
	b := d.bigCoef()
	pow := new(big.Int).Exp(bigTen, big.NewInt(int64(n)), nil)
	b.Mul(b, pow)
	return fromBig(b, d.scale)
}

// pow10Big returns 10^n as a *big.Int (n ≥ 0).
func pow10Big(n int32) *big.Int {
	return new(big.Int).Exp(bigTen, big.NewInt(int64(n)), nil)
}

// pow10Int64 holds the powers of ten representable in an int64 (10^0..10^18).
var pow10Int64 = [...]int64{
	1, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9,
	1e10, 1e11, 1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18,
}

// mulPow10Int64 returns v × 10^n if it fits in an int64, reporting overflow
// (or n beyond the table) as ok == false.
func mulPow10Int64(v int64, n int32) (int64, bool) {
	if n < 0 || int(n) >= len(pow10Int64) {
		return 0, false
	}
	p := pow10Int64[n]
	hi := v * p
	if v != 0 && hi/p != v {
		return 0, false
	}
	return hi, true
}
