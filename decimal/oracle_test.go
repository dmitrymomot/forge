package decimal_test

import (
	"math/big"
	"math/rand/v2"

	"github.com/dmitrymomot/forge/decimal"
)

// toRat converts a Decimal to an exact big.Rat via its string form, which is the
// oracle-safe conversion (String is the canonical exact representation).
func toRat(d decimal.Decimal) *big.Rat {
	r, ok := new(big.Rat).SetString(d.String())
	if !ok {
		panic("toRat: unparseable decimal string: " + d.String())
	}
	return r
}

// randDecimal builds a random Decimal: a signed coefficient (occasionally near the
// int64 boundary to exercise overflow) and a scale in [0,6].
func randDecimal(rng *rand.Rand) decimal.Decimal {
	scale := int32(rng.IntN(7))
	var coef int64
	switch rng.IntN(4) {
	case 0:
		// near the positive int64 boundary
		coef = 9223372036854775807 - int64(rng.IntN(1000))
	case 1:
		// near the negative int64 boundary
		coef = -9223372036854775808 + int64(rng.IntN(1000))
	default:
		coef = rng.Int64N(2_000_000_001) - 1_000_000_000
	}
	if rng.IntN(2) == 0 {
		coef = -coef
	}
	return decimal.New(coef, scale)
}
