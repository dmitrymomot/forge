package decimal_test

import (
	"errors"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/decimal"
)

func TestQuoRem_KnownAnswers(t *testing.T) {
	tests := []struct {
		a, b  string
		wantQ string
		wantR string
	}{
		{"7", "2", "3", "1"},
		{"-7", "2", "-3", "-1"}, // sign(r) follows the dividend
		{"7", "-2", "-3", "1"},
		{"-7", "-2", "3", "-1"},
		{"10", "5", "2", "0"},
		{"1", "3", "0", "1"},
		{"5.5", "2", "2", "1.5"},   // 5.5 = 2*2 + 1.5
		{"10", "0.3", "33", "0.1"}, // 10 = 0.3*33 + 0.1
	}
	for _, tc := range tests {
		t.Run(tc.a+"_qr_"+tc.b, func(t *testing.T) {
			a := decimal.MustParse(tc.a)
			b := decimal.MustParse(tc.b)
			q, r, err := a.QuoRem(b)
			require.NoError(t, err)
			assert.Equal(t, tc.wantQ, q.String(), "quotient")
			assert.Truef(t, decimal.MustParse(tc.wantR).Equal(r), "remainder: got %s want %s", r.String(), tc.wantR)
			assert.True(t, q.IsInteger(), "quotient must be integer")
			// Defining identity: a == b*q + r, exactly.
			assert.Truef(t, a.Equal(b.Mul(q).Add(r)), "b*q+r must equal a: a=%s b=%s q=%s r=%s", tc.a, tc.b, q, r)
		})
	}
}

func TestMod_KnownAnswers(t *testing.T) {
	tests := []struct{ a, b, want string }{
		{"7", "2", "1"},
		{"-7", "2", "-1"},
		{"10", "5", "0"},
		{"5.5", "2", "1.5"},
	}
	for _, tc := range tests {
		t.Run(tc.a+"_mod_"+tc.b, func(t *testing.T) {
			r, err := decimal.MustParse(tc.a).Mod(decimal.MustParse(tc.b))
			require.NoError(t, err)
			assert.True(t, decimal.MustParse(tc.want).Equal(r))
		})
	}
}

func TestQuoRem_Mod_ByZero(t *testing.T) {
	_, _, err := decimal.FromInt(1).QuoRem(decimal.Zero)
	require.Error(t, err)
	assert.True(t, errors.Is(err, decimal.ErrDivByZero))

	_, err = decimal.FromInt(1).Mod(decimal.MustParse("0.00"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, decimal.ErrDivByZero))
}

// TestQuoRem_Invariants checks the truncated-division contract over randomized
// operands spanning the int64 fast path and the big.Int slow path: exact
// reconstruction, an integer quotient, a remainder strictly smaller in magnitude
// than the divisor, and a remainder sign matching the dividend.
func TestQuoRem_Invariants(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 11))
	for range 10000 {
		a := randDecimal(rng)
		b := randDecimal(rng)
		if b.IsZero() {
			continue
		}
		q, r, err := a.QuoRem(b)
		require.NoError(t, err)
		require.Truef(t, a.Equal(b.Mul(q).Add(r)), "a=%s b=%s q=%s r=%s", a, b, q, r)
		require.Truef(t, q.IsInteger(), "quotient must be integer: %s", q)
		require.Truef(t, r.Abs().Cmp(b.Abs()) < 0, "|r| < |b| failed: a=%s b=%s r=%s", a, b, r)
		if !r.IsZero() {
			require.Equalf(t, a.Sign(), r.Sign(), "sign(r) must match sign(a): a=%s r=%s", a, r)
		}
	}
}
