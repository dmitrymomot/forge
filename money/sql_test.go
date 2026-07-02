package money_test

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/money"
)

// Compile-time proof that the SQL interfaces are satisfied.
var (
	_ driver.Valuer = money.Money{}
	_ sql.Scanner   = (*money.Money)(nil)
	_ driver.Valuer = money.NullMoney{}
	_ sql.Scanner   = (*money.NullMoney)(nil)
)

func TestMoneyValue(t *testing.T) {
	v, err := money.FromMinor(150, money.USD).Value()
	require.NoError(t, err)
	assert.Equal(t, "1.50 USD", v)

	// Full precision is preserved (not rounded to minor units).
	hp, err := money.Parse("1.23456", money.USD)
	require.NoError(t, err)
	v, err = hp.Value()
	require.NoError(t, err)
	assert.Equal(t, "1.23456 USD", v)

	// A Money with no currency cannot be serialized.
	_, err = money.Money{}.Value()
	require.Error(t, err)
	assert.True(t, errors.Is(err, money.ErrScan))
}

func TestMoneyScan(t *testing.T) {
	var m money.Money

	require.NoError(t, m.Scan("1.50 USD"))
	assert.Equal(t, "1.50", m.Amount().String())
	assert.Equal(t, money.USD, m.Currency())

	require.NoError(t, m.Scan([]byte("500 JPY")))
	assert.Equal(t, money.JPY, m.Currency())
	assert.Equal(t, int64(500), m.Minor())

	require.NoError(t, m.Scan("1.23456 USD")) // full precision text
	assert.Equal(t, "1.23456", m.Amount().String())
}

func TestMoneyScan_Errors(t *testing.T) {
	var m money.Money

	require.True(t, errors.Is(m.Scan("1.50"), money.ErrScan), "missing currency token")
	require.True(t, errors.Is(m.Scan("1.50 ZZZ"), money.ErrUnknownCurrency))
	require.Error(t, m.Scan("abc USD")) // malformed amount
	require.True(t, errors.Is(m.Scan(nil), money.ErrScan), "nil (use NullMoney)")
	require.True(t, errors.Is(m.Scan(1.5), money.ErrScan), "unsupported type")
}

func TestMoneyScan_Value_RoundTrip(t *testing.T) {
	orig, err := money.Parse("1234.5678", money.USD)
	require.NoError(t, err)
	v, err := orig.Value()
	require.NoError(t, err)

	var back money.Money
	require.NoError(t, back.Scan(v))
	assert.Equal(t, orig.Amount().String(), back.Amount().String())
	assert.Equal(t, orig.Currency(), back.Currency())
}

func TestNullMoney(t *testing.T) {
	var n money.NullMoney
	require.NoError(t, n.Scan(nil))
	assert.False(t, n.Valid)
	v, err := n.Value()
	require.NoError(t, err)
	assert.Nil(t, v)

	require.NoError(t, n.Scan("2.50 EUR"))
	assert.True(t, n.Valid)
	assert.Equal(t, money.EUR, n.Money.Currency())
	v, err = n.Value()
	require.NoError(t, err)
	assert.Equal(t, "2.50 EUR", v)
}
