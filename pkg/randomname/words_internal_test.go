package randomname

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWordListsExact pins the exact contents of every default word list:
// the precise word count per type and the absence of duplicate words within
// each list. These are internal assertions (same package) so they read the
// real defaultWords map rather than inferring it from generated output, which
// is what lets them catch in-source regressions such as a duplicated entry
// (e.g. the historical duplicate "radiant" in the Adjective list) or a stale
// count constant.
func TestWordListsExact(t *testing.T) {
	t.Parallel()

	// Expected exact counts per word type. Update intentionally when the
	// curated lists change.
	expectedCounts := map[WordType]int{
		Adjective: 141,
		Noun:      160,
		Color:     40,
		Size:      25,
		Origin:    30,
		Action:    36,
	}

	names := map[WordType]string{
		Adjective: "Adjective",
		Noun:      "Noun",
		Color:     "Color",
		Size:      "Size",
		Origin:    "Origin",
		Action:    "Action",
	}

	// Every WordType used in a pattern must have a default list.
	require.Len(t, defaultWords, len(expectedCounts), "defaultWords should define every expected word type")

	for wordType, want := range expectedCounts {
		t.Run(names[wordType], func(t *testing.T) {
			t.Parallel()

			words := defaultWords[wordType]
			require.NotEmpty(t, words, "word list must not be empty")

			// Exact count assertion.
			require.Len(t, words, want, "word count for %s must match exactly", names[wordType])

			// No-duplicates assertion across the word list: every word in the
			// list must be unique, so the unique-set size equals the slice
			// length.
			seen := make(map[string]struct{}, len(words))
			for _, w := range words {
				_, dup := seen[w]
				require.Falsef(t, dup, "duplicate word %q in %s list", w, names[wordType])
				seen[w] = struct{}{}
			}
			require.Len(t, seen, len(words), "no duplicate words allowed in %s list", names[wordType])
		})
	}
}
