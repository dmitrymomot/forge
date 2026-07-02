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

func TestMinor_Branches(t *testing.T) {
	// Exercise every reachable path of Minor: zero, negative, currencies with
	// 0 / 2 / 3 / 4 minor units, values with and without a fractional minor part,
	// and large magnitudes near the int64 range.
	tests := []struct {
		name string
		in   string
		cur  money.Currency
		want int64
	}{
		{"usd_zero", "0", money.USD, 0},
		{"usd_zero_fractional", "0.00", money.USD, 0},
		{"usd_positive_exact", "1.50", money.USD, 150},
		{"usd_negative_exact", "-1.50", money.USD, -150},
		{"usd_positive_rounds_up", "1.239", money.USD, 124},
		{"usd_negative_rounds_up", "-1.239", money.USD, -124},
		{"usd_halfeven_down_to_even", "1.005", money.USD, 100}, // 1.00 (even)
		{"usd_halfeven_up_to_even", "2.675", money.USD, 268},   // 2.68 (even)
		{"jpy_zero_minor_units", "500", money.JPY, 500},        // 0 minor units
		{"jpy_negative_no_fraction", "-500", money.JPY, -500},
		{"jpy_rounds_to_whole", "500.4", money.JPY, 500}, // truncates fractional
		{"bhd_three_minor_units", "1.234", money.BHD, 1234},
		{"bhd_negative_three", "-1.234", money.BHD, -1234},
		{"kwd_three_minor_units", "12.345", money.KWD, 12345},
		{"clf_four_minor_units", "1.2345", money.CLF, 12345}, // 4 minor units
		{"usd_large_value", "999999999.99", money.USD, 99999999999},
		{"usd_large_negative", "-999999999.99", money.USD, -99999999999},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := money.Parse(tt.in, tt.cur)
			require.NoError(t, err)
			assert.Equal(t, tt.want, m.Minor())
		})
	}
}

func TestIsZero(t *testing.T) {
	assert.True(t, money.FromMinor(0, money.USD).IsZero())
	assert.False(t, money.FromMinor(1, money.USD).IsZero())
}
