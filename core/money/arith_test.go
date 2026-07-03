package money_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/decimal"
	"github.com/dmitrymomot/forge/core/money"
)

func TestAdd_SameCurrency(t *testing.T) {
	a := money.FromMinor(150, money.USD) // 1.50
	b := money.FromMinor(275, money.USD) // 2.75
	sum, err := a.Add(b)
	require.NoError(t, err)
	assert.Equal(t, int64(425), sum.Minor()) // 4.25
	assert.Equal(t, money.USD, sum.Currency())
}

func TestAdd_CurrencyMismatch(t *testing.T) {
	a := money.FromMinor(150, money.USD)
	b := money.FromMinor(150, money.EUR)
	_, err := a.Add(b)
	require.Error(t, err)
	assert.True(t, errors.Is(err, money.ErrCurrencyMismatch))
}

func TestSub(t *testing.T) {
	a := money.FromMinor(500, money.USD) // 5.00
	b := money.FromMinor(175, money.USD) // 1.75
	diff, err := a.Sub(b)
	require.NoError(t, err)
	assert.Equal(t, int64(325), diff.Minor()) // 3.25

	_, err = a.Sub(money.FromMinor(1, money.EUR))
	assert.True(t, errors.Is(err, money.ErrCurrencyMismatch))
}

func TestMul_ExactNoRounding(t *testing.T) {
	// 10.00 USD × 0.0825 (tax) = 0.825000 exact; caller Rounds for settlement.
	price := money.FromMinor(1000, money.USD) // 10.00
	tax := price.Mul(decimal.New(825, 4))     // 0.0825
	assert.Equal(t, "0.825000", tax.Amount().String())
	assert.Equal(t, money.USD, tax.Currency())
	// Rounding to minor units happens on demand. HalfEven of 0.825 → 0.82 (even).
	assert.Equal(t, int64(82), tax.Minor())
}

func TestRound_ToMinorUnits(t *testing.T) {
	m, err := money.Parse("1.23456", money.USD)
	require.NoError(t, err)
	r := m.Round(decimal.HalfEven)
	assert.Equal(t, "1.23", r.Amount().String())
	assert.Equal(t, money.USD, r.Currency())

	// Down mode truncates.
	d := m.Round(decimal.Down)
	assert.Equal(t, "1.23", d.Amount().String())

	// Up mode rounds away from zero.
	u := m.Round(decimal.Up)
	assert.Equal(t, "1.24", u.Amount().String())
}

func TestCmp(t *testing.T) {
	a := money.FromMinor(150, money.USD)
	b := money.FromMinor(275, money.USD)
	c, err := a.Cmp(b)
	require.NoError(t, err)
	assert.Equal(t, -1, c)

	c, err = b.Cmp(a)
	require.NoError(t, err)
	assert.Equal(t, 1, c)

	c, err = a.Cmp(money.FromMinor(150, money.USD))
	require.NoError(t, err)
	assert.Equal(t, 0, c)

	_, err = a.Cmp(money.FromMinor(150, money.EUR))
	assert.True(t, errors.Is(err, money.ErrCurrencyMismatch))
}

func TestString(t *testing.T) {
	assert.Equal(t, "1.50 USD", money.FromMinor(150, money.USD).String())
	assert.Equal(t, "500 JPY", money.FromMinor(500, money.JPY).String())
	assert.Equal(t, "1.234 BHD", money.FromMinor(1234, money.BHD).String())
	assert.Equal(t, "-1.50 USD", money.FromMinor(-150, money.USD).String())
}

// TestString_RoundsFullPrecision exercises String's round-to-MinorUnits path on
// amounts carrying MORE precision than the currency scale — the existing
// TestString only feeds FromMinor values already at MinorUnits. String rounds
// HalfEven (banker's), so ties resolve to the even minor unit.
func TestString_RoundsFullPrecision(t *testing.T) {
	tests := []struct {
		in   string
		cur  money.Currency
		want string
	}{
		{"1.005", money.USD, "1.00 USD"},   // tie -> even (down)
		{"1.015", money.USD, "1.02 USD"},   // tie -> even (up); proves HalfEven not HalfUp
		{"1.2349", money.BHD, "1.235 BHD"}, // 3 minor units, .2349 -> .235
		{"-1.005", money.USD, "-1.00 USD"}, // negative tie keeps magnitude and sign
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			m, err := money.Parse(tc.in, tc.cur)
			require.NoError(t, err)
			assert.Equal(t, tc.want, m.String())
		})
	}
}

// TestString_NegativeRoundsToZeroSuppressesSign guards a presentation edge: a
// small negative amount that rounds to zero at MinorUnits must render WITHOUT a
// minus sign ("0.00 USD", never "-0.00 USD"), inheriting decimal's negative-zero
// suppression through the round + rescale in String.
func TestString_NegativeRoundsToZeroSuppressesSign(t *testing.T) {
	m, err := money.Parse("-0.001", money.USD)
	require.NoError(t, err)
	assert.Equal(t, "0.00 USD", m.String())
}

// TestRound_DirectedModes covers the Ceil and Floor rounding modes threaded
// through Money.Round, including a negative amount. TestRound_ToMinorUnits only
// covers HalfEven/Down/Up on a positive value, so the sign-aware directed modes
// are otherwise untested at the money layer.
func TestRound_DirectedModes(t *testing.T) {
	tests := []struct {
		in   string
		mode decimal.RoundingMode
		want string
	}{
		{"2.555", decimal.Ceil, "2.56"},    // toward +inf
		{"2.554", decimal.Floor, "2.55"},   // toward -inf drops the 4
		{"-2.551", decimal.Floor, "-2.56"}, // Floor on negative moves away from zero
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			m, err := money.Parse(tc.in, money.USD)
			require.NoError(t, err)
			assert.Equal(t, tc.want, m.Round(tc.mode).Amount().String())
		})
	}
}

// TestMul_ByIntegerQuantity documents the quantity x unit-price idiom
// (Mul(decimal.FromInt(n))): the scale-sum rule keeps the price's scale and no
// rounding occurs until settlement.
func TestMul_ByIntegerQuantity(t *testing.T) {
	m, err := money.Parse("10.00", money.USD)
	require.NoError(t, err)
	line := m.Mul(decimal.FromInt(3))
	assert.Equal(t, "30.00", line.Amount().String()) // scale 2 + 0 = 2, exact
	assert.Equal(t, "30.00 USD", line.String())
	assert.Equal(t, int64(3000), line.Minor())
}
