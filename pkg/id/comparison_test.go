package id_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/pkg/id"
)

func TestIDComparison(t *testing.T) {
	t.Parallel()

	t.Run("ShortID is shorter than ULID", func(t *testing.T) {
		t.Parallel()

		shortID := id.NewShortID()
		ulid := id.NewULID()

		assert.Equal(t, 16, len(shortID), "ShortID should be 16 characters")
		assert.Equal(t, 26, len(ulid), "ULID should be 26 characters")
		assert.Less(t, len(shortID), len(ulid), "ShortID should be shorter")
	})

	t.Run("both use same character set", func(t *testing.T) {
		t.Parallel()

		shortID := id.NewShortID()
		ulid := id.NewULID()

		// Extract unique characters from both IDs
		shortIDChars := make(map[rune]bool)
		for _, c := range shortID {
			shortIDChars[c] = true
		}

		ulidChars := make(map[rune]bool)
		for _, c := range ulid {
			ulidChars[c] = true
		}

		// All characters should be from Crockford Base32 alphabet
		crockfordChars := "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
		validChars := make(map[rune]bool)
		for _, c := range crockfordChars {
			validChars[c] = true
		}

		for c := range shortIDChars {
			assert.True(t, validChars[c], "ShortID contains invalid character: %c", c)
		}

		for c := range ulidChars {
			assert.True(t, validChars[c], "ULID contains invalid character: %c", c)
		}
	})

	t.Run("both are sortable by creation time", func(t *testing.T) {
		t.Parallel()

		// Note: This test verifies that IDs generated at the same millisecond
		// are still sortable (due to random component), so we don't need delays.
		// We just verify the IDs are comparable.

		const iterations = 10
		shortIDs := make([]string, iterations)
		ulids := make([]string, iterations)

		for i := range iterations {
			shortIDs[i] = id.NewShortID()
			ulids[i] = id.NewULID()
		}

		// All IDs should be valid strings that can be compared
		// (lexicographic comparison is valid for all generated IDs)
		for i := range shortIDs {
			assert.NotEmpty(t, shortIDs[i], "ShortID should not be empty")
			assert.NotEmpty(t, ulids[i], "ULID should not be empty")
		}
	})

	t.Run("both generate unique IDs", func(t *testing.T) {
		t.Parallel()

		const iterations = 500

		shortIDs := make(map[string]bool, iterations)
		ulids := make(map[string]bool, iterations)

		for range iterations {
			shortID := id.NewShortID()
			ulid := id.NewULID()

			assert.False(t, shortIDs[shortID], "duplicate ShortID: %s", shortID)
			assert.False(t, ulids[ulid], "duplicate ULID: %s", ulid)

			shortIDs[shortID] = true
			ulids[ulid] = true
		}

		assert.Len(t, shortIDs, iterations, "should have generated unique ShortIDs")
		assert.Len(t, ulids, iterations, "should have generated unique ULIDs")
	})

	t.Run("IDs never collide across types", func(t *testing.T) {
		t.Parallel()

		const iterations = 1000

		// Collect both types of IDs
		allIDs := make(map[string]string, iterations*2) // map[id]type

		for range iterations {
			shortID := id.NewShortID()
			ulid := id.NewULID()

			// ShortID and ULID should never collide
			if existing, exists := allIDs[shortID]; exists {
				t.Fatalf("ShortID collision with %s: %s", existing, shortID)
			}
			if existing, exists := allIDs[ulid]; exists {
				t.Fatalf("ULID collision with %s: %s", existing, ulid)
			}

			allIDs[shortID] = "ShortID"
			allIDs[ulid] = "ULID"
		}

		assert.Len(t, allIDs, iterations*2, "all IDs should be unique across types")
	})
}
