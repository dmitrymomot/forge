package keyset_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/keyset"
)

// base64.StdEncoding of 32 bytes of 0x01 and 0x02 respectively.
const (
	key1B64 = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
	key2B64 = "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI="
)

func TestNew_PrimaryAndRetired(t *testing.T) {
	ks, err := keyset.New(
		keyset.WithPrimary(2, []byte("key-two")),
		keyset.WithRetired(1, []byte("key-one")),
	)
	require.NoError(t, err)

	ver, key := ks.Primary()
	assert.Equal(t, 2, ver)
	assert.Equal(t, []byte("key-two"), key)

	k1, ok := ks.ByVersion(1)
	assert.True(t, ok)
	assert.Equal(t, []byte("key-one"), k1)

	_, ok = ks.ByVersion(99)
	assert.False(t, ok)
}

func TestNew_NoPrimary(t *testing.T) {
	_, err := keyset.New(keyset.WithRetired(1, []byte("only-retired")))
	require.ErrorIs(t, err, keyset.ErrNoPrimary)
}

func TestWithBase64Keys(t *testing.T) {
	ks, err := keyset.New(keyset.WithBase64Keys("2:" + key2B64 + ",1:" + key1B64))
	require.NoError(t, err)

	ver, key := ks.Primary()
	assert.Equal(t, 2, ver) // highest version is primary
	assert.Len(t, key, 32)

	_, ok := ks.ByVersion(1)
	assert.True(t, ok)
}

func TestWithBase64Keys_Bad(t *testing.T) {
	_, err := keyset.New(keyset.WithBase64Keys("1:not-base64!!!"))
	require.ErrorIs(t, err, keyset.ErrBadKeyMaterial)

	_, err = keyset.New(keyset.WithBase64Keys("missing-colon"))
	require.ErrorIs(t, err, keyset.ErrBadKeyMaterial)

	_, err = keyset.New(keyset.WithBase64Keys(""))
	require.ErrorIs(t, err, keyset.ErrBadKeyMaterial)
}

func TestNew_VersionTooLarge(t *testing.T) {
	_, err := keyset.New(keyset.WithPrimary(256, []byte("k")))
	require.ErrorIs(t, err, keyset.ErrBadKeyMaterial)

	_, err = keyset.New(keyset.WithRetired(300, []byte("k")), keyset.WithPrimary(1, []byte("k")))
	require.ErrorIs(t, err, keyset.ErrBadKeyMaterial)
}

func TestAll_DescendingOrder(t *testing.T) {
	ks, err := keyset.New(
		keyset.WithPrimary(3, []byte("c")),
		keyset.WithRetired(1, []byte("a")),
		keyset.WithRetired(2, []byte("b")),
	)
	require.NoError(t, err)

	var versions []int
	for v := range ks.All() {
		versions = append(versions, v)
	}
	assert.Equal(t, []int{3, 2, 1}, versions)
}
