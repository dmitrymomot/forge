package money_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/decimal"
	"github.com/dmitrymomot/forge/money"
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
