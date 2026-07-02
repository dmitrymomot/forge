package money_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/money"
)

func TestComparisonHelpers(t *testing.T) {
	a := money.FromMinor(150, money.USD)
	b := money.FromMinor(275, money.USD)
	aEq := money.FromMinor(150, money.USD)

	tests := []struct {
		name string
		fn   func(money.Money) (bool, error)
		arg  money.Money
		want bool
	}{
		{"a<b LessThan", a.LessThan, b, true},
		{"a<b LessThanOrEqual", a.LessThanOrEqual, b, true},
		{"a<b GreaterThan", a.GreaterThan, b, false},
		{"a<b GreaterThanOrEqual", a.GreaterThanOrEqual, b, false},
		{"a<b Equal", a.Equal, b, false},
		{"a==a Equal", a.Equal, aEq, true},
		{"a==a LessThanOrEqual", a.LessThanOrEqual, aEq, true},
		{"a==a GreaterThanOrEqual", a.GreaterThanOrEqual, aEq, true},
		{"a==a LessThan", a.LessThan, aEq, false},
		{"a==a GreaterThan", a.GreaterThan, aEq, false},
		{"b>a GreaterThan", b.GreaterThan, a, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.fn(tc.arg)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestComparisonHelpers_CurrencyMismatch(t *testing.T) {
	a := money.FromMinor(150, money.USD)
	e := money.FromMinor(150, money.EUR)

	// On a currency mismatch every helper must report (false, ErrCurrencyMismatch)
	// — never a spurious true that a caller ignoring the error would trust.
	fns := []func(money.Money) (bool, error){
		a.Equal, a.LessThan, a.LessThanOrEqual, a.GreaterThan, a.GreaterThanOrEqual,
	}
	for _, fn := range fns {
		got, err := fn(e)
		require.Error(t, err)
		assert.True(t, errors.Is(err, money.ErrCurrencyMismatch))
		assert.False(t, got)
	}
}

func TestComparisonHelpers_ScaleNormalized(t *testing.T) {
	a, err := money.Parse("1.5", money.USD)
	require.NoError(t, err)
	b, err := money.Parse("1.50", money.USD)
	require.NoError(t, err)

	eq, err := a.Equal(b)
	require.NoError(t, err)
	assert.True(t, eq, "1.5 and 1.50 are numerically equal despite different scales")
}
