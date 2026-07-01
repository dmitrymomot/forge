package encoding_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/encoding"
)

func TestEncodeDecodeInt(t *testing.T) {
	for _, n := range []uint64{0, 1, 61, 62, 1234567890, ^uint64(0)} {
		s := encoding.EncodeInt(n)
		got, err := encoding.DecodeInt(s)
		require.NoError(t, err)
		assert.Equal(t, n, got, "round-trip %d via %q", n, s)
	}
	assert.Equal(t, "0", encoding.EncodeInt(0))
}

func TestDecodeInt_Invalid(t *testing.T) {
	_, err := encoding.DecodeInt("")
	assert.ErrorIs(t, err, encoding.ErrInvalidEncoding)
	_, err = encoding.DecodeInt("!!")
	assert.ErrorIs(t, err, encoding.ErrInvalidEncoding)
	// Overflow: one digit past EncodeInt(max uint64) exceeds uint64 range.
	_, err = encoding.DecodeInt(encoding.EncodeInt(^uint64(0)) + "0")
	assert.ErrorIs(t, err, encoding.ErrInvalidEncoding)
}

func TestEncodeDecodeBytes_RoundTripWithLeadingZeros(t *testing.T) {
	cases := [][]byte{
		{},
		{0x00},
		{0x00, 0x00, 0x01},
		{0xff, 0xff, 0xff},
		[]byte("hello world"),
	}
	for _, b := range cases {
		s := encoding.Encode(b)
		got, err := encoding.Decode(s)
		require.NoError(t, err)
		assert.True(t, bytes.Equal(b, got), "round-trip %x via %q -> %x", b, s, got)
	}
}
