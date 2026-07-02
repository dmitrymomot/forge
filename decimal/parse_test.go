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

func TestParse_ErrorPaths(t *testing.T) {
	bad := []string{
		"",         // empty
		"+",        // lone plus
		"-",        // lone minus
		".",        // lone dot, no digits
		"+.",       // sign + lone dot
		"-.",       // sign + lone dot
		"1.2.3",    // multiple dots
		"12..3",    // adjacent dots
		"abc",      // invalid chars
		"1a2",      // invalid char in int part
		"1.2b",     // invalid char in frac part
		"1 2",      // space
		"0x10",     // hex-like
		"1e5",      // scientific notation not supported
		"1.5e3",    // scientific notation not supported
		"++1",      // double sign
		"--1",      // double sign
		"1,000.00", // thousands separator
	}
	for _, s := range bad {
		t.Run(s, func(t *testing.T) {
			_, err := decimal.Parse(s)
			require.Error(t, err)
			assert.True(t, errors.Is(err, decimal.ErrSyntax), "want ErrSyntax for %q", s)
		})
	}
}

func TestParse_ZeroForms(t *testing.T) {
	tests := []struct {
		in        string
		wantStr   string
		wantScale int32
	}{
		{"0", "0", 0},
		{"000", "0", 0},
		{"+0", "0", 0},
		{"-0", "0", 0},
		{"0.000", "0.000", 3},
		{"-0.000", "0.000", 3}, // negative-zero renders without sign
		{"+0.00", "0.00", 2},
		{"00.00", "0.00", 2},
		{".0", "0.0", 1},
		{"-.00", "0.00", 2},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			d, err := decimal.Parse(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.wantStr, d.String())
			assert.Equal(t, tc.wantScale, d.Scale())
			assert.True(t, d.IsZero())
		})
	}
}

func TestParse_LeadingTrailingZeros(t *testing.T) {
	d, err := decimal.Parse("007.500")
	require.NoError(t, err)
	assert.Equal(t, "7.500", d.String())
	assert.Equal(t, int32(3), d.Scale())
}

// TestParse_OversizedPromotesToBig confirms Parse handles coefficients far past
// int64 range (fromBig big path).
func TestParse_OversizedPromotesToBig(t *testing.T) {
	huge := bigVal + bigVal // ~60 digits
	d, err := decimal.Parse(huge + ".123")
	require.NoError(t, err)
	assert.Equal(t, huge+".123", d.String())
	assert.Equal(t, int32(3), d.Scale())
}

// TestString_NegativeNonZeroBig confirms the neg && !isAllZero branch renders a
// leading minus for a genuinely negative big value.
func TestString_NegativeNonZeroBig(t *testing.T) {
	d := decimal.MustParse("-" + bigVal + ".01")
	assert.Equal(t, "-"+bigVal+".01", d.String())
}

// TestParse_TrailingDotForms pins the grammar boundary for a trailing '.' with
// no fractional digits: it is accepted as the integer at scale 0 (multi-digit
// and signed forms included), while a dot with no digits on either side is a
// syntax error. The round-trip table covers the bare "5." case; this locks the
// signed and multi-digit variants plus the contrasting error path.
func TestParse_TrailingDotForms(t *testing.T) {
	ok := []struct{ in, want string }{
		{"123.", "123"},
		{"-7.", "-7"},
		{"+9.", "9"},
	}
	for _, tc := range ok {
		t.Run("ok_"+tc.in, func(t *testing.T) {
			d, err := decimal.Parse(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.want, d.String())
			assert.Equal(t, int32(0), d.Scale())
		})
	}

	for _, bad := range []string{".", "+.", "-."} {
		t.Run("bad_"+bad, func(t *testing.T) {
			_, err := decimal.Parse(bad)
			require.Error(t, err)
			assert.True(t, errors.Is(err, decimal.ErrSyntax))
		})
	}
}
