package money_test

import (
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/money"
)

func TestMinorOK(t *testing.T) {
	// In-range amount: ok and the expected count.
	n, ok := money.FromMinor(150, money.USD).MinorOK()
	require.True(t, ok)
	assert.Equal(t, int64(150), n)

	// An amount whose minor-unit count exceeds int64 range: 1e20 USD -> 1e22
	// minor units, far past math.MaxInt64 (~9.2e18).
	huge, err := money.Parse("100000000000000000000", money.USD)
	require.NoError(t, err)
	_, ok = huge.MinorOK()
	assert.False(t, ok, "minor-unit count overflows int64, must report not-ok")

	// Minor still returns (a saturated value) for the same input, never panicking.
	assert.NotPanics(t, func() { _ = huge.Minor() })
}

func TestAllocate_OverflowAmount(t *testing.T) {
	huge, err := money.Parse("100000000000000000000", money.USD)
	require.NoError(t, err)

	_, err = huge.Allocate(1, 1)
	assert.True(t, errors.Is(err, money.ErrOverflow), "Allocate must refuse an out-of-int64 amount")

	_, err = huge.Split(3)
	assert.True(t, errors.Is(err, money.ErrOverflow), "Split must refuse an out-of-int64 amount")
}

func TestAllocate_OverflowProduct(t *testing.T) {
	// total near MaxInt64; a ratio > 1 overflows total*ratio.
	m := money.FromMinor(math.MaxInt64, money.USD)
	_, err := m.Allocate(2, 1)
	assert.True(t, errors.Is(err, money.ErrOverflow))
}

func TestAllocate_OverflowRatioSum(t *testing.T) {
	m := money.FromMinor(100, money.USD)
	_, err := m.Allocate(math.MaxInt64, 1) // ratio sum overflows int64
	assert.True(t, errors.Is(err, money.ErrOverflow))
}

func TestAllocate_InRangeStillWorks(t *testing.T) {
	// Guard against the overflow checks rejecting normal amounts: a large but
	// in-range total allocates and sums back exactly.
	m := money.FromMinor(1_000_000_000, money.USD)
	parts, err := m.Allocate(1, 1, 1)
	require.NoError(t, err)
	var got int64
	for _, p := range parts {
		got += p.Minor()
	}
	assert.Equal(t, int64(1_000_000_000), got)
}
