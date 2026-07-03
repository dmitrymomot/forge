package decimal_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/decimal"
)

func TestFloat64(t *testing.T) {
	tests := []struct {
		in        string
		wantF     float64
		wantExact bool
	}{
		{"0", 0, true},
		{"2.5", 2.5, true},
		{"0.25", 0.25, true},
		{"-1.5", -1.5, true},
		{"100", 100, true},
		{"0.1", 0.1, false}, // 0.1 has no exact binary float representation
		{"0.3", 0.3, false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			f, exact := decimal.MustParse(tc.in).Float64()
			if tc.wantF == 0 {
				assert.Equal(t, 0.0, f)
			} else {
				assert.InEpsilon(t, tc.wantF, f, 1e-12)
			}
			assert.Equal(t, tc.wantExact, exact)
		})
	}
}

func TestMarshalText_RoundTrip(t *testing.T) {
	for _, s := range []string{"0", "2.50", "-0.001", "123456.789", "9223372036854775808"} {
		d := decimal.MustParse(s)
		b, err := d.MarshalText()
		require.NoError(t, err)
		assert.Equal(t, s, string(b))

		var got decimal.Decimal
		require.NoError(t, got.UnmarshalText([]byte(s)))
		assert.Equal(t, d.Scale(), got.Scale())
		assert.Equal(t, d.String(), got.String())
	}
}

func TestUnmarshalText_Invalid(t *testing.T) {
	var d decimal.Decimal
	require.Error(t, d.UnmarshalText([]byte("nope")))
}

func TestJSON_UsesText(t *testing.T) {
	type payload struct {
		Price decimal.Decimal `json:"price"`
	}
	in := payload{Price: decimal.MustParse("19.99")}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	assert.JSONEq(t, `{"price":"19.99"}`, string(b))

	var out payload
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, "19.99", out.Price.String())
}
