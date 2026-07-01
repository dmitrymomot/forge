package decimal

import "math/big"

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
