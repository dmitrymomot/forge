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

func TestNewULID(t *testing.T) {
	t.Parallel()

	t.Run("generates valid length", func(t *testing.T) {
		t.Parallel()

		ulid := id.NewULID()
		assert.Len(t, ulid, 26, "ULID should be exactly 26 characters")
	})

	t.Run("uses only Crockford Base32 alphabet", func(t *testing.T) {
		t.Parallel()

		ulid := id.NewULID()
		// Crockford Base32: 0-9, A-Z excluding I, L, O, U
		validChars := regexp.MustCompile(`^[0-9A-HJ-NP-TV-Z]+$`)
		require.True(t, validChars.MatchString(ulid), "ULID contains invalid characters: %s", ulid)
	})

	t.Run("generates unique IDs", func(t *testing.T) {
		t.Parallel()

		const iterations = 1000
		seen := make(map[string]bool, iterations)

		for range iterations {
			ulid := id.NewULID()
			require.False(t, seen[ulid], "duplicate ULID generated: %s", ulid)
			seen[ulid] = true
		}
	})

	t.Run("generates lexicographically sortable IDs", func(t *testing.T) {
		t.Parallel()

		const iterations = 100
		ulids := make([]string, iterations)

		// Generate ULIDs with small time gaps
		for i := range iterations {
			ulids[i] = id.NewULID()
			// Small sleep to ensure timestamp progression
			if i < iterations-1 {
				time.Sleep(2 * time.Millisecond)
			}
		}

		// Verify sortability: each ULID should be >= previous
		for i := 1; i < len(ulids); i++ {
			assert.GreaterOrEqual(t, ulids[i], ulids[i-1],
				"ULID at index %d (%s) should be >= previous (%s)", i, ulids[i], ulids[i-1])
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
					results <- id.NewULID()
				}
			})
		}

		// Wait and close channel
		wg.Wait()
		close(results)

		// Check for duplicates
		seen := make(map[string]bool, goroutines*perGoroutine)
		for ulid := range results {
			require.False(t, seen[ulid], "duplicate ULID in concurrent generation: %s", ulid)
			seen[ulid] = true
		}

		assert.Len(t, seen, goroutines*perGoroutine, "should generate expected number of unique IDs")
	})

	t.Run("timestamp portion reflects generation time", func(t *testing.T) {
		t.Parallel()

		// Generate ULID, wait, generate another
		ulid1 := id.NewULID()
		time.Sleep(10 * time.Millisecond)
		ulid2 := id.NewULID()

		// Extract timestamp portions (first 10 chars)
		ts1 := ulid1[:10]
		ts2 := ulid2[:10]

		// Second timestamp should be lexicographically greater
		assert.Greater(t, ts2, ts1, "later ULID should have greater timestamp portion")
	})

	t.Run("random portion differs between consecutive IDs", func(t *testing.T) {
		t.Parallel()

		// Generate two ULIDs in quick succession
		ulid1 := id.NewULID()
		ulid2 := id.NewULID()

		// Random portions (last 16 chars) should differ
		random1 := ulid1[10:]
		random2 := ulid2[10:]

		assert.NotEqual(t, random1, random2, "random portions should differ")
	})

	t.Run("performance benchmark", func(t *testing.T) {
		// Not parallel - measuring performance

		const iterations = 10000
		start := time.Now()

		for range iterations {
			_ = id.NewULID()
		}

		elapsed := time.Since(start)
		perOp := elapsed / iterations

		// Should be fast: < 10µs per operation on most hardware
		assert.Less(t, perOp, 10*time.Microsecond,
			"ULID generation should be fast: got %v per operation", perOp)
	})
}
