package decimal

import (
	"fmt"
	"math/big"
)

// Add returns d + e, exact, at scale max(d.scale, e.scale). Never rounds.
func (d Decimal) Add(e Decimal) Decimal { return d.addSub(e, false) }

// Sub returns d - e, exact, at scale max(d.scale, e.scale). Never rounds.
func (d Decimal) Sub(e Decimal) Decimal { return d.addSub(e, true) }

// addSub implements Add (sub=false) and Sub (sub=true).
func (d Decimal) addSub(e Decimal, sub bool) Decimal {
	scale := max(d.scale, e.scale)

	// Fast path: both int64, and aligning both to the common scale stays in int64.
	if d.big == nil && e.big == nil {
		ac, aok := scaleInt64(d.coef, scale-d.scale)
		bc, bok := scaleInt64(e.coef, scale-e.scale)
		if aok && bok {
			if sub {
				if r, ok := subInt64(ac, bc); ok {
					return Decimal{coef: r, scale: scale}
				}
			} else {
				if r, ok := addInt64(ac, bc); ok {
					return Decimal{coef: r, scale: scale}
				}
			}
		}
	}

	// Big path.
	da := d.bigCoef()
	if d.scale < scale {
		da.Mul(da, pow10Big(scale-d.scale))
	}
	eb := e.bigCoef()
	if e.scale < scale {
		eb.Mul(eb, pow10Big(scale-e.scale))
	}
	if sub {
		da.Sub(da, eb)
	} else {
		da.Add(da, eb)
	}
	return fromBig(da, scale)
}

// Mul returns d * e, exact, at scale d.scale + e.scale. Never rounds.
func (d Decimal) Mul(e Decimal) Decimal {
	scale := d.scale + e.scale
	if d.big == nil && e.big == nil {
		if r, ok := mulInt64(d.coef, e.coef); ok {
			return Decimal{coef: r, scale: scale}
		}
	}
	prod := new(big.Int).Mul(d.bigCoef(), e.bigCoef())
	return fromBig(prod, scale)
}

// Div returns d / e computed to exactly `scale` fractional digits using mode.
// A non-terminating quotient (e.g. 1/3) is well-defined because the caller names
// the scale and rounding mode. e == 0 returns ErrDivByZero.
func (d Decimal) Div(e Decimal, scale int32, mode RoundingMode) (Decimal, error) {
	if e.IsZero() {
		return Decimal{}, fmt.Errorf("decimal: %w", ErrDivByZero)
	}
	if scale < 0 {
		scale = 0
	}

	// We want quotient = d / e rounded to `scale` fractional digits.
	// numerator   = coefₐ × 10^(scale + scale_e − scaleₐ), scaled so the integer
	//               division by coef_e yields exactly `scale` fractional digits.
	// If the exponent is negative, scale the denominator instead.
	num := d.bigCoef()
	den := e.bigCoef()

	exp := int64(scale) + int64(e.scale) - int64(d.scale)
	if exp >= 0 {
		num.Mul(num, pow10Big(int32(exp)))
	} else {
		den.Mul(den, pow10Big(int32(-exp)))
	}

	// Rounded integer division: q = round(num / den) with the requested mode.
	q := divRound(num, den, mode)
	return fromBig(q, scale), nil
}

// scaleInt64 returns c * 10^n staying in int64, reporting ok=false on overflow.
func scaleInt64(c int64, n int32) (int64, bool) {
	for range int(n) {
		hi := c * 10
		if c != 0 && hi/10 != c {
			return 0, false
		}
		c = hi
	}
	return c, true
}

// addInt64 returns a+b, reporting ok=false on overflow.
func addInt64(a, b int64) (int64, bool) {
	s := a + b
	if (a > 0 && b > 0 && s < 0) || (a < 0 && b < 0 && s >= 0) {
		return 0, false
	}
	return s, true
}

// subInt64 returns a-b, reporting ok=false on overflow.
func subInt64(a, b int64) (int64, bool) {
	s := a - b
	if (b < 0 && a > 0 && s < 0) || (b > 0 && a < 0 && s >= 0) {
		return 0, false
	}
	return s, true
}

// mulInt64 returns a*b, reporting ok=false on overflow.
func mulInt64(a, b int64) (int64, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	p := a * b
	if p/b != a {
		return 0, false
	}
	// -MinInt64 special case guard: a*b == MinInt64 with a==-1 is fine; the p/b
	// check above already catches the overflowing pairs.
	return p, true
}
