package money_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/money"
)

func TestSum(t *testing.T) {
	total, err := money.Sum(
		money.FromMinor(150, money.USD),
		money.FromMinor(275, money.USD),
		money.FromMinor(75, money.USD),
	)
	require.NoError(t, err)
	assert.Equal(t, int64(500), total.Minor())
	assert.Equal(t, money.USD, total.Currency())

	// A single value returns itself (first fixes the currency).
	one, err := money.Sum(money.FromMinor(999, money.USD))
	require.NoError(t, err)
	assert.Equal(t, int64(999), one.Minor())

	// Mixed signs total exactly.
	net, err := money.Sum(
		money.FromMinor(-100, money.USD),
		money.FromMinor(250, money.USD),
		money.FromMinor(-50, money.USD),
	)
	require.NoError(t, err)
	assert.Equal(t, int64(100), net.Minor())
}

func TestSum_PreservesFullPrecision(t *testing.T) {
	a, err := money.Parse("0.001", money.USD)
	require.NoError(t, err)
	b, err := money.Parse("0.002", money.USD)
	require.NoError(t, err)

	sum, err := money.Sum(a, b)
	require.NoError(t, err)
	assert.Equal(t, "0.003", sum.Amount().String()) // no rounding to minor units
}

func TestSum_CurrencyMismatch(t *testing.T) {
	_, err := money.Sum(
		money.FromMinor(150, money.USD),
		money.FromMinor(150, money.EUR),
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, money.ErrCurrencyMismatch))
}
