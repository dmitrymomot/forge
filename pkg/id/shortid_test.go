package id_test

import (
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/id"
)

func TestNewShortID(t *testing.T) {
	t.Parallel()

	t.Run("generates valid length", func(t *testing.T) {
		t.Parallel()

		shortID := id.NewShortID()
		assert.Len(t, shortID, 16, "ShortID should be exactly 16 characters")
	})

	t.Run("uses only Crockford Base32 alphabet", func(t *testing.T) {
		t.Parallel()

		shortID := id.NewShortID()
		// Crockford Base32: 0-9, A-Z excluding I, L, O, U
		validChars := regexp.MustCompile(`^[0-9A-HJ-NP-TV-Z]+$`)
		require.True(t, validChars.MatchString(shortID), "ShortID contains invalid characters: %s", shortID)
	})

	t.Run("generates unique IDs", func(t *testing.T) {
		t.Parallel()

		const iterations = 1000
		seen := make(map[string]bool, iterations)

		for range iterations {
			shortID := id.NewShortID()
			require.False(t, seen[shortID], "duplicate ShortID generated: %s", shortID)
			seen[shortID] = true
		}
	})

	t.Run("generates lexicographically sortable IDs", func(t *testing.T) {
		t.Parallel()

		const iterations = 100
		shortIDs := make([]string, iterations)

		// Generate ShortIDs with small time gaps
		for i := range iterations {
			shortIDs[i] = id.NewShortID()
			// Small sleep to ensure timestamp progression
			if i < iterations-1 {
				time.Sleep(2 * time.Millisecond)
			}
		}

		// Verify sortability: each ShortID should be >= previous
		for i := 1; i < len(shortIDs); i++ {
			assert.GreaterOrEqual(t, shortIDs[i], shortIDs[i-1],
				"ShortID at index %d (%s) should be >= previous (%s)", i, shortIDs[i], shortIDs[i-1])
		}
	})

	t.Run("concurrent generation produces unique IDs", func(t *testing.T) {
		t.Parallel()

		const goroutines = 50
		const perGoroutine = 100

		results := make(chan string, goroutines*perGoroutine)
		var wg sync.WaitGroup

		// Launch concurrent generators
		for range goroutines {
			wg.Go(func() {
				for range perGoroutine {
					results <- id.NewShortID()
				}
			})
		}

		// Wait and close channel
		wg.Wait()
		close(results)

		// Check for duplicates
		seen := make(map[string]bool, goroutines*perGoroutine)
		for shortID := range results {
			require.False(t, seen[shortID], "duplicate ShortID in concurrent generation: %s", shortID)
			seen[shortID] = true
		}

		assert.Len(t, seen, goroutines*perGoroutine, "should generate expected number of unique IDs")
	})

	t.Run("timestamp portion reflects generation time", func(t *testing.T) {
		t.Parallel()

		// Generate ShortID, wait, generate another
		shortID1 := id.NewShortID()
		time.Sleep(10 * time.Millisecond)
		shortID2 := id.NewShortID()

		// Extract timestamp portions (first 6 chars)
		ts1 := shortID1[:6]
		ts2 := shortID2[:6]

		// Second timestamp should be lexicographically greater
		assert.Greater(t, ts2, ts1, "later ShortID should have greater timestamp portion")
	})

	t.Run("random portion differs between consecutive IDs", func(t *testing.T) {
		t.Parallel()

		// Generate two ShortIDs in quick succession
		shortID1 := id.NewShortID()
		shortID2 := id.NewShortID()

		// Random portions (last 10 chars) should differ
		random1 := shortID1[6:]
		random2 := shortID2[6:]

		assert.NotEqual(t, random1, random2, "random portions should differ")
	})

	t.Run("performance benchmark", func(t *testing.T) {
		// Not parallel - measuring performance

		const iterations = 10000
		start := time.Now()

		for range iterations {
			_ = id.NewShortID()
		}

		elapsed := time.Since(start)
		perOp := elapsed / iterations

		// Should be fast: < 10µs per operation on most hardware
		assert.Less(t, perOp, 10*time.Microsecond,
			"ShortID generation should be fast: got %v per operation", perOp)
	})

	t.Run("shorter than ULID", func(t *testing.T) {
		t.Parallel()

		shortID := id.NewShortID()
		ulid := id.NewULID()

		assert.Less(t, len(shortID), len(ulid), "ShortID should be shorter than ULID")
	})
}
