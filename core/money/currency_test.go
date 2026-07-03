package money_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/core/money"
)

func TestCurrency_ZeroValueFields(t *testing.T) {
	// A Currency built by hand exposes its four documented fields.
	c := money.Currency{Code: "USD", Num: "840", MinorUnits: 2, Symbol: "$"}
	assert.Equal(t, "USD", c.Code)
	assert.Equal(t, "840", c.Num)
	assert.Equal(t, int32(2), c.MinorUnits)
	assert.Equal(t, "$", c.Symbol)
}

func TestSentinels_AreDistinct(t *testing.T) {
	// Sentinels are distinct, non-nil, and carry the "money: " prefix.
	assert.NotNil(t, money.ErrCurrencyMismatch)
	assert.NotNil(t, money.ErrUnknownCurrency)
	assert.NotNil(t, money.ErrInvalidAllocation)
	assert.NotEqual(t, money.ErrCurrencyMismatch, money.ErrUnknownCurrency)
	assert.Contains(t, money.ErrCurrencyMismatch.Error(), "money: ")
	assert.Contains(t, money.ErrUnknownCurrency.Error(), "money: ")
	assert.Contains(t, money.ErrInvalidAllocation.Error(), "money: ")
}
