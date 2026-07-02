package money_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/money"
)

func TestNeg_Abs(t *testing.T) {
	m := money.FromMinor(150, money.USD) // 1.50
	assert.Equal(t, "-1.50 USD", m.Neg().String())
	assert.Equal(t, money.USD, m.Neg().Currency())

	neg := money.FromMinor(-150, money.USD)
	assert.Equal(t, "1.50 USD", neg.Abs().String())
	assert.Equal(t, "1.50 USD", m.Abs().String()) // already positive
	assert.Equal(t, "0.00 USD", money.FromMinor(0, money.USD).Neg().String())

	// Full precision is preserved (Neg/Abs do not round to minor units).
	hp, err := money.Parse("1.23456", money.USD)
	require.NoError(t, err)
	assert.Equal(t, "-1.23456", hp.Neg().Amount().String())
	assert.Equal(t, "1.23456", hp.Neg().Abs().Amount().String())
}

func TestIsPositive_IsNegative(t *testing.T) {
	pos := money.FromMinor(1, money.USD)
	neg := money.FromMinor(-1, money.USD)
	zero := money.FromMinor(0, money.USD)

	assert.True(t, pos.IsPositive())
	assert.False(t, pos.IsNegative())
	assert.True(t, neg.IsNegative())
	assert.False(t, neg.IsPositive())
	assert.False(t, zero.IsPositive())
	assert.False(t, zero.IsNegative())
}
