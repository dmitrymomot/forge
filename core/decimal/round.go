package decimal

import (
	"math"
	"math/big"
)

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
	if d.big == nil {
		if q, ok := roundInt64(d.coef, drop, mode); ok {
			return Decimal{coef: q, scale: scale}
		}
	}
	rounded := roundBig(d.bigCoef(), drop, mode)
	return fromBig(rounded, scale)
}

// RescaleExact is like Rescale but also reports whether the operation was
// lossless. exact is false only when reducing the scale discarded one or more
// nonzero digits (rounding changed the numeric value); increasing or preserving
// the scale is always exact. It lets a caller assert, e.g., that an amount fits
// in a currency's minor units without silent rounding.
func (d Decimal) RescaleExact(scale int32, mode RoundingMode) (result Decimal, exact bool) {
	out := d.Rescale(scale, mode)
	return out, out.Cmp(d) == 0
}

// Truncate returns d with the fractional digits beyond places removed, rounding
// toward zero. It never increases the scale: Truncate(2) leaves 1.5 as 1.5 and
// reduces 1.789 to 1.78. A negative places is treated as 0.
func (d Decimal) Truncate(places int32) Decimal {
	if places < 0 {
		places = 0
	}
	if places >= d.scale {
		return d
	}
	return d.Rescale(places, Down)
}

// Floor returns the greatest integer value less than or equal to d (rounding
// toward −∞), at scale 0.
func (d Decimal) Floor() Decimal { return d.Rescale(0, Floor) }

// Ceil returns the least integer value greater than or equal to d (rounding
// toward +∞), at scale 0.
func (d Decimal) Ceil() Decimal { return d.Rescale(0, Ceil) }

// pow10Int64 holds 10^0 … 10^18, every power of ten that fits int64.
var pow10Int64 = [19]int64{
	1, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9,
	1e10, 1e11, 1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18,
}

// roundInt64 is the allocation-free counterpart of roundBig for int64
// coefficients: it divides coef by 10^drop (drop ≥ 0), applying mode to the
// remainder. ok=false defers to the big path when drop exceeds the int64
// power-of-ten table or coef is MinInt64 (whose magnitude overflows int64).
func roundInt64(coef int64, drop int32, mode RoundingMode) (int64, bool) {
	if drop <= 0 {
		return coef, true
	}
	if int(drop) >= len(pow10Int64) || coef == math.MinInt64 {
		return 0, false
	}
	div := pow10Int64[drop]

	neg := coef < 0
	abs := coef
	if neg {
		abs = -abs
	}

	q, r := abs/div, abs%div
	if r != 0 && roundAwayFromZeroInt64(q, r, div, mode, neg) {
		q++ // cannot overflow: q ≤ |coef|/10
	}
	if neg {
		q = -q
	}
	return q, true
}

// roundAwayFromZeroInt64 mirrors roundAwayFromZero for a positive magnitude
// quotient q with nonzero remainder r (0 < r < div ≤ 10^18): it decides
// whether to increment the magnitude by one. neg is the sign of the original
// value, used by the sign-aware modes.
func roundAwayFromZeroInt64(q, r, div int64, mode RoundingMode, neg bool) bool {
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
		// Half modes: compare 2*r against div; 2*r < 2×10^18 never overflows.
		switch twice := 2 * r; {
		case twice < div:
			return false // below half → toward zero
		case twice > div:
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
				return q&1 == 1
			}
		}
	}
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

// divRound computes round(num / den) as a *big.Int using mode. den must be
// nonzero. The sign of the true quotient is num.Sign() * den.Sign().
func divRound(num, den *big.Int, mode RoundingMode) *big.Int {
	// Determine result sign, then work on absolute magnitudes.
	neg := (num.Sign() < 0) != (den.Sign() < 0)
	an := new(big.Int).Abs(num)
	ad := new(big.Int).Abs(den)

	q := new(big.Int)
	r := new(big.Int)
	q.QuoRem(an, ad, r) // an = q*ad + r, 0 ≤ r < ad

	if r.Sign() != 0 && roundAwayFromZero(q, r, ad, mode, neg) {
		q.Add(q, big.NewInt(1))
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
