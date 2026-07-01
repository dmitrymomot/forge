package id_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/id"
)

func TestUUID_StringKAT(t *testing.T) {
	u := id.UUID{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	assert.Equal(t, "00010203-0405-0607-0809-0a0b0c0d0e0f", u.String())
	assert.Equal(t, "00010203-0405-0607-0809-0A0B0C0D0E0F", u.StringUpper())
	assert.Equal(t, u.String(), u.StringLower())
}

func TestUUID_Time(t *testing.T) {
	u := id.UUID{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	// first 6 bytes big-endian = 0x000102030405 ms
	assert.Equal(t, int64(0x000102030405), u.Time().UnixMilli())
}

func TestUUID_ParseRoundTrip(t *testing.T) {
	u := id.UUID{0xff, 0x00, 0xab, 0xcd, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	got, err := id.ParseUUID(u.String())
	require.NoError(t, err)
	assert.Equal(t, u, got)
	// case-insensitive
	got2, err := id.ParseUUID(u.StringUpper())
	require.NoError(t, err)
	assert.Equal(t, u, got2)
}

func TestUUID_ParseMalformed(t *testing.T) {
	for _, s := range []string{"", "not-a-uuid", "00010203-0405-0607-0809-0a0b0c0d0e0", "zz010203-0405-0607-0809-0a0b0c0d0e0f"} {
		_, err := id.ParseUUID(s)
		assert.ErrorIs(t, err, id.ErrMalformed, "input %q", s)
	}
}

func TestUUID_IsZero(t *testing.T) {
	assert.True(t, id.UUID{}.IsZero())
	assert.False(t, id.UUID{1}.IsZero())
}

func TestUUID_ValueScan(t *testing.T) {
	u := id.UUID{0xff, 0x00, 0xab, 0xcd, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}

	v, err := u.Value()
	require.NoError(t, err)
	assert.Equal(t, u.String(), v)

	var fromStr id.UUID
	require.NoError(t, fromStr.Scan(u.String()))
	assert.Equal(t, u, fromStr)

	var fromBytes id.UUID
	require.NoError(t, fromBytes.Scan([]byte(u.String())))
	assert.Equal(t, u, fromBytes)

	var fromRaw id.UUID
	require.NoError(t, fromRaw.Scan([]byte{0xff, 0x00, 0xab, 0xcd, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}))
	assert.Equal(t, u, fromRaw)

	var fromNil id.UUID
	require.NoError(t, fromNil.Scan(nil))
	assert.True(t, fromNil.IsZero())

	var bad id.UUID
	assert.True(t, errors.Is(bad.Scan(123), id.ErrMalformed))
}

func TestUUID_JSON(t *testing.T) {
	u := id.UUID{0xff, 0x00, 0xab, 0xcd, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	b, err := u.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, u.String(), string(b))

	var got id.UUID
	require.NoError(t, got.UnmarshalText(b))
	assert.Equal(t, u, got)
}
