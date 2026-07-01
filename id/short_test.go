package id_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/id"
)

func TestShort_Length(t *testing.T) {
	s := id.Short{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	assert.Len(t, s.String(), 16)
}

func TestShort_Time(t *testing.T) {
	s := id.Short{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0, 0, 0, 0}
	assert.Equal(t, int64(0x010203040506), s.Time().UnixMilli())
}

func TestShort_ParseRoundTrip(t *testing.T) {
	s := id.Short{0xff, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	got, err := id.ParseShort(s.String())
	require.NoError(t, err)
	assert.Equal(t, s, got)

	lower, err := id.ParseShort(s.StringLower())
	require.NoError(t, err)
	assert.Equal(t, s, got)
	assert.Equal(t, s, lower)
}

func TestShort_ParseMalformed(t *testing.T) {
	for _, in := range []string{"", "000000000000000", "00000000000000000", "U000000000000000"} {
		_, err := id.ParseShort(in)
		assert.ErrorIs(t, err, id.ErrMalformed, "input %q", in)
	}
}

func TestShort_ValueScan(t *testing.T) {
	s := id.Short{0xff, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	v, err := s.Value()
	require.NoError(t, err)
	assert.Equal(t, s.String(), v)

	var got id.Short
	require.NoError(t, got.Scan(s.String()))
	assert.Equal(t, s, got)
}
