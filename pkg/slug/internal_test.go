package slug

import (
	"errors"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// withFailingRand swaps the package randRead seam for a reader that always
// fails, restoring the original on cleanup. This lets us exercise the
// deterministic-fallback path in generateSuffix that production code can only
// reach when crypto/rand is unavailable.
func withFailingRand(t *testing.T) {
	t.Helper()
	orig := randRead
	randRead = func([]byte) (int, error) {
		return 0, errors.New("forced rand.Read failure")
	}
	t.Cleanup(func() { randRead = orig })
}

func TestGenerateSuffixFallback(t *testing.T) {
	// Cannot be parallel: it mutates the package-level randRead seam.
	withFailingRand(t)

	tests := []struct {
		name      string
		length    int
		lowercase bool
		pattern   string
	}{
		{name: "lowercase fallback", length: 6, lowercase: true, pattern: "^[a-z0-9]{6}$"},
		{name: "mixed case fallback", length: 6, lowercase: false, pattern: "^[a-zA-Z0-9]{6}$"},
		{name: "long fallback", length: 50, lowercase: true, pattern: "^[a-z0-9]{50}$"},
		{name: "single char fallback", length: 1, lowercase: true, pattern: "^[a-z0-9]$"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateSuffix(tt.length, tt.lowercase)
			// Fallback must still produce a correctly-sized, charset-valid suffix.
			require.Len(t, got, tt.length)
			require.Regexp(t, regexp.MustCompile(tt.pattern), got)

			// Fallback is deterministic: identical inputs yield identical output.
			require.Equal(t, got, generateSuffix(tt.length, tt.lowercase))
		})
	}
}

func TestGenerateSuffixFallbackZeroLength(t *testing.T) {
	withFailingRand(t)
	require.Equal(t, "", generateSuffix(0, true))
}

// TestMakeUsesFallbackOnRandFailure verifies the fallback is reachable through
// the public Make API (e.g. via WithSuffix) and that Make never panics when
// randomness is unavailable.
func TestMakeUsesFallbackOnRandFailure(t *testing.T) {
	withFailingRand(t)

	result := Make("hello world", WithSuffix(6))
	require.True(t, len(result) > len("hello-world"))
	require.Regexp(t, regexp.MustCompile("^hello-world-[a-z0-9]{6}$"), result)
}

// TestGenerateSuffixRefillFallback covers the path where the primary random
// batch is consumed entirely by the biased-tail rejection and a refill is
// required. A reader that returns only biased-tail bytes on the first call and
// then fails on refill forces the loop to fall back for the remaining
// positions. The result must still be the requested length.
func TestGenerateSuffixRefillFallback(t *testing.T) {
	orig := randRead
	calls := 0
	randRead = func(b []byte) (int, error) {
		calls++
		if calls == 1 {
			// Fill with 0xFF, which is in the biased tail for a 36/62-char
			// charset, so every byte is rejected and a refill is triggered.
			for i := range b {
				b[i] = 0xFF
			}
			return len(b), nil
		}
		return 0, errors.New("forced refill failure")
	}
	t.Cleanup(func() { randRead = orig })

	got := generateSuffix(8, true)
	require.Len(t, got, 8)
	require.Regexp(t, regexp.MustCompile("^[a-z0-9]{8}$"), got)
	require.GreaterOrEqual(t, calls, 2, "refill path should have been exercised")
}
