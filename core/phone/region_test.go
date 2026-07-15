package phone_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/country"
	"github.com/dmitrymomot/forge/core/phone"
)

func TestParseRegion_BareNationalStripsTrunkZero(t *testing.T) {
	p, err := phone.ParseRegion("07911 123456", "GB")
	require.NoError(t, err)
	assert.Equal(t, "+447911123456", p.E164())
	c, ok := p.Country()
	assert.True(t, ok)
	assert.Equal(t, "GB", c.Alpha2)
}

func TestParseRegion_UnknownRegion(t *testing.T) {
	_, err := phone.ParseRegion("07911 123456", "ZZ")
	assert.ErrorIs(t, err, phone.ErrMissingCountryCode)
}

func TestParseRegion_PlusInputResolvesSharedCode(t *testing.T) {
	p, err := phone.ParseRegion("+1 604 555 0199", "CA")
	require.NoError(t, err)
	c, ok := p.Country()
	assert.True(t, ok)
	assert.Equal(t, "CA", c.Alpha2) // region hint pinned CA among +1 candidates
}

func TestCountry_UniqueDialCode(t *testing.T) {
	// +44 is NOT unique in the committed country table (GB shares it with the
	// Crown dependencies GG/IM/JE), so this uses +49 (Germany), which has no
	// other country sharing its dial code.
	p, _ := phone.Parse("+49 30 12345678")
	c, ok := p.Country()
	assert.True(t, ok)
	assert.Equal(t, "DE", c.Alpha2)
}

func TestCountry_AmbiguousReturnsPrimaryFalse(t *testing.T) {
	p, _ := phone.Parse("+1 415 555 2671")
	c, ok := p.Country()
	assert.False(t, ok) // ambiguous +1, no hint
	assert.Equal(t, "US", c.Alpha2)

	cands := p.Candidates()
	codes := make([]string, len(cands))
	for i, c := range cands {
		codes[i] = c.Alpha2
	}
	assert.Contains(t, codes, "US")
	assert.Contains(t, codes, "CA")
}

func TestCandidates_ZeroPhone(t *testing.T) {
	var p phone.Phone
	assert.Nil(t, p.Candidates())
	_, ok := p.Country()
	assert.False(t, ok)
	_ = country.US
}
