package decimal_test

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/decimal"
)

// Compile-time proof that the SQL interfaces are satisfied.
var (
	_ driver.Valuer = decimal.Decimal{}
	_ sql.Scanner   = (*decimal.Decimal)(nil)
	_ driver.Valuer = decimal.NullDecimal{}
	_ sql.Scanner   = (*decimal.NullDecimal)(nil)
)

func TestValue(t *testing.T) {
	v, err := decimal.MustParse("2.50").Value()
	require.NoError(t, err)
	assert.Equal(t, "2.50", v) // scale-preserving String form
	_, ok := v.(string)
	assert.True(t, ok, "driver.Value must be a string")
}

func TestScan_SupportedTypes(t *testing.T) {
	var d decimal.Decimal

	require.NoError(t, d.Scan("2.50"))
	assert.Equal(t, "2.50", d.String())

	require.NoError(t, d.Scan([]byte("1.2345")))
	assert.Equal(t, "1.2345", d.String())

	require.NoError(t, d.Scan(int64(-42)))
	assert.Equal(t, "-42", d.String())
}

func TestScan_RejectsFloatNilAndJunk(t *testing.T) {
	var d decimal.Decimal

	require.True(t, errors.Is(d.Scan(1.5), decimal.ErrScan), "float64 must be rejected to preserve exactness")
	require.True(t, errors.Is(d.Scan(nil), decimal.ErrScan), "nil must be rejected (use NullDecimal)")
	require.True(t, errors.Is(d.Scan(true), decimal.ErrScan), "unsupported type must be rejected")

	// A malformed string surfaces the parse error, not ErrScan.
	require.True(t, errors.Is(d.Scan("not-a-decimal"), decimal.ErrSyntax))
}

func TestScan_Value_RoundTrip(t *testing.T) {
	orig := decimal.MustParse("-123456789012345678901234567890.5")
	v, err := orig.Value()
	require.NoError(t, err)

	var back decimal.Decimal
	require.NoError(t, back.Scan(v))
	assert.Equal(t, orig.String(), back.String())
	assert.Equal(t, orig.Scale(), back.Scale())
}

func TestNullDecimal(t *testing.T) {
	// Null source → not valid, Value() is nil.
	var n decimal.NullDecimal
	require.NoError(t, n.Scan(nil))
	assert.False(t, n.Valid)
	v, err := n.Value()
	require.NoError(t, err)
	assert.Nil(t, v)

	// Non-null source → valid, round-trips.
	require.NoError(t, n.Scan("9.99"))
	assert.True(t, n.Valid)
	assert.Equal(t, "9.99", n.Decimal.String())
	v, err = n.Value()
	require.NoError(t, err)
	assert.Equal(t, "9.99", v)

	// A bad non-null source still errors.
	var n2 decimal.NullDecimal
	require.Error(t, n2.Scan(1.5))
}
