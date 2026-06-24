package id

import (
	"errors"
	"io"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// crockfordPattern matches a string made up solely of Crockford Base32 symbols
// (0-9, A-Z excluding I, L, O, U).
var crockfordPattern = regexp.MustCompile(`^[0-9A-HJ-NP-TV-Z]+$`)

// TestShortIDTimestampClamping verifies the 30-bit timestamp field is clamped
// (never wrapped) at both ends of its valid range.
func TestShortIDTimestampClamping(t *testing.T) {
	t.Parallel()

	t.Run("before epoch clamps to zero", func(t *testing.T) {
		t.Parallel()

		require.Equal(t, uint64(0), shortIDTimestamp(shortIDEpoch-1))
		require.Equal(t, uint64(0), shortIDTimestamp(0))
		require.Equal(t, uint64(0), shortIDTimestamp(shortIDEpoch))
	})

	t.Run("beyond 30-bit range clamps to max", func(t *testing.T) {
		t.Parallel()

		// One second past the representable maximum and far beyond it both clamp.
		require.Equal(t, uint64(shortIDMaxTimestamp), shortIDTimestamp(shortIDEpoch+shortIDMaxTimestamp+1))
		require.Equal(t, uint64(shortIDMaxTimestamp), shortIDTimestamp(shortIDEpoch+shortIDMaxTimestamp+1_000_000))
	})

	t.Run("in-range values map to seconds since epoch", func(t *testing.T) {
		t.Parallel()

		require.Equal(t, uint64(1), shortIDTimestamp(shortIDEpoch+1))
		require.Equal(t, uint64(86400), shortIDTimestamp(shortIDEpoch+86400))
		require.Equal(t, uint64(shortIDMaxTimestamp), shortIDTimestamp(shortIDEpoch+shortIDMaxTimestamp))
	})

	t.Run("documented range covers at least 34 years", func(t *testing.T) {
		t.Parallel()

		// 2^30 seconds is ~34.04 years; assert the max timestamp encodes a date
		// no earlier than 2058 so the "~34 years (until ~2058)" doc claim holds.
		maxTime := time.Unix(shortIDEpoch+shortIDMaxTimestamp, 0).UTC()
		require.GreaterOrEqual(t, maxTime.Year(), 2058,
			"30-bit second-resolution range should reach at least 2058, got %s", maxTime)
	})
}

// TestShortIDSortOrderAcrossRange exercises the timestamp wraparound / sort-order
// invariant across the FULL documented range. Increasing generation times must
// produce non-decreasing 6-character timestamp prefixes. The pre-fix implementation
// masked the low 30 bits of the millisecond clock, so the prefix wrapped roughly
// every 12.43 days; these widely-spaced timestamps would have broken ordering then.
func TestShortIDSortOrderAcrossRange(t *testing.T) {
	t.Parallel()

	const day = int64(24 * 60 * 60)

	// Timestamps (Unix seconds) in strictly increasing order, deliberately spanning
	// points where the old 12.43-day millisecond-mask scheme wrapped, plus the
	// extremes of the documented range.
	times := []int64{
		shortIDEpoch,                           // epoch -> all-zero timestamp prefix
		shortIDEpoch + 1,                       // +1 second
		shortIDEpoch + day,                     // +1 day
		shortIDEpoch + 13*day,                  // ~13 days: PAST the old ~12.43-day wrap point
		shortIDEpoch + 26*day,                  // ~26 days: past a second old wrap point
		shortIDEpoch + 365*day,                 // +1 year
		shortIDEpoch + 10*365*day,              // +10 years
		shortIDEpoch + shortIDMaxTimestamp,     // last representable second (~2058)
		shortIDEpoch + shortIDMaxTimestamp + 1, // beyond range -> clamps, must not wrap below
	}

	prefixes := make([]string, len(times))
	for i, ts := range times {
		id := newShortID(ts)
		require.Len(t, id, 16, "ShortID must be 16 chars for ts=%d", ts)
		require.True(t, crockfordPattern.MatchString(id),
			"ShortID %q for ts=%d contains non-Crockford characters", id, ts)
		prefixes[i] = id[:6]
	}

	// Earliest timestamp encodes to all zeros; the next must already be greater.
	require.Equal(t, "000000", prefixes[0], "epoch timestamp prefix should be all-zero")

	// Strictly increasing inputs (except the final clamp, which equals the max)
	// must yield non-decreasing prefixes, and adjacent distinct in-range values
	// must strictly increase — proving no wraparound anywhere in the range.
	for i := 1; i < len(prefixes); i++ {
		require.GreaterOrEqual(t, prefixes[i], prefixes[i-1],
			"timestamp prefix regressed between ts=%d (%s) and ts=%d (%s) — wraparound bug",
			times[i-1], prefixes[i-1], times[i], prefixes[i])
	}

	// Spot-check the specific wraparound that the old masking caused: a value just
	// past the old ~12.43-day boundary must sort strictly after the +1-day value.
	require.Greater(t, prefixes[3], prefixes[2],
		"~13-day-later ShortID must sort after the +1-day ShortID (old mask wrapped here)")
}

// TestNewShortIDRandFailureFallback exercises the crypto/rand-failure fallback
// path via the randReader seam. The fallback must never panic (the pre-fix code
// wrote 8 bytes into a 6-byte slice, panicking with index-out-of-range) and must
// still produce a well-formed, sortable ShortID.
//
// This test mutates the package-level randReader seam, so it cannot run in
// parallel with other tests in this package.
func TestNewShortIDRandFailureFallback(t *testing.T) {
	orig := randReader
	t.Cleanup(func() { randReader = orig })
	randReader = errReader{}

	ts := int64(shortIDEpoch + 12345)

	require.NotPanics(t, func() {
		_ = newShortID(ts)
	}, "rand-failure fallback must not panic")

	id := newShortID(ts)
	require.Len(t, id, 16, "fallback ShortID should still be 16 chars")
	require.True(t, crockfordPattern.MatchString(id),
		"fallback ShortID %q must use only Crockford Base32 characters", id)

	// The timestamp prefix is independent of randomness, so it must still match
	// the non-failing path and remain sortable.
	expectedPrefix := newShortIDWithReader(ts, zeroReader{})[:6]
	require.Equal(t, expectedPrefix, id[:6],
		"fallback must preserve the timestamp prefix")
}

// errReader always fails, simulating a crypto/rand outage.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("rand failure") }

// zeroReader yields deterministic zero bytes; used only to compute the expected
// timestamp prefix without involving the fallback path.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// newShortIDWithReader builds a ShortID using an explicit reader, isolating the
// computation from the package-global seam for assertion purposes.
func newShortIDWithReader(unixSeconds int64, r io.Reader) string {
	orig := randReader
	defer func() { randReader = orig }()
	randReader = r
	return newShortID(unixSeconds)
}
