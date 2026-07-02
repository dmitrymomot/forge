package decimal_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/decimal"
)

func TestMarshalJSON_QuotedString(t *testing.T) {
	for _, s := range []string{"0", "2.50", "-0.001", "19.99", "9223372036854775808", "-123456789012345678901234567890.5"} {
		d := decimal.MustParse(s)
		b, err := d.MarshalJSON()
		require.NoError(t, err)
		assert.Equal(t, `"`+s+`"`, string(b), "must emit a quoted string preserving scale")
	}
}

func TestUnmarshalJSON_StringAndNumber(t *testing.T) {
	// Quoted-string form (what MarshalJSON emits).
	var d decimal.Decimal
	require.NoError(t, json.Unmarshal([]byte(`"2.50"`), &d))
	assert.Equal(t, "2.50", d.String())
	assert.Equal(t, int32(2), d.Scale())

	// Bare JSON number is accepted and keeps its scale (never routed via float64).
	var d2 decimal.Decimal
	require.NoError(t, json.Unmarshal([]byte(`19.99`), &d2))
	assert.Equal(t, "19.99", d2.String())

	// A high-precision bare number keeps every digit — the whole point of not
	// going through float64.
	var d3 decimal.Decimal
	require.NoError(t, json.Unmarshal([]byte(`0.12345678901234567890`), &d3))
	assert.Equal(t, "0.12345678901234567890", d3.String())
}

func TestUnmarshalJSON_NullIsNoOp(t *testing.T) {
	d := decimal.MustParse("7.5")
	require.NoError(t, d.UnmarshalJSON([]byte("null")))
	assert.Equal(t, "7.5", d.String()) // receiver unchanged
}

func TestUnmarshalJSON_Invalid(t *testing.T) {
	// Empty string, scientific notation, and malformed values are all rejected.
	for _, s := range []string{`"abc"`, `"1e5"`, `""`, `"1.2.3"`, `1e5`} {
		var d decimal.Decimal
		require.Errorf(t, json.Unmarshal([]byte(s), &d), "input %q should be rejected", s)
	}
}

func TestJSON_RoundTripInStruct(t *testing.T) {
	type payload struct {
		Price decimal.Decimal `json:"price"`
	}
	in := payload{Price: decimal.MustParse("1234.5678")}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	assert.JSONEq(t, `{"price":"1234.5678"}`, string(b))

	var out payload
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, in.Price.Scale(), out.Price.Scale())
	assert.Equal(t, in.Price.String(), out.Price.String())
}
