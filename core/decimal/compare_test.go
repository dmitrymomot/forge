package decimal_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/core/decimal"
)

func TestCmp_Equal_ScaleNormalized(t *testing.T) {
	tests := []struct {
		a, b    string
		wantCmp int
		wantEq  bool
	}{
		{"2.50", "2.5", 0, true},
		{"2.5", "2.50", 0, true},
		{"0", "0.00", 0, true},
		{"1.1", "1.10", 0, true},
		{"1.10", "1.2", -1, false},
		{"1.2", "1.10", 1, false},
		{"-1.5", "-1.50", 0, true},
		{"-1.5", "1.5", -1, false},
		{"1.5", "-1.5", 1, false},
		{"-2", "-3", 1, false},
		{"100", "99.99", 1, false},
		// crosses int64 during alignment
		{"9223372036854775807.0", "9223372036854775807", 0, true},
		{"9223372036854775808", "9223372036854775807", 1, false},
	}
	for _, tc := range tests {
		t.Run(tc.a+"_vs_"+tc.b, func(t *testing.T) {
			a := decimal.MustParse(tc.a)
			b := decimal.MustParse(tc.b)
			assert.Equal(t, tc.wantCmp, a.Cmp(b))
			assert.Equal(t, tc.wantEq, a.Equal(b))
		})
	}
}

func TestNeg_Abs(t *testing.T) {
	tests := []struct {
		in      string
		wantNeg string
		wantAbs string
	}{
		{"2.50", "-2.50", "2.50"},
		{"-2.50", "2.50", "2.50"},
		{"0.00", "0.00", "0.00"},
		{"9223372036854775808", "-9223372036854775808", "9223372036854775808"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			d := decimal.MustParse(tc.in)
			assert.Equal(t, tc.wantNeg, d.Neg().String())
			assert.Equal(t, tc.wantAbs, d.Abs().String())
			// scale is preserved by Neg/Abs
			assert.Equal(t, d.Scale(), d.Neg().Scale())
			assert.Equal(t, d.Scale(), d.Abs().Scale())
		})
	}
}

func TestNeg_MinInt64PromotesToBig(t *testing.T) {
	// -(-9223372036854775808) has no int64 representation, so Neg must promote.
	d := decimal.New(-9223372036854775808, 0)
	assert.Equal(t, "9223372036854775808", d.Neg().String())
}
