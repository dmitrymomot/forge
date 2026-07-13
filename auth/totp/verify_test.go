package totp_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/totp"
	"github.com/dmitrymomot/forge/core/clock"
)

// newVerifier pins the clock to a fixed instant so window math is exact.
func newVerifier(t *testing.T, at time.Time, opts ...totp.Option) *totp.TOTP {
	t.Helper()
	tp, err := totp.New(append(opts, totp.WithClock(clock.NewMock(at)))...)
	require.NoError(t, err)
	return tp
}

func TestVerify_CurrentStep(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_111_111_111, 0).UTC()
	tp := newVerifier(t, now)
	secret := b32(rfc4226Raw)
	code, err := tp.Code(secret, now)
	require.NoError(t, err)

	matchedAt, err := tp.Verify(secret, code, time.Time{})
	require.NoError(t, err)
	// Returned value is the step-start time: lossless counter round-trip.
	assert.Equal(t, (now.Unix()/30)*30, matchedAt.Unix())
	assert.Equal(t, int64(0), matchedAt.Unix()%30)
}

func TestVerify_SkewWindow(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_111_111_111, 0).UTC()
	secret := b32(rfc4226Raw)
	tp := newVerifier(t, now) // skew ±1

	for _, offset := range []time.Duration{-30 * time.Second, 30 * time.Second} {
		code, err := tp.Code(secret, now.Add(offset))
		require.NoError(t, err)
		_, err = tp.Verify(secret, code, time.Time{})
		assert.NoError(t, err, "offset %s within ±1 skew", offset)
	}
	for _, offset := range []time.Duration{-60 * time.Second, 60 * time.Second} {
		code, err := tp.Code(secret, now.Add(offset))
		require.NoError(t, err)
		_, err = tp.Verify(secret, code, time.Time{})
		assert.ErrorIs(t, err, totp.ErrInvalidCode, "offset %s outside ±1 skew", offset)
	}

	strict := newVerifier(t, now, totp.WithSkew(0))
	code, err := strict.Code(secret, now.Add(-30*time.Second))
	require.NoError(t, err)
	_, err = strict.Verify(secret, code, time.Time{})
	assert.ErrorIs(t, err, totp.ErrInvalidCode)
}

func TestVerify_Replay(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_111_111_111, 0).UTC()
	tp := newVerifier(t, now)
	secret := b32(rfc4226Raw)
	code, err := tp.Code(secret, now)
	require.NoError(t, err)

	matchedAt, err := tp.Verify(secret, code, time.Time{})
	require.NoError(t, err)

	// Same code with lastUsed = the returned step: replayed.
	_, err = tp.Verify(secret, code, matchedAt)
	assert.ErrorIs(t, err, totp.ErrReplayed)

	// Codes from steps before lastUsed are also replayed, not merely invalid.
	prev, err := tp.Code(secret, now.Add(-30*time.Second))
	require.NoError(t, err)
	_, err = tp.Verify(secret, prev, matchedAt)
	assert.ErrorIs(t, err, totp.ErrReplayed)

	// A later step still verifies: clock advances one step, same lastUsed.
	later := newVerifier(t, now.Add(30*time.Second))
	next, err := later.Code(secret, now.Add(30*time.Second))
	require.NoError(t, err)
	nextAt, err := later.Verify(secret, next, matchedAt)
	require.NoError(t, err)
	assert.True(t, nextAt.After(matchedAt))
}

func TestVerify_TimeRoundTripSurvivesStorage(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_111_111_111, 0).UTC()
	tp := newVerifier(t, now)
	secret := b32(rfc4226Raw)
	code, err := tp.Code(secret, now)
	require.NoError(t, err)
	matchedAt, err := tp.Verify(secret, code, time.Time{})
	require.NoError(t, err)

	// Round-tripping through unix seconds (a timestamptz column) preserves
	// the replay boundary exactly.
	stored := time.Unix(matchedAt.Unix(), 0)
	_, err = tp.Verify(secret, code, stored)
	assert.ErrorIs(t, err, totp.ErrReplayed)
}

func TestVerify_GarbageInputs(t *testing.T) {
	t.Parallel()
	tp := newVerifier(t, time.Unix(1_111_111_111, 0).UTC())
	_, err := tp.Verify("###", "123456", time.Time{})
	require.Error(t, err)
	_, err = tp.Verify(b32(rfc4226Raw), "", time.Time{})
	assert.ErrorIs(t, err, totp.ErrInvalidCode)
	_, err = tp.Verify(b32(rfc4226Raw), "abcdef", time.Time{})
	assert.ErrorIs(t, err, totp.ErrInvalidCode)
}
