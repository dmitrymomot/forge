package country_test

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/core/country"
)

func TestByAlpha2_HitAndCaseInsensitive(t *testing.T) {
	c, ok := country.ByAlpha2("us")
	assert.True(t, ok)
	assert.Equal(t, "US", c.Alpha2)
	assert.Equal(t, "USA", c.Alpha3)
	assert.Equal(t, "840", c.Numeric)
	assert.Equal(t, "United States", c.Name)
	assert.Equal(t, "USD", c.Currency)
	assert.Equal(t, "1", c.DialCode)
	assert.Equal(t, "\U0001F1FA\U0001F1F8", c.Emoji) // 🇺🇸
}

func TestByAlpha2_Miss(t *testing.T) {
	_, ok := country.ByAlpha2("ZZ")
	assert.False(t, ok)
}

func TestByAlpha3AndNumeric(t *testing.T) {
	c, ok := country.ByAlpha3("deu")
	assert.True(t, ok)
	assert.Equal(t, "DE", c.Alpha2)
	c, ok = country.ByNumeric("826")
	assert.True(t, ok)
	assert.Equal(t, "GB", c.Alpha2)
}

func TestByDialCode_Shared(t *testing.T) {
	cs := country.ByDialCode("1")
	codes := make([]string, len(cs))
	for i, c := range cs {
		codes[i] = c.Alpha2
	}
	assert.Contains(t, codes, "US")
	assert.Contains(t, codes, "CA")
	assert.Nil(t, country.ByDialCode("999"))
}

func TestAll_SortedByName(t *testing.T) {
	all := country.All()
	assert.NotEmpty(t, all)
	assert.True(t, sort.SliceIsSorted(all, func(i, j int) bool { return all[i].Name < all[j].Name }))
}

func TestVars_Populated(t *testing.T) {
	assert.Equal(t, "GB", country.GB.Alpha2)
	assert.Equal(t, "\U0001F1E9\U0001F1EA", country.DE.Emoji) // 🇩🇪 filled at init
}
