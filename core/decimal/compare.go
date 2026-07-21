package decimal

import (
	"cmp"
	"math/big"
)

// Cmp compares d and e numerically, ignoring stored scale, and returns -1, 0, or
// +1 as d < e, d == e, or d > e.
func (d Decimal) Cmp(e Decimal) int {
	if d.big == nil && e.big == nil {
		// Identical scale: compare coefficients directly.
		if d.scale == e.scale {
			return cmp.Compare(d.coef, e.coef)
		}
		// Mixed scales: align both coefficients to the common scale in int64.
		scale := max(d.scale, e.scale)
		dc, dok := scaleInt64(d.coef, scale-d.scale)
		ec, eok := scaleInt64(e.coef, scale-e.scale)
		if dok && eok {
			return cmp.Compare(dc, ec)
		}
	}
	// Differing signs settle the answer without big.Int allocation; equal signs
	// (a coefficient is big-backed, or int64 alignment overflowed) fall through
	// to the exact big comparison.
	if ds, es := d.Sign(), e.Sign(); ds != es {
		return cmp.Compare(ds, es)
	}
	// Both int64 at different scales: align on int64 when it cannot overflow.
	if d.big == nil && e.big == nil {
		if c, ok := cmpInt64(d, e); ok {
			return c
		}
	}
	da, eb := alignBig(d, e)
	return da.Cmp(eb)
}

// Equal reports whether d and e are numerically equal (scale-normalized).
func (d Decimal) Equal(e Decimal) bool { return d.Cmp(e) == 0 }

// cmpInt64 compares two int64-mode decimals by aligning the lower-scale
// coefficient up on int64. ok is false when the alignment would overflow,
// sending the caller to the big path.
func cmpInt64(d, e Decimal) (int, bool) {
	dc, ec := d.coef, e.coef
	switch {
	case d.scale < e.scale:
		v, ok := mulPow10Int64(dc, e.scale-d.scale)
		if !ok {
			return 0, false
		}
		dc = v
	case e.scale < d.scale:
		v, ok := mulPow10Int64(ec, d.scale-e.scale)
		if !ok {
			return 0, false
		}
		ec = v
	}
	switch {
	case dc < ec:
		return -1, true
	case dc > ec:
		return 1, true
	default:
		return 0, true
	}
}

// alignBig returns the two coefficients scaled to a common (maximum) scale as
// *big.Int, so comparison/addition never overflows.
func alignBig(d, e Decimal) (*big.Int, *big.Int) {
	da := d.bigCoef()
	eb := e.bigCoef()
	switch {
	case d.scale < e.scale:
		da.Mul(da, pow10Big(e.scale-d.scale))
	case e.scale < d.scale:
		eb.Mul(eb, pow10Big(d.scale-e.scale))
	}
	return da, eb
}
