package rng_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/gaming/rng"
)

func pityTable(t *testing.T, threshold uint64) *rng.Table[string] {
	t.Helper()
	table, err := rng.NewTable(testEntries(), rng.WithPity(threshold, "legendary"))
	require.NoError(t, err)
	return table
}

func TestWithPity_Validation(t *testing.T) {
	t.Parallel()
	_, err := rng.NewTable(testEntries(), rng.WithPity(0, "legendary"))
	assert.ErrorIs(t, err, rng.ErrInvalidTable, "zero threshold")
	_, err = rng.NewTable(testEntries(), rng.WithPity(10))
	assert.ErrorIs(t, err, rng.ErrInvalidTable, "no hit keys")
	_, err = rng.NewTable(testEntries(), rng.WithPity(10, "nosuch"))
	assert.ErrorIs(t, err, rng.ErrInvalidTable, "unknown hit key")
}

func TestPickWithPity_ForcedAtThreshold(t *testing.T) {
	t.Parallel()
	table := pityTable(t, 5)
	// misses = 4, threshold = 5 → misses+1 >= threshold → forced hit.
	for range 50 { // any stream state forces a hit
		e, next := table.PickWithPity(rng.Casual(), 4)
		assert.Equal(t, "legendary", e.Key)
		assert.Zero(t, next)
	}
}

func TestPickWithPity_CounterSemantics(t *testing.T) {
	t.Parallel()
	table := pityTable(t, 1000000) // threshold high enough to never force here
	s, err := rng.New(testSeed(), "pity", 0)
	require.NoError(t, err)
	misses := uint64(0)
	hits := 0
	for range 2000 {
		e, next := table.PickWithPity(s, misses)
		if e.Key == "legendary" {
			assert.Zero(t, next, "natural hit resets")
			hits++
		} else {
			assert.Equal(t, misses+1, next, "miss increments")
		}
		misses = next
	}
	assert.Positive(t, hits) // 5% over 2000 draws: expected ~100
}

func TestPickWithPity_DeterministicReplay(t *testing.T) {
	t.Parallel()
	table := pityTable(t, 20)
	run := func() ([]string, []uint64) {
		s, err := rng.New(testSeed(), "replay", 3)
		require.NoError(t, err)
		keys := make([]string, 100)
		counters := make([]uint64, 100)
		misses := uint64(0)
		for i := range keys {
			e, next := table.PickWithPity(s, misses)
			keys[i], counters[i], misses = e.Key, next, next
		}
		return keys, counters
	}
	k1, c1 := run()
	k2, c2 := run()
	assert.Equal(t, k1, k2)
	assert.Equal(t, c1, c2)
}

func TestPickWithPity_ForcedPickIsWeightedAmongHits(t *testing.T) {
	t.Parallel()
	table, err := rng.NewTable(testEntries(), rng.WithPity(1, "rare", "legendary"))
	require.NoError(t, err)
	s, err := rng.New(testSeed(), "forced", 0)
	require.NoError(t, err)
	counts := map[string]int{}
	for range 30000 { // threshold 1 → every pick forced into {rare, legendary}
		e, next := table.PickWithPity(s, 0)
		assert.Zero(t, next)
		counts[e.Key]++
	}
	assert.Zero(t, counts["common"])
	// rare:legendary = 250:50 = 5:1 → of 30000: 25000 vs 5000.
	assert.InDelta(t, 25000, counts["rare"], 800)
	assert.InDelta(t, 5000, counts["legendary"], 800)
}

func TestPickWithPity_PanicsWithoutWithPity(t *testing.T) {
	t.Parallel()
	table, err := rng.NewTable(testEntries())
	require.NoError(t, err)
	assert.Panics(t, func() { table.PickWithPity(rng.Casual(), 0) })
}
