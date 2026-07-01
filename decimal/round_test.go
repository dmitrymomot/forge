package decimal_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/decimal"
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
