package decimal_test

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/decimal"
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
