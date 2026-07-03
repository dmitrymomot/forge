package id_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/id"
)

// Published ULID example: timestamp component "01ARZ3NDEK" == 1469922850259 ms
// (see the note in TestULID_KATTimestamp). With zero randomness the canonical
// string is that prefix plus 16 '0's.
const ulidKAT = "01ARZ3NDEK0000000000000000"

func TestULID_KATTimestamp(t *testing.T) {
	u, err := id.ParseULID(ulidKAT)
	require.NoError(t, err)
	// Crockford base32 decode of "01ARZ3NDEK" (verified independently) is
	// 1469922850259 ms, not the often-quoted 1469918176385 (which actually
	// decodes to a different string, "01ARYZ6S41").
	assert.Equal(t, int64(1469922850259), u.Time().UnixMilli())
	// re-encoding the parsed value reproduces the canonical uppercase string
	assert.Equal(t, ulidKAT, u.StringUpper())
}

func TestULID_Length(t *testing.T) {
	u := id.ULID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	assert.Len(t, u.String(), 26)
}

func TestULID_ParseRoundTrip(t *testing.T) {
	u := id.ULID{0xff, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	got, err := id.ParseULID(u.String())
	require.NoError(t, err)
	assert.Equal(t, u, got)
}

func TestULID_CaseInsensitiveAndAliases(t *testing.T) {
	u, err := id.ParseULID(ulidKAT)
	require.NoError(t, err)
	// lowercase parses to the same value
	lower, err := id.ParseULID(u.StringLower())
	require.NoError(t, err)
	assert.Equal(t, u, lower)
	// Crockford aliases: I/L -> 1, O -> 0 (lowercase o/i/l too)
	aliased, err := id.ParseULID("O1ARZ3NDEK000000000000000o")
	require.NoError(t, err)
	assert.Equal(t, u, aliased)
}

func TestULID_ParseMalformed(t *testing.T) {
	for _, s := range []string{
		"",                            // empty
		"01ARZ3NDEK000000000000000",   // 25 chars
		"01ARZ3NDEK0000000000000000X", // 27 chars
		"U1ARZ3NDEK0000000000000000",  // 'U' is not in the Crockford alphabet
		"81ARZ3NDEK0000000000000000",  // first char > 7 => 128-bit overflow
	} {
		_, err := id.ParseULID(s)
		assert.ErrorIs(t, err, id.ErrMalformed, "input %q", s)
	}
}

func TestULID_ValueScan(t *testing.T) {
	u := id.ULID{0xff, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	v, err := u.Value()
	require.NoError(t, err)
	assert.Equal(t, u.String(), v)

	var got id.ULID
	require.NoError(t, got.Scan(u.String()))
	assert.Equal(t, u, got)
}
