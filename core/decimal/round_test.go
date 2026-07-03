package decimal_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/core/decimal"
)

func TestRound_AllModes_HalvesAndNonHalves(t *testing.T) {
	// Round to 0 places. Columns are the result string for each mode.
	type row struct {
		in                                                string
		halfEven, halfUp, halfDown, up, down, ceil, floor string
	}
	rows := []row{
		//   in       HalfEven HalfUp HalfDown Up   Down Ceil Floor
		{"0.5", "0", "1", "0", "1", "0", "1", "0"},
		{"1.5", "2", "2", "1", "2", "1", "2", "1"},
		{"2.5", "2", "3", "2", "3", "2", "3", "2"},
		{"-0.5", "0", "-1", "0", "-1", "0", "0", "-1"},
		{"-1.5", "-2", "-2", "-1", "-2", "-1", "-1", "-2"},
		{"-2.5", "-2", "-3", "-2", "-3", "-2", "-2", "-3"},
		{"0.4", "0", "0", "0", "1", "0", "1", "0"},
		{"0.6", "1", "1", "1", "1", "0", "1", "0"},
		{"-0.4", "0", "0", "0", "-1", "0", "0", "-1"},
		{"-0.6", "-1", "-1", "-1", "-1", "0", "0", "-1"},
		{"2.0", "2", "2", "2", "2", "2", "2", "2"},
	}
	for _, r := range rows {
		t.Run(r.in, func(t *testing.T) {
			d := decimal.MustParse(r.in)
			assert.Equal(t, r.halfEven, d.Round(0, decimal.HalfEven).String(), "HalfEven")
			assert.Equal(t, r.halfUp, d.Round(0, decimal.HalfUp).String(), "HalfUp")
			assert.Equal(t, r.halfDown, d.Round(0, decimal.HalfDown).String(), "HalfDown")
			assert.Equal(t, r.up, d.Round(0, decimal.Up).String(), "Up")
			assert.Equal(t, r.down, d.Round(0, decimal.Down).String(), "Down")
			assert.Equal(t, r.ceil, d.Round(0, decimal.Ceil).String(), "Ceil")
			assert.Equal(t, r.floor, d.Round(0, decimal.Floor).String(), "Floor")
		})
	}
}

func TestRound_ToPlaces(t *testing.T) {
	tests := []struct {
		in     string
		places int32
		mode   decimal.RoundingMode
		want   string
	}{
		{"2.345", 2, decimal.HalfEven, "2.34"},
		{"2.355", 2, decimal.HalfEven, "2.36"},
		{"2.345", 2, decimal.HalfUp, "2.35"},
		{"1.005", 2, decimal.HalfUp, "1.01"},
		{"-2.345", 2, decimal.HalfUp, "-2.35"},
		{"1.2345", 2, decimal.Down, "1.23"},
		{"1.2345", 2, decimal.Up, "1.24"},
		{"-1.2345", 2, decimal.Ceil, "-1.23"},
		{"-1.2345", 2, decimal.Floor, "-1.24"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, decimal.MustParse(tc.in).Round(tc.places, tc.mode).String())
		})
	}
}

func TestRescale_IncreaseIsExactPadding(t *testing.T) {
	d := decimal.MustParse("2.5")
	up := d.Rescale(4, decimal.HalfEven)
	assert.Equal(t, "2.5000", up.String())
	assert.Equal(t, int32(4), up.Scale())
	assert.True(t, up.Equal(d))
}

func TestRescale_DecreaseAppliesMode(t *testing.T) {
	d := decimal.MustParse("2.567")
	assert.Equal(t, "2.57", d.Rescale(2, decimal.HalfEven).String())
	assert.Equal(t, "2.56", d.Rescale(2, decimal.Down).String())
}

func TestRound_BigCoefficient(t *testing.T) {
	// A value whose coefficient exceeds int64, rounded down a scale, stays exact.
	d := decimal.MustParse("92233720368547758.085")
	assert.Equal(t, "92233720368547758.09", d.Round(2, decimal.HalfUp).String())
}

// TestRescale_NegativeScaleClamped drives Rescale's scale<0 clamp.
func TestRescale_NegativeScaleClamped(t *testing.T) {
	d := decimal.MustParse("123.456")
	got := d.Rescale(-3, decimal.HalfEven)
	// clamps to scale 0: 123.456 -> 123 (HalfEven, .456 < .5).
	assert.Equal(t, "123", got.String())
	assert.Equal(t, int32(0), got.Scale())
}

// TestRescale_SameScale drives Rescale's early return when scale == d.scale.
func TestRescale_SameScale(t *testing.T) {
	d := decimal.MustParse("2.50")
	got := d.Rescale(2, decimal.HalfEven)
	assert.Equal(t, "2.50", got.String())
	assert.Equal(t, int32(2), got.Scale())
	assert.True(t, d.Equal(got))
}

// TestRescale_UpLargeScale pads a big-backed value to a much larger scale.
func TestRescale_UpLargeScale(t *testing.T) {
	d := decimal.MustParse(bigVal + ".5") // big-backed, scale 1
	got := d.Rescale(40, decimal.HalfEven)
	assert.Equal(t, int32(40), got.Scale())
	assert.Equal(t, bigVal+".5"+repeatZero(39), got.String())
}

// TestRescale_DownBigWithRounding drives roundBig on a big-int-backed value
// (drop > 0) for several modes.
func TestRescale_DownBigWithRounding(t *testing.T) {
	// A big-backed value with fractional digits to be dropped.
	d := decimal.MustParse(bigVal + ".55") // scale 2
	tests := []struct {
		mode decimal.RoundingMode
		want string
	}{
		{decimal.HalfEven, bigVal + ".6"},
		{decimal.HalfUp, bigVal + ".6"},
		{decimal.HalfDown, bigVal + ".5"},
		{decimal.Down, bigVal + ".5"},
		{decimal.Up, bigVal + ".6"},
		{decimal.Ceil, bigVal + ".6"},
		{decimal.Floor, bigVal + ".5"},
	}
	for _, tc := range tests {
		got := d.Rescale(1, tc.mode)
		assert.Equal(t, tc.want, got.String(), "mode=%v", tc.mode)
	}
}

// TestRescale_DownBigNegative exercises roundBig sign handling on big negatives.
func TestRescale_DownBigNegative(t *testing.T) {
	d := decimal.MustParse("-" + bigVal + ".55")
	assert.Equal(t, "-"+bigVal+".6", d.Rescale(1, decimal.Up).String())
	assert.Equal(t, "-"+bigVal+".6", d.Rescale(1, decimal.Floor).String())
	assert.Equal(t, "-"+bigVal+".5", d.Rescale(1, decimal.Ceil).String())
}

// TestRound_BigIntBacked drives Round (alias of Rescale) reducing scale on a
// big-int-backed value with rounding.
func TestRound_BigIntBacked(t *testing.T) {
	d := decimal.MustParse(bigVal + ".999")
	got := d.Round(0, decimal.HalfUp)
	// .999 rounds up: adds 1 to the big coefficient.
	assert.Equal(t, int32(0), got.Scale())
	assert.NotEqual(t, bigVal, got.String())
}

// TestRound_BigCoefficientHalfEvenTieAtInteger pins HalfEven tie resolution at
// the integer boundary on a coefficient far past int64, where the whole big
// integer's parity (via q.Bit(0)) decides keep-vs-increment. TestRescale_
// DownBigWithRounding covers a big tie that rounds UP to even; this pair covers
// both parities at the .5 integer tie, including the round-DOWN-to-even case.
func TestRound_BigCoefficientHalfEvenTieAtInteger(t *testing.T) {
	// Even last integer digit (0): .5 tie stays put (round down to even).
	even := decimal.MustParse("123456789012345678901234567890.5")
	assert.Equal(t, "123456789012345678901234567890", even.Round(0, decimal.HalfEven).String())

	// Odd last integer digit (1): .5 tie rounds up to the even neighbor.
	odd := decimal.MustParse("123456789012345678901234567891.5")
	assert.Equal(t, "123456789012345678901234567892", odd.Round(0, decimal.HalfEven).String())
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		in     string
		places int32
		want   string
	}{
		{"1.789", 2, "1.78"},   // drops toward zero
		{"1.5", 2, "1.5"},      // fewer digits than places: unchanged, no padding
		{"1.999", 0, "1"},      // to integer
		{"-1.789", 2, "-1.78"}, // toward zero (magnitude decreases)
		{"-1.999", 0, "-1"},
		{"123.456", -1, "123"}, // negative places clamps to 0
		{"2", 5, "2"},          // already fewer digits: unchanged, no padding
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, decimal.MustParse(tc.in).Truncate(tc.places).String())
		})
	}
}

func TestFloor_Ceil(t *testing.T) {
	tests := []struct {
		in        string
		wantFloor string
		wantCeil  string
	}{
		{"1.1", "1", "2"},
		{"1.9", "1", "2"},
		{"2.0", "2", "2"},
		{"-1.1", "-2", "-1"},
		{"-1.9", "-2", "-1"},
		{"0", "0", "0"},
		{"5", "5", "5"},
		{"123456789012345678901234567890.5", "123456789012345678901234567890", "123456789012345678901234567891"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			d := decimal.MustParse(tc.in)
			assert.Equal(t, tc.wantFloor, d.Floor().String(), "floor")
			assert.Equal(t, tc.wantCeil, d.Ceil().String(), "ceil")
			assert.Equal(t, int32(0), d.Floor().Scale())
			assert.Equal(t, int32(0), d.Ceil().Scale())
		})
	}
}

func TestRescaleExact(t *testing.T) {
	tests := []struct {
		in        string
		scale     int32
		mode      decimal.RoundingMode
		wantStr   string
		wantExact bool
	}{
		{"2.5", 4, decimal.HalfEven, "2.5000", true},  // padding is lossless
		{"2.50", 1, decimal.HalfEven, "2.5", true},    // dropped digit was zero
		{"2.00", 0, decimal.HalfEven, "2", true},      // dropped zeros
		{"100", 0, decimal.HalfEven, "100", true},     // same scale
		{"2.567", 2, decimal.HalfEven, "2.57", false}, // dropped nonzero digit
		{"2.500", 0, decimal.Down, "2", false},        // dropped .500 (nonzero fraction)
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, exact := decimal.MustParse(tc.in).RescaleExact(tc.scale, tc.mode)
			assert.Equal(t, tc.wantStr, got.String())
			assert.Equal(t, tc.wantExact, exact)
		})
	}
}
