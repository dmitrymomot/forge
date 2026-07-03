package id_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/encoding"
	"github.com/dmitrymomot/forge/core/id"
)

// This is a permanent compatibility regression guard: it proves
// encoding.Encode32/Decode32 stay byte-compatible with id's ULID/Short
// Crockford layout (MSB-first, leading zero-bit padding, same alphabet). It is
// test-only and lives in id_test so the import direction (id_test ->
// encoding) respects layering: id sits above encoding.

// TestEncodingCompat_ULIDKnownAnswer reuses the ulidKAT known-answer vector
// declared in id/ulid_test.go ("01ARZ3NDEK" timestamp prefix + 16 zero bits).
func TestEncodingCompat_ULIDKnownAnswer(t *testing.T) {
	u, err := id.ParseULID(ulidKAT)
	require.NoError(t, err)

	assert.Equal(t, u.String(), encoding.Encode32(u[:]),
		"encoding.Encode32 must match id.ULID.String() for the KAT vector")

	back, err := encoding.Decode32(u.String())
	require.NoError(t, err)
	assert.Equal(t, u[:], back, "encoding.Decode32 must round-trip the KAT string to id's bytes")
}

func TestEncodingCompat_ULIDMax(t *testing.T) {
	// All-0xFF is the other edge: exercises every bit set, no padding-zero case.
	u := id.ULID{
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
	}
	assert.Equal(t, u.String(), encoding.Encode32(u[:]))

	back, err := encoding.Decode32(u.String())
	require.NoError(t, err)
	assert.Equal(t, u[:], back)
}

func TestEncodingCompat_ULIDRandom(t *testing.T) {
	for range 1000 {
		u := id.NewULID()
		assert.Equal(t, u.String(), encoding.Encode32(u[:]),
			"encoding.Encode32 must match id.ULID.String()")

		back, err := encoding.Decode32(u.String())
		require.NoError(t, err)
		assert.Equal(t, u[:], back, "encoding.Decode32 must round-trip id's string to id's bytes")
	}
}

func TestEncodingCompat_ShortRandom(t *testing.T) {
	for range 1000 {
		s := id.NewShort()
		assert.Equal(t, s.String(), encoding.Encode32(s[:]),
			"encoding.Encode32 must match id.Short.String()")

		back, err := encoding.Decode32(s.String())
		require.NoError(t, err)
		assert.Equal(t, s[:], back, "encoding.Decode32 must round-trip id's string to id's bytes")
	}
}

// TestEncodingCompat_RejectionParity ensures encoding.Decode32 and id.ParseULID
// reject the SAME non-canonical (overflow) 26-char strings, so the two decoders
// can never diverge on malleability.
func TestEncodingCompat_RejectionParity(t *testing.T) {
	// First char '8' sets a leading pad bit => the value overflows 128 bits.
	const overflow = "81ARZ3NDEK0000000000000000"
	_, encErr := encoding.Decode32(overflow)
	_, idErr := id.ParseULID(overflow)
	assert.Error(t, encErr, "encoding.Decode32 must reject overflow input")
	assert.Error(t, idErr, "id.ParseULID must reject overflow input")
}
