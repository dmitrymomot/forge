package decimal_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/decimal"
)

func TestParse_String_RoundTrip(t *testing.T) {
	tests := []struct {
		in        string
		wantScale int32
		wantStr   string // String() output; preserves scale
	}{
		{"0", 0, "0"},
		{"-0", 0, "0"},
		{"123", 0, "123"},
		{"-123", 0, "-123"},
		{"2.50", 2, "2.50"},
		{"2.5", 1, "2.5"},
		{"0.001", 3, "0.001"},
		{"-0.001", 3, "-0.001"},
		{"+7.25", 2, "7.25"},
		{"1000000.000001", 6, "1000000.000001"},
		{".5", 1, "0.5"},
		{"-.5", 1, "-0.5"},
		{"5.", 0, "5"},
		{"007", 0, "7"},
		{"00.50", 2, "0.50"},
		// crosses int64 so exercises the big path
		{"9223372036854775808", 0, "9223372036854775808"},
		{"92233720368547758080.00", 2, "92233720368547758080.00"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			d, err := decimal.Parse(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.wantScale, d.Scale())
			assert.Equal(t, tc.wantStr, d.String())

			// Round-trip: Parse(d.String()) reproduces scale + string.
			d2, err := decimal.Parse(d.String())
			require.NoError(t, err)
			assert.Equal(t, d.Scale(), d2.Scale())
			assert.Equal(t, d.String(), d2.String())
		})
	}
}

func TestParse_Errors(t *testing.T) {
	bad := []string{
		"", "   ", "abc", "1.2.3", "1e5", "1E5", "1.5e3",
		"++1", "1..2", "0x10", "1 000", "--1", ".", "-", "+", "1.2 ",
		"nan", "inf", "0b1", "1,000",
	}
	for _, s := range bad {
		t.Run("bad_"+s, func(t *testing.T) {
			_, err := decimal.Parse(s)
			require.Error(t, err)
			assert.True(t, errors.Is(err, decimal.ErrSyntax), "want ErrSyntax for %q", s)
		})
	}
}

func TestMustParse(t *testing.T) {
	assert.Equal(t, "3.14", decimal.MustParse("3.14").String())
	assert.Panics(t, func() { decimal.MustParse("nope") })
}
