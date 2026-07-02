package money_test

import (
	"errors"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/money"
)

func TestAllocate_KnownVectors(t *testing.T) {
	// 0.05 USD (5 cents) allocated 1:1:1 → 2,2,1 (largest-remainder), sums to 5.
	m := money.FromMinor(5, money.USD)
	parts, err := m.Allocate(1, 1, 1)
	require.NoError(t, err)
	require.Len(t, parts, 3)
	assert.Equal(t, int64(2), parts[0].Minor())
	assert.Equal(t, int64(2), parts[1].Minor())
	assert.Equal(t, int64(1), parts[2].Minor())

	// Weighted 1.00 USD (100 cents) 70:30 → 70,30.
	m2 := money.FromMinor(100, money.USD)
	p2, err := m2.Allocate(70, 30)
	require.NoError(t, err)
	assert.Equal(t, int64(70), p2[0].Minor())
	assert.Equal(t, int64(30), p2[1].Minor())

	// 0.10 USD 1:1:1 → 4,3,3 sums to 10.
	m3 := money.FromMinor(10, money.USD)
	p3, err := m3.Allocate(1, 1, 1)
	require.NoError(t, err)
	assert.Equal(t, int64(4), p3[0].Minor())
	assert.Equal(t, int64(3), p3[1].Minor())
	assert.Equal(t, int64(3), p3[2].Minor())
}

func TestAllocate_InvalidRatios(t *testing.T) {
	m := money.FromMinor(100, money.USD)

	_, err := m.Allocate()
	assert.True(t, errors.Is(err, money.ErrInvalidAllocation))

	_, err = m.Allocate(0, 0)
	assert.True(t, errors.Is(err, money.ErrInvalidAllocation))

	_, err = m.Allocate(-1, 2) // sum > 0 but mixed; ratios must be usable
	// A negative ratio is invalid; sum-with-negatives is rejected.
	assert.True(t, errors.Is(err, money.ErrInvalidAllocation))
}

func TestAllocate_PreservesCurrency(t *testing.T) {
	m := money.FromMinor(500, money.JPY)
	parts, err := m.Allocate(1, 1)
	require.NoError(t, err)
	for _, p := range parts {
		assert.Equal(t, money.JPY, p.Currency())
	}
}

func TestSplit(t *testing.T) {
	// 0.10 USD split into 3 → 4,3,3.
	m := money.FromMinor(10, money.USD)
	parts, err := m.Split(3)
	require.NoError(t, err)
	require.Len(t, parts, 3)
	assert.Equal(t, int64(4), parts[0].Minor())
	assert.Equal(t, int64(3), parts[1].Minor())
	assert.Equal(t, int64(3), parts[2].Minor())

	_, err = m.Split(0)
	assert.True(t, errors.Is(err, money.ErrInvalidAllocation))

	_, err = m.Split(-1)
	assert.True(t, errors.Is(err, money.ErrInvalidAllocation))
}

// TestAllocate_SumInvariant is the headline property: for randomized amounts and
// ratio sets (both signs of amount), the parts always sum back to the total.
func TestAllocate_SumInvariant(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for range 5000 {
		units := int64(rng.Intn(2_000_001) - 1_000_000) // -1e6 .. 1e6 cents
		m := money.FromMinor(units, money.USD)

		n := rng.Intn(8) + 1
		ratios := make([]int, n)
		sum := 0
		for i := range ratios {
			ratios[i] = rng.Intn(10) + 1 // 1..10, always positive
			sum += ratios[i]
		}

		parts, err := m.Allocate(ratios...)
		require.NoError(t, err)
		require.Len(t, parts, n)

		var got int64
		for _, p := range parts {
			got += p.Minor()
		}
		assert.Equalf(t, units, got, "sum(parts) must equal total; units=%d ratios=%v", units, ratios)
	}
}

func TestSplit_SumInvariant(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for range 5000 {
		units := int64(rng.Intn(2_000_001) - 1_000_000)
		m := money.FromMinor(units, money.USD)
		n := rng.Intn(12) + 1

		parts, err := m.Split(n)
		require.NoError(t, err)
		require.Len(t, parts, n)

		var got int64
		for _, p := range parts {
			got += p.Minor()
		}
		assert.Equalf(t, units, got, "sum(parts) must equal total; units=%d n=%d", units, n)
	}
}

func FuzzAllocate_SumInvariant(f *testing.F) {
	f.Add(int64(5), uint8(3), uint64(0x0102030405060708))
	f.Add(int64(-100), uint8(7), uint64(0xdeadbeefcafef00d))
	f.Add(int64(0), uint8(1), uint64(1))

	f.Fuzz(func(t *testing.T, units int64, nRaw uint8, seed uint64) {
		// Clamp units to a sane monetary range to keep multiplication in range.
		if units > 1_000_000_000 {
			units = 1_000_000_000
		}
		if units < -1_000_000_000 {
			units = -1_000_000_000
		}
		n := int(nRaw%12) + 1 // 1..12 parts

		// Derive positive ratios deterministically from seed.
		ratios := make([]int, n)
		s := seed
		for i := range ratios {
			s = s*6364136223846793005 + 1442695040888963407 // LCG
			ratios[i] = int(s%10) + 1                       // 1..10
		}

		m := money.FromMinor(units, money.USD)
		parts, err := m.Allocate(ratios...)
		if err != nil {
			t.Fatalf("unexpected error for positive ratios: %v", err)
		}
		if len(parts) != n {
			t.Fatalf("got %d parts, want %d", len(parts), n)
		}
		var got int64
		for _, p := range parts {
			got += p.Minor()
		}
		if got != units {
			t.Fatalf("sum(parts)=%d != total=%d (ratios=%v)", got, units, ratios)
		}
	})
}

// TestAllocate_ZeroRatioReceivesZero covers the valid case where a ZERO ratio
// (allowed because the ratio sum is > 0) yields a zero part while the weighted
// buckets absorb the whole amount. TestAllocate_InvalidRatios only covers the
// all-zero (sum == 0) rejection, never a zero-among-positives split.
func TestAllocate_ZeroRatioReceivesZero(t *testing.T) {
	parts, err := money.FromMinor(100, money.USD).Allocate(0, 0, 5)
	require.NoError(t, err)
	require.Len(t, parts, 3)
	assert.Equal(t, int64(0), parts[0].Minor())
	assert.Equal(t, int64(0), parts[1].Minor())
	assert.Equal(t, int64(100), parts[2].Minor())

	m, err := money.Parse("0.05", money.USD) // 5 cents
	require.NoError(t, err)
	p2, err := m.Allocate(3, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(5), p2[0].Minor())
	assert.Equal(t, int64(0), p2[1].Minor())
}

// TestAllocate_NegativeAmount pins the signed-leftover path (step = -1) with a
// concrete known vector. The sum-invariant tests randomize negative amounts but
// assert only the total; here we lock the exact largest-remainder distribution
// for a negative amount, and the negative Split that delegates to it.
func TestAllocate_NegativeAmount(t *testing.T) {
	parts, err := money.FromMinor(-100, money.USD).Allocate(1, 1, 1)
	require.NoError(t, err)
	require.Len(t, parts, 3)
	// base -33 each (sum -99); the -1 leftover unit goes to the first bucket.
	assert.Equal(t, int64(-34), parts[0].Minor())
	assert.Equal(t, int64(-33), parts[1].Minor())
	assert.Equal(t, int64(-33), parts[2].Minor())

	sp, err := money.FromMinor(-5, money.USD).Split(2)
	require.NoError(t, err)
	assert.Equal(t, int64(-3), sp[0].Minor())
	assert.Equal(t, int64(-2), sp[1].Minor())
}
