package decimal_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/decimal"
)

func TestNew_ScaleSignZero(t *testing.T) {
	tests := []struct {
		name      string
		coef      int64
		scale     int32
		wantScale int32
		wantSign  int
		wantZero  bool
	}{
		{"positive", 250, 2, 2, 1, false},
		{"negative", -5, 0, 0, -1, false},
		{"zero coef keeps scale", 0, 3, 3, 0, true},
		{"scale zero", 42, 0, 0, 1, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := decimal.New(tc.coef, tc.scale)
			assert.Equal(t, tc.wantScale, d.Scale())
			assert.Equal(t, tc.wantSign, d.Sign())
			assert.Equal(t, tc.wantZero, d.IsZero())
		})
	}
}

func TestNew_NegativeScaleNormalizes(t *testing.T) {
	// New(5, -2) == 5 × 10^2 == 500 at scale 0.
	d := decimal.New(5, -2)
	assert.Equal(t, int32(0), d.Scale())
	assert.Equal(t, 1, d.Sign())
	// 500 is not zero and positive; deeper value assertions land in Task 6.
	assert.False(t, d.IsZero())
}

func TestFromInt(t *testing.T) {
	d := decimal.FromInt(-7)
	assert.Equal(t, int32(0), d.Scale())
	assert.Equal(t, -1, d.Sign())
}

func TestZeroValueAndVar(t *testing.T) {
	var z decimal.Decimal
	assert.True(t, z.IsZero())
	assert.Equal(t, 0, z.Sign())
	assert.Equal(t, int32(0), z.Scale())
	assert.True(t, decimal.Zero.IsZero())
	assert.Equal(t, 0, decimal.Zero.Sign())
}
