package money_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/money"
)

func TestCurrencyByCode_KnownVectors(t *testing.T) {
	tests := []struct {
		code      string
		wantNum   string
		wantMinor int32
	}{
		{"USD", "840", 2},
		{"EUR", "978", 2},
		{"GBP", "826", 2},
		{"JPY", "392", 0}, // zero minor units
		{"CHF", "756", 2},
		{"BHD", "048", 3}, // three minor units
		{"KWD", "414", 3},
		{"CLF", "990", 4}, // four minor units
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			c, ok := money.CurrencyByCode(tt.code)
			require.True(t, ok)
			assert.Equal(t, tt.code, c.Code)
			assert.Equal(t, tt.wantNum, c.Num)
			assert.Equal(t, tt.wantMinor, c.MinorUnits)
			assert.NotEmpty(t, c.Symbol, "Symbol must never be empty; falls back to Code")
		})
	}
}

func TestCurrencyByCode_CaseInsensitiveAndUnknown(t *testing.T) {
	c, ok := money.CurrencyByCode("usd")
	require.True(t, ok, "lookup is case-insensitive")
	assert.Equal(t, "USD", c.Code)

	_, ok = money.CurrencyByCode("ZZZ")
	assert.False(t, ok)

	_, ok = money.CurrencyByCode("")
	assert.False(t, ok)
}

func TestPackageVars_MatchRegistry(t *testing.T) {
	// The exported vars are the same values the registry returns.
	assert.Equal(t, "USD", money.USD.Code)
	assert.Equal(t, int32(0), money.JPY.MinorUnits)
	got, ok := money.CurrencyByCode("EUR")
	require.True(t, ok)
	assert.Equal(t, money.EUR, got)
}

func TestRegistry_AllEntriesWellFormed(t *testing.T) {
	// Every bundled entry has a 3-letter uppercase code, a numeric Num, a
	// non-negative MinorUnits, and a non-empty Symbol.
	for _, code := range []string{"USD", "EUR", "GBP", "JPY", "BHD", "CLF"} {
		c, ok := money.CurrencyByCode(code)
		require.True(t, ok)
		assert.Len(t, c.Code, 3)
		assert.Equal(t, strings.ToUpper(c.Code), c.Code)
		assert.GreaterOrEqual(t, c.MinorUnits, int32(0))
		assert.NotEmpty(t, c.Num)
		assert.NotEmpty(t, c.Symbol)
	}
}
