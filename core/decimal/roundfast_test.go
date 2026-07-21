package decimal

// White-box differential tests: roundInt64 must agree with roundBig (the
// retained fallback) on every int64 input it accepts, for every mode.

import (
	"math"
	"math/big"
	"testing"
)

var allModes = []RoundingMode{HalfEven, HalfUp, HalfDown, Up, Down, Ceil, Floor}

// checkRoundAgainstBig asserts the fast path matches roundBig for one input.
func checkRoundAgainstBig(t *testing.T, coef int64, drop int32, mode RoundingMode) {
	t.Helper()
	q, ok := roundInt64(coef, drop, mode)
	if !ok {
		if coef != math.MinInt64 && int(drop) < len(pow10Int64) {
			t.Fatalf("roundInt64(%d, %d, %d) refused an in-range input", coef, drop, mode)
		}
		return
	}
	want := roundBig(big.NewInt(coef), drop, mode)
	if !want.IsInt64() || want.Int64() != q {
		t.Fatalf("roundInt64(%d, %d, mode %d) = %d, roundBig = %s", coef, drop, mode, q, want)
	}
}

func TestRoundInt64MatchesRoundBig(t *testing.T) {
	t.Parallel()

	coefs := []int64{
		0, 1, -1, 4, 5, 6, -4, -5, -6, 9, 10, 11, 15, 25, -15, -25,
		49, 50, 51, -49, -50, -51, 99, 100, 101, 149, 150, 151, 250, -250,
		999_999_999_999_999_999, -999_999_999_999_999_999,
		1_000_000_000_000_000_001, -1_000_000_000_000_000_001,
		math.MaxInt64, math.MaxInt64 - 1, math.MinInt64, math.MinInt64 + 1,
	}
	for _, coef := range coefs {
		for drop := int32(0); drop <= 20; drop++ {
			for _, mode := range allModes {
				checkRoundAgainstBig(t, coef, drop, mode)
			}
		}
	}
}

func TestRoundInt64FallsBackOutOfRange(t *testing.T) {
	t.Parallel()

	if _, ok := roundInt64(math.MinInt64, 1, HalfEven); ok {
		t.Fatal("MinInt64 must defer to the big path")
	}
	if _, ok := roundInt64(1, 19, HalfEven); ok {
		t.Fatal("drop ≥ 19 must defer to the big path")
	}
	if q, ok := roundInt64(math.MinInt64, 0, HalfEven); !ok || q != math.MinInt64 {
		t.Fatalf("drop 0 is a pass-through: got %d, %v", q, ok)
	}
}

func FuzzRoundInt64VsBig(f *testing.F) {
	f.Add(int64(12345), int32(2), int32(HalfEven))
	f.Add(int64(-12345), int32(3), int32(HalfUp))
	f.Add(int64(math.MaxInt64), int32(18), int32(Floor))
	f.Add(int64(math.MinInt64+1), int32(9), int32(Ceil))
	f.Add(int64(250), int32(1), int32(HalfDown))
	f.Fuzz(func(t *testing.T, coef int64, drop int32, mode int32) {
		if drop < 0 || drop > 25 {
			return
		}
		m := RoundingMode(mode)
		if m < HalfEven || m > Floor {
			return
		}
		checkRoundAgainstBig(t, coef, drop, m)
	})
}
