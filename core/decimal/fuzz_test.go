package decimal_test

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/decimal"
)

func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"0", "-0", "2.50", "-.5", "5.", "007", "1.2.3", "1e5", "", "abc",
		"9223372036854775808", "0.000000001", "-123456789.987654321",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		d, err := decimal.Parse(s)
		if err != nil {
			return // rejected input is fine; we only require no panic.
		}
		// Accepted input must round-trip: Parse(d.String()) reproduces the value.
		out := d.String()
		d2, err2 := decimal.Parse(out)
		require.NoErrorf(t, err2, "re-parse of %q (from %q) failed", out, s)
		require.Equalf(t, out, d2.String(), "round-trip mismatch for %q", s)
		require.Equalf(t, d.Scale(), d2.Scale(), "scale drift for %q", s)
	})
}

// FuzzRescaleCmpOracle checks Rescale and Cmp against independent big.Rat /
// big.Int math, so the int64 fast paths and the big path can never drift from
// each other or from the definition.
func FuzzRescaleCmpOracle(f *testing.F) {
	seeds := []struct {
		a, b        int64
		sa, sb, tgt uint8
		mode        uint8
	}{
		{1749755, -4000, 2, 2, 3, 0},
		{-4000005, 17, 5, 2, 0, 3},
		{9223372036854775807, 1, 18, 0, 2, 1},
		{-9223372036854775808, -1, 3, 9, 1, 6},
		{25, 245, 1, 2, 0, 2},
	}
	for _, s := range seeds {
		f.Add(s.a, s.b, s.sa, s.sb, s.tgt, s.mode)
	}
	f.Fuzz(func(t *testing.T, a, b int64, sa, sb, tgt, mode uint8) {
		da := decimal.New(a, int32(sa%25))
		db := decimal.New(b, int32(sb%25))
		m := decimal.RoundingMode(mode % 7)
		scale := int32(tgt % 25)

		got := da.Rescale(scale, m)
		require.Equal(t, scale, got.Scale())
		// Oracle: the rounded value must be within one ulp of the true value,
		// on the correct side for the mode, and exact when no digits drop.
		ulp := new(big.Rat).SetFrac(big.NewInt(1), pow10(scale))
		diff := new(big.Rat).Sub(toRat(got), toRat(da))
		require.Truef(t, diff.Abs(diff).Cmp(ulp) < 0, "rescale(%s,%d,%v)=%s drifted a full unit", da, scale, m, got)
		switch m {
		case decimal.Down:
			require.True(t, toRat(got).Abs(toRat(got)).Cmp(toRat(da).Abs(toRat(da))) <= 0, "Down grew magnitude")
		case decimal.Up:
			require.True(t, toRat(got).Abs(toRat(got)).Cmp(toRat(da).Abs(toRat(da))) >= 0, "Up shrank magnitude")
		case decimal.Ceil:
			require.True(t, toRat(got).Cmp(toRat(da)) >= 0, "Ceil went down")
		case decimal.Floor:
			require.True(t, toRat(got).Cmp(toRat(da)) <= 0, "Floor went up")
		}

		require.Equal(t, toRat(da).Cmp(toRat(db)), da.Cmp(db), "Cmp(%s,%s) disagrees with big.Rat", da, db)
	})
}

func pow10(n int32) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
}

func FuzzAddMulOracle(f *testing.F) {
	seeds := []struct{ a, b string }{
		{"2.50", "0.25"},
		{"-1.5", "2.5"},
		{"9223372036854775807", "1"},
		{"0.1", "0.2"},
		{"-3.14159", "2.71828"},
	}
	for _, s := range seeds {
		f.Add(s.a, s.b)
	}
	f.Fuzz(func(t *testing.T, as, bs string) {
		a, errA := decimal.Parse(as)
		b, errB := decimal.Parse(bs)
		if errA != nil || errB != nil {
			return
		}
		ra, rb := toRat(a), toRat(b)

		gotAdd := a.Add(b)
		require.Truef(t, new(big.Rat).Add(ra, rb).Cmp(toRat(gotAdd)) == 0,
			"add(%q,%q)=%s mismatch", as, bs, gotAdd.String())

		gotMul := a.Mul(b)
		require.Truef(t, new(big.Rat).Mul(ra, rb).Cmp(toRat(gotMul)) == 0,
			"mul(%q,%q)=%s mismatch", as, bs, gotMul.String())
	})
}
