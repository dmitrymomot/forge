package phone_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/phone"
)

func TestParse_PlusForm(t *testing.T) {
	p, err := phone.Parse("+1 (415) 555-2671")
	require.NoError(t, err)
	assert.Equal(t, "+14155552671", p.E164())
	assert.Equal(t, "1", p.DialCode())
	assert.Equal(t, "4155552671", p.NationalNumber())
	assert.False(t, p.IsZero())
}

func TestParse_DoubleZeroPrefix(t *testing.T) {
	p, err := phone.Parse("0044 20 7946 0018")
	require.NoError(t, err)
	assert.Equal(t, "+442079460018", p.E164())
	assert.Equal(t, "44", p.DialCode())
}

func TestParse_Errors(t *testing.T) {
	_, err := phone.Parse("415-555-2671")
	assert.ErrorIs(t, err, phone.ErrMissingCountryCode)

	_, err = phone.Parse("+1 abc")
	assert.ErrorIs(t, err, phone.ErrInvalidNumber)

	_, err = phone.Parse("+999 12345")
	assert.ErrorIs(t, err, phone.ErrUnknownDialCode)

	_, err = phone.Parse("+1")
	assert.ErrorIs(t, err, phone.ErrInvalidNumber) // national number empty

	_, err = phone.Parse("+1 1234567890123456")
	assert.ErrorIs(t, err, phone.ErrInvalidNumber) // > 15 digits
}

func TestParse_ThreeDigitDialCode(t *testing.T) {
	p, err := phone.Parse("+380 44 123 4567")
	require.NoError(t, err)
	assert.Equal(t, "380", p.DialCode())
	assert.Equal(t, "441234567", p.NationalNumber())
}

func TestParse_ZeroValue(t *testing.T) {
	var p phone.Phone
	assert.True(t, p.IsZero())
	assert.Empty(t, p.E164())
	assert.Empty(t, p.DialCode())
	assert.Empty(t, p.NationalNumber())
}
