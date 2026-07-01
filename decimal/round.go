package decimal

import "math/big"

// RoundingMode selects how discarded low-order digits are handled when reducing
// scale. HalfEven (banker's rounding) is the default and zero value.
type RoundingMode int

const (
	// HalfEven rounds to the nearest neighbor; ties go to the even neighbor.
	HalfEven RoundingMode = iota
	// HalfUp rounds to the nearest neighbor; ties go away from zero.
	HalfUp
	// HalfDown rounds to the nearest neighbor; ties go toward zero.
	HalfDown
	// Up always rounds away from zero.
	Up
	// Down always rounds toward zero (truncation).
	Down
	// Ceil always rounds toward +∞.
	Ceil
	// Floor always rounds toward -∞.
	Floor
)

// Round returns d rounded to places fractional digits using mode. It is an alias
// for Rescale(places, mode).
func (d Decimal) Round(places int32, mode RoundingMode) Decimal {
	return d.Rescale(places, mode)
}

// Rescale returns d expressed at exactly scale fractional digits. Increasing the
// scale pads with zeros (exact); decreasing it applies mode to the discarded
// low-order digits. A negative target scale is treated as 0.
func (d Decimal) Rescale(scale int32, mode RoundingMode) Decimal {
	if scale < 0 {
		scale = 0
	}
	if scale == d.scale {
		return d
	}
	if scale > d.scale {
		// Pad with zeros: multiply coefficient by 10^(scale-d.scale).
		out := d.mulPow10(scale - d.scale)
		out.scale = scale
		return out
	}
	// Reduce scale: drop (d.scale - scale) low digits with rounding.
	drop := d.scale - scale
	rounded := roundBig(d.bigCoef(), drop, mode)
	return fromBig(rounded, scale)
}

// roundBig divides coef by 10^drop (drop ≥ 0), applying mode to the remainder,
// and returns the rounded quotient. Sign handling is exact for every mode.
func roundBig(coef *big.Int, drop int32, mode RoundingMode) *big.Int {
	if drop <= 0 {
		return new(big.Int).Set(coef)
	}
	div := pow10Big(drop)

	// Work on the absolute value; track the sign separately so half/away/toward
	// semantics are expressed relative to zero consistently.
	neg := coef.Sign() < 0
	abs := new(big.Int).Abs(coef)

	q := new(big.Int)
	r := new(big.Int)
	q.QuoRem(abs, div, r) // abs = q*div + r, 0 ≤ r < div

	if r.Sign() != 0 {
		if roundAwayFromZero(q, r, div, mode, neg) {
			q.Add(q, big.NewInt(1))
		}
	}
	if neg {
		q.Neg(q)
	}
	return q
}

// roundAwayFromZero decides, for a positive magnitude quotient q with nonzero
// remainder r (0 < r < div), whether to increment the magnitude by one. neg is
// the sign of the original value, used by the sign-aware modes.
func roundAwayFromZero(q, r, div *big.Int, mode RoundingMode, neg bool) bool {
	switch mode {
	case Down:
		return false
	case Up:
		return true
	case Ceil:
		// toward +∞: increase magnitude only for positive values.
		return !neg
	case Floor:
		// toward -∞: increase magnitude only for negative values.
		return neg
	default:
		// Half modes: compare 2*r against div.
		twice := new(big.Int).Lsh(r, 1) // 2*r
		cmp := twice.Cmp(div)
		switch {
		case cmp < 0:
			return false // below half → toward zero
		case cmp > 0:
			return true // above half → away from zero
		default:
			// Exactly half.
			switch mode {
			case HalfUp:
				return true
			case HalfDown:
				return false
			default: // HalfEven
				// Round to even: increment if current last digit of q is odd.
				return q.Bit(0) == 1
			}
		}
	}
}
