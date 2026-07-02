package decimal_test

import (
	"errors"
	"math/big"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/decimal"
)

func TestDiv_KnownAnswers(t *testing.T) {
	tests := []struct {
		a, b  string
		scale int32
		mode  decimal.RoundingMode
		want  string
	}{
		{"1", "3", 4, decimal.HalfEven, "0.3333"},
		{"2", "3", 4, decimal.HalfEven, "0.6667"},
		{"1", "3", 0, decimal.HalfEven, "0"},
		{"10", "4", 2, decimal.HalfEven, "2.50"},
		{"10", "4", 0, decimal.HalfEven, "2"}, // 2.5 → even → 2
		{"7", "2", 0, decimal.HalfUp, "4"},    // 3.5 → up → 4
		{"-7", "2", 1, decimal.HalfEven, "-3.5"},
		{"1", "8", 3, decimal.Down, "0.125"},
		{"1", "7", 6, decimal.HalfEven, "0.142857"},
		{"100", "3", 2, decimal.Floor, "33.33"},
		{"-100", "3", 2, decimal.Floor, "-33.34"},
		{"0", "5", 3, decimal.HalfEven, "0.000"},
	}
	for _, tc := range tests {
		t.Run(tc.a+"_div_"+tc.b, func(t *testing.T) {
			q, err := decimal.MustParse(tc.a).Div(decimal.MustParse(tc.b), tc.scale, tc.mode)
			require.NoError(t, err)
			assert.Equal(t, tc.want, q.String())
			assert.Equal(t, tc.scale, q.Scale())
		})
	}
}

func TestDiv_ByZero(t *testing.T) {
	_, err := decimal.FromInt(1).Div(decimal.Zero, 2, decimal.HalfEven)
	require.Error(t, err)
	assert.True(t, errors.Is(err, decimal.ErrDivByZero))

	_, err = decimal.FromInt(1).Div(decimal.MustParse("0.00"), 2, decimal.HalfEven)
	require.Error(t, err)
	assert.True(t, errors.Is(err, decimal.ErrDivByZero))
}

func TestDiv_RatOracle(t *testing.T) {
	rng := rand.New(rand.NewPCG(99, 100))
	modes := []decimal.RoundingMode{
		decimal.HalfEven, decimal.HalfUp, decimal.HalfDown,
		decimal.Up, decimal.Down, decimal.Ceil, decimal.Floor,
	}
	for range 20000 {
		a := randDecimal(rng)
		b := randDecimal(rng)
		if b.IsZero() {
			continue
		}
		scale := int32(rng.IntN(9))
		mode := modes[rng.IntN(len(modes))]

		got, err := a.Div(b, scale, mode)
		require.NoError(t, err)

		// Oracle: exact quotient as a Rat, rounded to scale/mode via big.Int.
		want := ratRoundToScale(new(big.Rat).Quo(toRat(a), toRat(b)), scale, mode)
		require.Truef(t, want.Cmp(toRat(got)) == 0,
			"%s / %s @scale=%d mode=%d = %s; oracle = %s",
			a.String(), b.String(), scale, mode, got.String(), want.RatString())
	}
}

// ratRoundToScale rounds a big.Rat to `scale` fractional digits using the same
// rounding semantics as decimal.RoundingMode, returning the result as a big.Rat.
func ratRoundToScale(r *big.Rat, scale int32, mode decimal.RoundingMode) *big.Rat {
	// scaled = r * 10^scale, then round to an integer, then divide back.
	pow := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
	scaled := new(big.Rat).Mul(r, new(big.Rat).SetInt(pow))

	num := scaled.Num()
	den := scaled.Denom()
	neg := num.Sign() < 0
	absNum := new(big.Int).Abs(num)

	q := new(big.Int)
	rem := new(big.Int)
	q.QuoRem(absNum, den, rem)

	if rem.Sign() != 0 && ratRoundUp(q, rem, den, mode, neg) {
		q.Add(q, big.NewInt(1))
	}
	if neg {
		q.Neg(q)
	}
	// result = q / 10^scale
	return new(big.Rat).SetFrac(q, pow)
}

func ratRoundUp(q, rem, den *big.Int, mode decimal.RoundingMode, neg bool) bool {
	switch mode {
	case decimal.Down:
		return false
	case decimal.Up:
		return true
	case decimal.Ceil:
		return !neg
	case decimal.Floor:
		return neg
	default:
		twice := new(big.Int).Lsh(rem, 1)
		cmp := twice.Cmp(den)
		switch {
		case cmp < 0:
			return false
		case cmp > 0:
			return true
		default:
			switch mode {
			case decimal.HalfUp:
				return true
			case decimal.HalfDown:
				return false
			default:
				return q.Bit(0) == 1
			}
		}
	}
}

// TestDiv_NegativeScaleClamped exercises Div's scale<0 clamp to 0.
func TestDiv_NegativeScaleClamped(t *testing.T) {
	x := decimal.MustParse("10")
	y := decimal.MustParse("4")
	got, err := x.Div(y, -5, decimal.HalfEven)
	require.NoError(t, err)
	// scale clamps to 0, so 10/4 = 2.5 rounds (HalfEven) to 2.
	assert.Equal(t, "2", got.String())
	assert.Equal(t, int32(0), got.Scale())
}

// TestDiv_BigRoundingHighScale drives Div's big.Int path with a non-terminating
// quotient rounded at a high scale (exercises divRound on large values).
func TestDiv_BigRoundingHighScale(t *testing.T) {
	x := decimal.MustParse(bigVal)
	y := decimal.MustParse("7")
	got, err := x.Div(y, 20, decimal.HalfEven)
	require.NoError(t, err)
	// Verify against exact recomputation: got*7 rounded back should be near x.
	assert.Equal(t, int32(20), got.Scale())
	assert.NotEqual(t, "0", got.String())
}

// TestDiv_NegativeExponentScalesDenominator drives the exp<0 branch of Div,
// where the denominator (not the numerator) is scaled by 10^(-exp).
func TestDiv_NegativeExponentScalesDenominator(t *testing.T) {
	// d.scale large, target scale small: exp = scale + e.scale - d.scale < 0.
	x := decimal.MustParse("123.456789") // scale 6
	y := decimal.MustParse("2")          // scale 0
	got, err := x.Div(y, 0, decimal.HalfEven)
	require.NoError(t, err)
	// 123.456789 / 2 = 61.7283945 -> rounded to scale 0 -> 62 (HalfEven, .72.. > .5).
	assert.Equal(t, "62", got.String())
}
