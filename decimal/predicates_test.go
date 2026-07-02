package decimal_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/decimal"
)

func TestIsPositive_IsNegative(t *testing.T) {
	tests := []struct {
		in      string
		wantPos bool
		wantNeg bool
	}{
		{"0", false, false},
		{"0.00", false, false},
		{"1", true, false},
		{"0.001", true, false},
		{"-1", false, true},
		{"-0.001", false, true},
		{"9223372036854775808", true, false},         // big positive
		{"-9223372036854775808", false, true},        // int64 min (fast path)
		{"-99999999999999999999999999", false, true}, // big negative
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			d := decimal.MustParse(tc.in)
			assert.Equal(t, tc.wantPos, d.IsPositive())
			assert.Equal(t, tc.wantNeg, d.IsNegative())
		})
	}
}

func TestIsInteger(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"0", true},
		{"0.00", true}, // zero is an integer at any scale
		{"12", true},
		{"12.0", true}, // trailing-zero fraction is still whole
		{"12.00", true},
		{"-12.000", true},
		{"100", true},
		{"12.5", false},
		{"0.1", false},
		{"-0.001", false},
		{"123456789012345678901234567890", true}, // big integer
		{"123456789012345678901234567890.0", true},  // big with zero fraction
		{"123456789012345678901234567890.5", false}, // big with nonzero fraction
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, decimal.MustParse(tc.in).IsInteger())
		})
	}

	// Scale so large that 10^scale overflows int64: a nonzero int64 coefficient
	// cannot be a multiple of it, so the value is not an integer; zero still is.
	assert.False(t, decimal.New(5, 19).IsInteger())
	assert.True(t, decimal.New(0, 19).IsInteger())
}
