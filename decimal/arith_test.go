package decimal_test

import (
	"math"
	"math/big"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/decimal"
)

func TestAdd_Sub_Mul_KnownAnswers(t *testing.T) {
	tests := []struct {
		a, b        string
		add         string
		sub         string
		mul         string
		addSubScale int32 // max(scales)
		mulScale    int32 // sum of scales
	}{
		{"2.50", "0.25", "2.75", "2.25", "0.6250", 2, 4},
		{"1", "0.1", "1.1", "0.9", "0.1", 1, 1},
		{"0.1", "0.2", "0.3", "-0.1", "0.02", 1, 2},
		{"-1.5", "2.5", "1.0", "-4.0", "-3.75", 1, 2},
		{"100", "0", "100", "100", "0", 0, 0},
		{"3.14", "-3.14", "0.00", "6.28", "-9.8596", 2, 4},
	}
	for _, tc := range tests {
		t.Run(tc.a+"_"+tc.b, func(t *testing.T) {
			a := decimal.MustParse(tc.a)
			b := decimal.MustParse(tc.b)

			add := a.Add(b)
			sub := a.Sub(b)
			mul := a.Mul(b)

			assert.Equal(t, tc.add, add.String(), "add")
			assert.Equal(t, tc.sub, sub.String(), "sub")
			assert.Equal(t, tc.mul, mul.String(), "mul")
			assert.Equal(t, tc.addSubScale, add.Scale(), "add scale")
			assert.Equal(t, tc.addSubScale, sub.Scale(), "sub scale")
			assert.Equal(t, tc.mulScale, mul.Scale(), "mul scale")
		})
	}
}

func TestAdd_OverflowPromotesAndDemotes(t *testing.T) {
	max := decimal.New(9223372036854775807, 0)
	one := decimal.FromInt(1)
	// int64 max + 1 must promote to big and be exact.
	sum := max.Add(one)
	assert.Equal(t, "9223372036854775808", sum.String())
	// Subtracting back demotes to the int64 fast path and is exact.
	back := sum.Sub(one)
	assert.Equal(t, "9223372036854775807", back.String())
	assert.True(t, back.Equal(max))
}

func TestSub_MinInt64OverflowPromotes(t *testing.T) {
	// C4: 0 - MinInt64 overflows int64 and must promote to big.Int, yielding
	// the correct positive magnitude rather than wrapping back to MinInt64.
	got := decimal.Zero.Sub(decimal.New(math.MinInt64, 0))
	assert.Equal(t, "9223372036854775808", got.String())

	// Boundary: MinInt64 - (-1) also overflows and must promote.
	got2 := decimal.New(math.MinInt64, 0).Sub(decimal.FromInt(-1))
	assert.Equal(t, "-9223372036854775807", got2.String())

	// A normal in-range subtraction must stay exact on the fast path.
	inRange := decimal.FromInt(5).Sub(decimal.FromInt(3))
	assert.Equal(t, "2", inRange.String())
}

func TestMul_MinInt64OverflowPromotes(t *testing.T) {
	// C3: MinInt64 * -1 overflows int64 and must promote to big.Int, yielding
	// the correct positive magnitude rather than wrapping back to MinInt64.
	minInt := decimal.New(math.MinInt64, 0)
	negOne := decimal.FromInt(-1)

	got := minInt.Mul(negOne)
	assert.Equal(t, "9223372036854775808", got.String())

	// Operand order must not matter.
	gotRev := negOne.Mul(minInt)
	assert.Equal(t, "9223372036854775808", gotRev.String())

	// A normal in-range multiplication must stay exact on the fast path.
	inRange := decimal.FromInt(6).Mul(decimal.FromInt(7))
	assert.Equal(t, "42", inRange.String())
}

func TestAlgebraicLaws(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for range 2000 {
		a := randDecimal(rng)
		b := randDecimal(rng)
		c := randDecimal(rng)

		// a + 0 == a
		assert.True(t, a.Add(decimal.Zero).Equal(a))
		// a - a is numerically zero
		assert.True(t, a.Sub(a).IsZero())
		// a * 1 == a
		assert.True(t, a.Mul(decimal.FromInt(1)).Equal(a))
		// Neg(Neg(a)) == a
		assert.True(t, a.Neg().Neg().Equal(a))
		// commutativity
		assert.True(t, a.Add(b).Equal(b.Add(a)))
		assert.True(t, a.Mul(b).Equal(b.Mul(a)))
		// associativity of + (numeric)
		assert.True(t, a.Add(b).Add(c).Equal(a.Add(b.Add(c))))
		// associativity of * (numeric)
		assert.True(t, a.Mul(b).Mul(c).Equal(a.Mul(b.Mul(c))))
	}
}

func TestAddSubMul_RatOracle(t *testing.T) {
	rng := rand.New(rand.NewPCG(42, 7))
	for range 20000 {
		a := randDecimal(rng)
		b := randDecimal(rng)

		ra, rb := toRat(a), toRat(b)

		assertRatEq(t, new(big.Rat).Add(ra, rb), a.Add(b), "add", a, b)
		assertRatEq(t, new(big.Rat).Sub(ra, rb), a.Sub(b), "sub", a, b)
		assertRatEq(t, new(big.Rat).Mul(ra, rb), a.Mul(b), "mul", a, b)
	}
}

// assertRatEq asserts a Decimal equals an exact big.Rat oracle value.
func assertRatEq(t *testing.T, want *big.Rat, got decimal.Decimal, op string, a, b decimal.Decimal) {
	t.Helper()
	require.True(t, want.Cmp(toRat(got)) == 0,
		"%s(%s, %s) = %s; oracle = %s", op, a.String(), b.String(), got.String(), want.RatString())
}

// TestMul_NegativeConstructedScale locks the interaction between New's
// negative-scale normalization and Mul's scale-sum rule: New(coef, negScale)
// first scales the coefficient up to scale 0, so the product's scale is the sum
// of the ALREADY-normalized scales — a porting trap for anyone assuming the
// negative scale survives into Mul (as it would in shopspring's exp model).
func TestMul_NegativeConstructedScale(t *testing.T) {
	a := decimal.New(1234, -5) // 1234 * 10^5 = 123400000 at scale 0
	assert.Equal(t, int32(0), a.Scale())
	assert.Equal(t, "123400000", a.String())

	b := decimal.New(45, 1) // 4.5 at scale 1
	prod := a.Mul(b)
	// scale = 0 + 1 = 1; 123400000 * 4.5 = 555300000.0
	assert.Equal(t, "555300000.0", prod.String())
	assert.Equal(t, int32(1), prod.Scale())
}
