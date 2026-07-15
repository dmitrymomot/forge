package phone_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/country"
	"github.com/dmitrymomot/forge/core/phone"
)

func TestNew_DefaultRegionValidated(t *testing.T) {
	_, err := phone.New(phone.WithDefaultRegion("ZZ"))
	require.Error(t, err)
	assert.ErrorIs(t, err, phone.ErrMissingCountryCode)
}

func TestParser_DefaultRegionAppliesToBareInput(t *testing.T) {
	p, err := phone.New(phone.WithDefaultRegion("US"))
	require.NoError(t, err)
	ph, err := p.Parse("415-555-2671")
	require.NoError(t, err)
	assert.Equal(t, "+14155552671", ph.E164())
}

func TestParser_GateRejectsUnsupported(t *testing.T) {
	set := country.NewSet(country.US, country.GB)
	p, err := phone.New(phone.WithAllowedCountries(set))
	require.NoError(t, err)

	_, err = p.Parse("+33 6 12 34 56 78") // France, unique dial code, not in set
	assert.ErrorIs(t, err, phone.ErrUnsupportedRegion)

	ph, err := p.Parse("+44 20 7946 0018") // GB, supported
	require.NoError(t, err)
	assert.Equal(t, "GB", func() string { c, _ := ph.Country(); return c.Alpha2 }())
}

func TestParser_GateAmbiguousPassesIfAnyCandidateSupported(t *testing.T) {
	set := country.NewSet(country.US) // US supported, CA not
	p, err := phone.New(phone.WithAllowedCountries(set))
	require.NoError(t, err)
	_, err = p.Parse("+1 415 555 2671") // ambiguous +1; US is a candidate → passes
	assert.NoError(t, err)
}

func TestParser_DefaultRegionDoesNotPinFullyQualifiedInput(t *testing.T) {
	set := country.NewSet(country.US) // US supported, CA not
	p, err := phone.New(phone.WithDefaultRegion("CA"), phone.WithAllowedCountries(set))
	require.NoError(t, err)

	// Fully-qualified +1 input stays ambiguous; US is a candidate → passes,
	// and must not be mislabeled as CA.
	ph, err := p.Parse("+1 415 555 2671")
	require.NoError(t, err)
	_, resolved := ph.Country()
	assert.False(t, resolved)
	assert.Equal(t, "+14155552671", ph.E164())

	// Bare national input still gets the default region applied (checked
	// without the US-only gate, since a resolved CA number would otherwise
	// be correctly rejected as unsupported).
	p2, err := phone.New(phone.WithDefaultRegion("CA"))
	require.NoError(t, err)
	ph, err = p2.Parse("416 555 0199")
	require.NoError(t, err)
	assert.Equal(t, "+14165550199", ph.E164())
	c, resolved := ph.Country()
	require.True(t, resolved)
	assert.Equal(t, "CA", c.Alpha2)
}

func TestParser_ZeroSetFailsClosed(t *testing.T) {
	var empty country.Set
	p, err := phone.New(phone.WithAllowedCountries(empty))
	require.NoError(t, err)
	_, err = p.Parse("+1 415 555 2671")
	assert.ErrorIs(t, err, phone.ErrUnsupportedRegion)
}
