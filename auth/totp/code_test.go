package totp_test

import (
	"encoding/base32"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/totp"
)

// RFC 6238 Appendix B reference vectors: 8-digit codes, 30s period.
// Secrets are the ASCII seed repeated to the hash length.
func TestCode_RFC6238Vectors(t *testing.T) {
	t.Parallel()
	seeds := map[totp.Algorithm]string{
		totp.SHA1:   "12345678901234567890",
		totp.SHA256: "12345678901234567890123456789012",
		totp.SHA512: "1234567890123456789012345678901234567890123456789012345678901234",
	}
	vectors := []struct {
		unix  int64
		codes map[totp.Algorithm]string
	}{
		{59, map[totp.Algorithm]string{totp.SHA1: "94287082", totp.SHA256: "46119246", totp.SHA512: "90693936"}},
		{1111111109, map[totp.Algorithm]string{totp.SHA1: "07081804", totp.SHA256: "68084774", totp.SHA512: "25091201"}},
		{1111111111, map[totp.Algorithm]string{totp.SHA1: "14050471", totp.SHA256: "67062674", totp.SHA512: "99943326"}},
		{1234567890, map[totp.Algorithm]string{totp.SHA1: "89005924", totp.SHA256: "91819424", totp.SHA512: "93441116"}},
		{2000000000, map[totp.Algorithm]string{totp.SHA1: "69279037", totp.SHA256: "90698825", totp.SHA512: "38618901"}},
		{20000000000, map[totp.Algorithm]string{totp.SHA1: "65353130", totp.SHA256: "77737706", totp.SHA512: "47863826"}},
	}
	for alg, seed := range seeds {
		tp, err := totp.New(totp.WithAlgorithm(alg), totp.WithDigits(8))
		require.NoError(t, err)
		secret := b32(seed)
		for _, v := range vectors {
			got, err := tp.Code(secret, time.Unix(v.unix, 0).UTC())
			require.NoError(t, err)
			assert.Equal(t, v.codes[alg], got, "alg %s time %d", alg, v.unix)
		}
	}
}

func TestCode_PreEpochRejected(t *testing.T) {
	t.Parallel()
	tp, err := totp.New()
	require.NoError(t, err)
	_, err = tp.Code(b32(rfc4226Raw), time.Unix(-1, 0))
	assert.Error(t, err)
}

func TestGenerateSecret(t *testing.T) {
	t.Parallel()
	sizes := map[totp.Algorithm]int{totp.SHA1: 20, totp.SHA256: 32, totp.SHA512: 64}
	for alg, size := range sizes {
		tp, err := totp.New(totp.WithAlgorithm(alg))
		require.NoError(t, err)
		s, err := tp.GenerateSecret()
		require.NoError(t, err)
		raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s)
		require.NoError(t, err, "secret must be canonical unpadded uppercase base32")
		assert.Len(t, raw, size)

		s2, err := tp.GenerateSecret()
		require.NoError(t, err)
		assert.NotEqual(t, s, s2, "secrets must be random")
	}
}

func TestGenerateSecret_RoundTripsThroughCode(t *testing.T) {
	t.Parallel()
	tp, err := totp.New()
	require.NoError(t, err)
	s, err := tp.GenerateSecret()
	require.NoError(t, err)
	_, err = tp.Code(s, time.Unix(59, 0))
	require.NoError(t, err)
}
