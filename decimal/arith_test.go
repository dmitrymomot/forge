package decimal_test

import (
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
