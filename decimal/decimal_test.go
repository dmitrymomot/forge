package decimal_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/decimal"
)

// bigVal is a coefficient that overflows int64, forcing the *big.Int slow path.
// Shared across the package_test files (all package decimal_test).
const bigVal = "123456789012345678901234567890"

// repeatZero returns a string of n '0' runes, used to build expected big-value
// renderings without importing strings into every test file.
func repeatZero(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = '0'
	}
	return string(b)
}

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

// TestMulPow10_Int64Overflow drives mulPow10's int64->big promotion via New with
// a negative scale whose scale-up overflows int64.
func TestMulPow10_Int64Overflow(t *testing.T) {
	// coef * 10^30 overflows int64, forcing the big path in mulPow10.
	d := decimal.New(999999999, -30)
	// 999999999 * 10^30
	want := "999999999" + repeatZero(30)
	assert.Equal(t, want, d.String())
	assert.Equal(t, int32(0), d.Scale())
}

// TestMulPow10_AlreadyBig drives mulPow10's big path when the receiver is already
// big-backed (d.big != nil), via Rescale-up on a big value.
func TestMulPow10_AlreadyBig(t *testing.T) {
	d := decimal.MustParse(bigVal) // big-backed, scale 0
	got := d.Rescale(5, decimal.HalfEven)
	assert.Equal(t, bigVal+".00000", got.String())
	assert.Equal(t, int32(5), got.Scale())
}

// TestNew_NegativeScaleSmall keeps mulPow10 on the int64 fast path (no overflow).
func TestNew_NegativeScaleSmall(t *testing.T) {
	d := decimal.New(5, -2) // 5 * 10^2 = 500
	assert.Equal(t, "500", d.String())
	assert.Equal(t, int32(0), d.Scale())
}
