package encoding_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/encoding"
)

func TestEncode32_Widths(t *testing.T) {
	assert.Len(t, encoding.Encode32(make([]byte, 16)), 26, "16 bytes -> 26 chars (ULID width)")
	assert.Len(t, encoding.Encode32(make([]byte, 10)), 16, "10 bytes -> 16 chars (Short width)")
	assert.Equal(t, "", encoding.Encode32(nil))
}

func TestEncode32_RoundTrip(t *testing.T) {
	cases := [][]byte{
		{0x00},
		{0xff},
		bytes.Repeat([]byte{0xAB}, 16),
		{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	}
	for _, b := range cases {
		s := encoding.Encode32(b)
		got, err := encoding.Decode32(s)
		require.NoError(t, err)
		assert.True(t, bytes.Equal(b, got), "round-trip %x via %q -> %x", b, s, got)
	}
}

func TestDecode32_AliasesAndCase(t *testing.T) {
	// I/L -> 1, O -> 0, case-insensitive; the two spellings decode identically.
	a, err := encoding.Decode32("ABCDEFGH")
	require.NoError(t, err)
	b, err := encoding.Decode32("abcdefgh")
	require.NoError(t, err)
	assert.True(t, bytes.Equal(a, b), "decode is case-insensitive")

	withAlias, err := encoding.Decode32("0O1I1L") // O->0, I->1, L->1
	require.NoError(t, err)
	canonical, err := encoding.Decode32("001111")
	require.NoError(t, err)
	assert.True(t, bytes.Equal(withAlias, canonical), "I/L->1, O->0 aliases")
}

func TestDecode32_InvalidChar(t *testing.T) {
	_, err := encoding.Decode32("U") // U is excluded from Crockford
	assert.ErrorIs(t, err, encoding.ErrInvalidEncoding)
	_, err = encoding.Decode32("!")
	assert.ErrorIs(t, err, encoding.ErrInvalidEncoding)
}

func TestDecode32_RejectsNonCanonicalOverflow(t *testing.T) {
	// A 26-char string encodes 130 bits into 16 bytes; the top 2 bits are pad
	// and must be zero. First char '8' sets a pad bit => overflow, must reject.
	_, err := encoding.Decode32("81ARZ3NDEK0000000000000000")
	assert.ErrorIs(t, err, encoding.ErrInvalidEncoding)
	// The canonical max (first char '7') is still accepted and round-trips.
	max := bytes.Repeat([]byte{0xff}, 16)
	s := encoding.Encode32(max)
	got, err := encoding.Decode32(s)
	require.NoError(t, err)
	assert.Equal(t, max, got)
}
