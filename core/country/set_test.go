package country_test

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/country"
)

func TestNewSet_ContainsAndLen(t *testing.T) {
	s := country.NewSet(country.US, country.GB, country.DE)
	assert.Equal(t, 3, s.Len())
	assert.True(t, s.Contains(country.US))
	assert.False(t, s.Contains(country.FR))
	assert.True(t, s.ContainsCode("gb"))
	assert.False(t, s.ContainsCode("fr"))
}

func TestNewSetFromCodes_OKAndFailClosed(t *testing.T) {
	s, err := country.NewSetFromCodes("US", "gb")
	require.NoError(t, err)
	assert.Equal(t, 2, s.Len())
	assert.True(t, s.ContainsCode("US"))

	_, err = country.NewSetFromCodes("US", "ZZ")
	require.Error(t, err)
	assert.ErrorIs(t, err, country.ErrUnknownCode)
}

func TestSet_AllSorted(t *testing.T) {
	s := country.NewSet(country.US, country.GB, country.DE)
	all := s.All()
	assert.Len(t, all, 3)
	assert.True(t, sort.SliceIsSorted(all, func(i, j int) bool { return all[i].Name < all[j].Name }))
}

func TestSet_ZeroValueFailsClosed(t *testing.T) {
	var s country.Set
	assert.Equal(t, 0, s.Len())
	assert.False(t, s.Contains(country.US))
	assert.False(t, s.ContainsCode("US"))
	assert.Empty(t, s.All())
}

func TestNewSet_CanonicalizesAndStaysConsistent(t *testing.T) {
	// A partially-filled recognized country is stored canonically, so All()
	// yields full data.
	s := country.NewSet(country.Country{Alpha2: "US"})
	all := s.All()
	require.Len(t, all, 1)
	assert.Equal(t, "United States", all[0].Name)
	assert.Equal(t, "USD", all[0].Currency)

	// An unrecognized-but-nonempty alpha-2 stays consistent across Contains,
	// Len, and All (no silent drop from All).
	x := country.NewSet(country.Country{Alpha2: "ZZ"})
	assert.Equal(t, 1, x.Len())
	assert.True(t, x.ContainsCode("ZZ"))
	assert.Len(t, x.All(), 1)
}
