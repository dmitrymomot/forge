package money_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/money"
)

func TestMoneyMarshalJSON(t *testing.T) {
	m := money.FromMinor(150, money.USD)
	b, err := json.Marshal(m)
	require.NoError(t, err)
	assert.JSONEq(t, `{"amount":"1.50","currency":"USD"}`, string(b))

	// Full precision is preserved (not rounded to minor units).
	hp, err := money.Parse("1.23456", money.USD)
	require.NoError(t, err)
	b, err = json.Marshal(hp)
	require.NoError(t, err)
	assert.JSONEq(t, `{"amount":"1.23456","currency":"USD"}`, string(b))

	// Zero-minor-unit currency renders the integer amount.
	b, err = json.Marshal(money.FromMinor(500, money.JPY))
	require.NoError(t, err)
	assert.JSONEq(t, `{"amount":"500","currency":"JPY"}`, string(b))
}

func TestMoneyUnmarshalJSON(t *testing.T) {
	var m money.Money
	require.NoError(t, json.Unmarshal([]byte(`{"amount":"1.50","currency":"USD"}`), &m))
	assert.Equal(t, "1.50", m.Amount().String())
	assert.Equal(t, money.USD, m.Currency())

	// Currency lookup is case-insensitive.
	var m2 money.Money
	require.NoError(t, json.Unmarshal([]byte(`{"amount":"9.99","currency":"eur"}`), &m2))
	assert.Equal(t, money.EUR, m2.Currency())
}

func TestMoneyUnmarshalJSON_Errors(t *testing.T) {
	var m money.Money

	err := json.Unmarshal([]byte(`{"amount":"1.00","currency":"ZZZ"}`), &m)
	require.Error(t, err)
	assert.True(t, errors.Is(err, money.ErrUnknownCurrency))

	require.Error(t, json.Unmarshal([]byte(`{"amount":"abc","currency":"USD"}`), &m))
	require.Error(t, json.Unmarshal([]byte(`"1.50 USD"`), &m)) // not an object
}

func TestMoneyMarshalJSON_EmptyCurrency(t *testing.T) {
	// A Money with no currency cannot round-trip, so MarshalJSON refuses it just
	// like Value — never emitting {"amount":"0","currency":""} that would then
	// fail to unmarshal.
	_, err := json.Marshal(money.Money{})
	require.Error(t, err)
	assert.ErrorIs(t, err, money.ErrScan)
}

func TestMoneyUnmarshalJSON_NullIsNoOp(t *testing.T) {
	m := money.FromMinor(150, money.USD)
	require.NoError(t, m.UnmarshalJSON([]byte("null")))
	assert.Equal(t, "1.50", m.Amount().String()) // unchanged
}

func TestMoneyJSON_RoundTrip(t *testing.T) {
	type invoice struct {
		Total money.Money `json:"total"`
	}
	tot, err := money.Parse("1234.56", money.USD)
	require.NoError(t, err)

	b, err := json.Marshal(invoice{Total: tot})
	require.NoError(t, err)

	var out invoice
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, tot.Amount().String(), out.Total.Amount().String())
	assert.Equal(t, tot.Currency(), out.Total.Currency())
}
