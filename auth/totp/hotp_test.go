package totp_test

import (
	"encoding/base32"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/totp"
)

func b32(raw string) string {
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte(raw))
}

// rfc4226Secret is the RFC 4226 Appendix D shared secret.
const rfc4226Raw = "12345678901234567890"

// rfc4226Codes are the ten Appendix D reference codes for counters 0..9.
var rfc4226Codes = []string{
	"755224", "287082", "359152", "969429", "338314",
	"254676", "287922", "162583", "399871", "520489",
}

func TestHOTPCode_RFC4226Vectors(t *testing.T) {
	t.Parallel()
	tp, err := totp.New() // SHA-1, 6 digits
	require.NoError(t, err)
	secret := b32(rfc4226Raw)
	for counter, want := range rfc4226Codes {
		got, err := tp.HOTPCode(secret, uint64(counter))
		require.NoError(t, err)
		assert.Equal(t, want, got, "counter %d", counter)
	}
}

func TestHOTPCode_BadSecret(t *testing.T) {
	t.Parallel()
	tp, err := totp.New()
	require.NoError(t, err)
	_, err = tp.HOTPCode("not!!base32###", 0)
	assert.Error(t, err)
}

func TestHOTPCode_SecretNormalization(t *testing.T) {
	t.Parallel()
	tp, err := totp.New()
	require.NoError(t, err)
	canonical, err := tp.HOTPCode(b32(rfc4226Raw), 0)
	require.NoError(t, err)
	// lowercase, spaces, and trailing padding must decode identically —
	// users retype secrets by hand.
	messy := "gezd gnbv gy3t qojq gezd gnbv gy3t qojq==="
	got, err := tp.HOTPCode(messy, 0)
	require.NoError(t, err)
	assert.Equal(t, canonical, got)
}

func TestVerifyHOTP_MatchAndNextCounter(t *testing.T) {
	t.Parallel()
	tp, err := totp.New()
	require.NoError(t, err)
	secret := b32(rfc4226Raw)

	// Exact counter.
	next, err := tp.VerifyHOTP(secret, rfc4226Codes[3], 3, 0)
	require.NoError(t, err)
	assert.Equal(t, uint64(4), next)

	// Within lookahead: stored counter 2, client advanced to 5.
	next, err = tp.VerifyHOTP(secret, rfc4226Codes[5], 2, 4)
	require.NoError(t, err)
	assert.Equal(t, uint64(6), next)

	// Beyond lookahead.
	_, err = tp.VerifyHOTP(secret, rfc4226Codes[9], 2, 4)
	assert.ErrorIs(t, err, totp.ErrInvalidCode)

	// Behind the counter (replayed HOTP looks like any other mismatch).
	_, err = tp.VerifyHOTP(secret, rfc4226Codes[1], 3, 4)
	assert.ErrorIs(t, err, totp.ErrInvalidCode)

	// Negative lookahead is caller error, not ErrInvalidCode.
	_, err = tp.VerifyHOTP(secret, rfc4226Codes[3], 3, -1)
	require.Error(t, err)
	assert.NotErrorIs(t, err, totp.ErrInvalidCode)
}
