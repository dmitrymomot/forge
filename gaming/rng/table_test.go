package rng_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/gaming/rng"
)

func testEntries() []rng.Entry[string] {
	return []rng.Entry[string]{
		{Key: "common", Weight: 700, Item: "coins"},
		{Key: "rare", Weight: 250, Item: "gem"},
		{Key: "legendary", Weight: 50, Item: "dragon"},
	}
}

func TestNewTable_Validation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		entries []rng.Entry[string]
	}{
		{"empty", nil},
		{"empty key", []rng.Entry[string]{{Key: "", Weight: 1}}},
		{"zero weight", []rng.Entry[string]{{Key: "a", Weight: 0}}},
		{"duplicate key", []rng.Entry[string]{{Key: "a", Weight: 1}, {Key: "a", Weight: 2}}},
		{"overflow", []rng.Entry[string]{{Key: "a", Weight: math.MaxUint64}, {Key: "b", Weight: 1}}},
		{"exceeds int", []rng.Entry[string]{{Key: "a", Weight: math.MaxInt64}, {Key: "b", Weight: 1}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := rng.NewTable(tc.entries)
			assert.ErrorIs(t, err, rng.ErrInvalidTable)
		})
	}
}

func TestTable_PickDeterministicAndSpecFaithful(t *testing.T) {
	t.Parallel()
	table, err := rng.NewTable(testEntries())
	require.NoError(t, err)

	s, err := rng.New(testSeed(), "loot", 0)
	require.NoError(t, err)
	got := make([]string, 20)
	for i := range got {
		got[i] = table.Pick(s).Key
	}

	// Reference: manual cumulative walk over an equal stream (rng/v1
	// weighted pick: IntN(total), first cumulative bucket wins).
	ref, err := rng.New(testSeed(), "loot", 0)
	require.NoError(t, err)
	entries := testEntries()
	for i := range got {
		draw := uint64(ref.IntN(1000))
		var cum uint64
		want := ""
		for _, e := range entries {
			cum += e.Weight
			if draw < cum {
				want = e.Key
				break
			}
		}
		assert.Equal(t, want, got[i], "pick #%d", i)
	}
}

func TestTable_PickReturnsFullEntry(t *testing.T) {
	t.Parallel()
	table, err := rng.NewTable(testEntries())
	require.NoError(t, err)
	e := table.Pick(rng.Casual())
	assert.NotEmpty(t, e.Key)
	assert.NotZero(t, e.Weight)
	assert.NotEmpty(t, e.Item)
}

func TestTable_Version(t *testing.T) {
	t.Parallel()
	a, err := rng.NewTable(testEntries())
	require.NoError(t, err)
	b, err := rng.NewTable(testEntries())
	require.NoError(t, err)
	assert.Equal(t, a.Version(), b.Version())
	assert.Len(t, a.Version(), 64) // hex SHA-256

	weightChanged := testEntries()
	weightChanged[2].Weight = 51
	c, err := rng.NewTable(weightChanged)
	require.NoError(t, err)
	assert.NotEqual(t, a.Version(), c.Version())

	keyRenamed := testEntries()
	keyRenamed[2].Key = "mythic"
	d, err := rng.NewTable(keyRenamed)
	require.NoError(t, err)
	assert.NotEqual(t, a.Version(), d.Version())

	// Item payload does NOT affect the version (identity is key+weight).
	itemChanged := testEntries()
	itemChanged[2].Item = "phoenix"
	e, err := rng.NewTable(itemChanged)
	require.NoError(t, err)
	assert.Equal(t, a.Version(), e.Version())
}

func TestTable_Distribution(t *testing.T) {
	t.Parallel()
	table, err := rng.NewTable(testEntries())
	require.NoError(t, err)
	s, err := rng.New(testSeed(), "dist", 0)
	require.NoError(t, err)
	counts := map[string]int{}
	const draws = 100000
	for range draws {
		counts[table.Pick(s).Key]++
	}
	assert.InDelta(t, 70000, counts["common"], 1500)
	assert.InDelta(t, 25000, counts["rare"], 1500)
	assert.InDelta(t, 5000, counts["legendary"], 700)
}

func TestTable_ImmutableAfterConstruction(t *testing.T) {
	t.Parallel()
	entries := testEntries()
	table, err := rng.NewTable(entries)
	require.NoError(t, err)
	v := table.Version()
	entries[0].Weight = 1 // mutating the input slice must not affect the table
	assert.Equal(t, v, table.Version())
	s, err := rng.New(testSeed(), "immut", 0)
	require.NoError(t, err)
	assert.NotPanics(t, func() { table.Pick(s) })
}
