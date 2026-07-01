package money_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/decimal"
	"github.com/dmitrymomot/forge/money"
)

func TestNew_AndAccessors(t *testing.T) {
	m := money.New(decimal.New(150, 2), money.USD) // 1.50
	assert.Equal(t, money.USD, m.Currency())
	assert.Equal(t, "1.50", m.Amount().String())
}

func TestFromMinor(t *testing.T) {
	tests := []struct {
		name    string
		cur     money.Currency
		wantAmt string
		units   int64
	}{
		{"usd_150", money.USD, "1.50", 150},
		{"usd_zero", money.USD, "0.00", 0},
		{"usd_negative", money.USD, "-1.50", -150},
		{"jpy_500", money.JPY, "500", 500},     // 0 minor units
		{"bhd_1234", money.BHD, "1.234", 1234}, // 3 minor units
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := money.FromMinor(tt.units, tt.cur)
			assert.Equal(t, tt.wantAmt, m.Amount().String())
			assert.Equal(t, tt.units, m.Minor())
		})
	}
}

func TestParse(t *testing.T) {
	m, err := money.Parse("1.50", money.USD)
	require.NoError(t, err)
	assert.Equal(t, "1.50", m.Amount().String())
	assert.Equal(t, money.USD, m.Currency())

	// A high-precision amount is stored exactly (rounding is deferred to Minor).
	m2, err := money.Parse("1.23456", money.USD)
	require.NoError(t, err)
	assert.Equal(t, "1.23456", m2.Amount().String())

	_, err = money.Parse("not-a-number", money.USD)
	require.Error(t, err)
}

func TestMinor_RoundsToMinorUnits(t *testing.T) {
	// Minor rounds the amount to the currency's MinorUnits (banker's rounding),
	// then reports it as an integer count of minor units.
	m, err := money.Parse("1.005", money.USD) // 3 dp, 2 minor units
	require.NoError(t, err)
	// HalfEven on 1.005 → 1.00 (0 is even) → 100 minor units.
	assert.Equal(t, int64(100), m.Minor())

	m2, err := money.Parse("2.675", money.USD)
	require.NoError(t, err)
	// HalfEven on 2.675 → 2.68 (8 is even) → 268 minor units.
	assert.Equal(t, int64(268), m2.Minor())
}

func TestIsZero(t *testing.T) {
	assert.True(t, money.FromMinor(0, money.USD).IsZero())
	assert.False(t, money.FromMinor(1, money.USD).IsZero())
}
