package phone_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/phone"
)

func TestValueAndScan_RoundTrip(t *testing.T) {
	p, _ := phone.Parse("+14155552671")
	v, err := p.Value()
	require.NoError(t, err)
	assert.Equal(t, "+14155552671", v)

	var got phone.Phone
	require.NoError(t, got.Scan("+14155552671"))
	assert.Equal(t, "+14155552671", got.E164())

	require.NoError(t, got.Scan([]byte("+442079460018")))
	assert.Equal(t, "+442079460018", got.E164())
}

func TestValueAndScan_ZeroAndNull(t *testing.T) {
	var zero phone.Phone
	v, err := zero.Value()
	require.NoError(t, err)
	assert.Nil(t, v)

	var got phone.Phone
	require.NoError(t, got.Scan(nil))
	assert.True(t, got.IsZero())
	require.NoError(t, got.Scan(""))
	assert.True(t, got.IsZero())
}

func TestScan_Garbage(t *testing.T) {
	var p phone.Phone
	assert.ErrorIs(t, p.Scan("nope"), phone.ErrMissingCountryCode)
	assert.ErrorIs(t, p.Scan(12345), phone.ErrInvalidNumber)
}
