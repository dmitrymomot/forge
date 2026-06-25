package randomname_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/randomname"
)

func TestWordLists(t *testing.T) {
	t.Parallel()

	// expectedCount is the EXACT number of words defined for each type.
	// These are pinned so that an accidental duplicate or count regression
	// (e.g. the historical duplicate "radiant" in Adjective, or a stale
	// Action count) is caught immediately. They are verified exactly in the
	// internal TestWordListsExact test; here they drive the variety check.
	tests := []struct {
		wordType      randomname.WordType
		expectedCount int
		examples      []string
	}{
		{
			wordType:      randomname.Adjective,
			expectedCount: 141,
			examples:      []string{"brave", "mighty", "swift", "clever", "elegant"},
		},
		{
			wordType:      randomname.Noun,
			expectedCount: 160,
			examples:      []string{"tiger", "eagle", "dolphin", "fox", "wolf"},
		},
		{
			wordType:      randomname.Color,
			expectedCount: 40,
			examples:      []string{"red", "blue", "crimson", "azure", "quantum"},
		},
		{
			wordType:      randomname.Size,
			expectedCount: 25,
			examples:      []string{"tiny", "small", "huge", "massive", "nano"},
		},
		{
			wordType:      randomname.Origin,
			expectedCount: 30,
			examples:      []string{"arctic", "tropical", "lunar", "cosmic", "urban"},
		},
		{
			wordType:      randomname.Action,
			expectedCount: 36,
			examples:      []string{"flying", "running", "dancing", "blazing", "soaring"},
		},
	}

	for _, tt := range tests {
		t.Run("word type validation", func(t *testing.T) {
			t.Parallel()
			// Generate many names with single word type to collect unique words
			seen := make(map[string]bool)
			for range 500 {
				name := randomname.Generate(&randomname.Options{
					Pattern: []randomname.WordType{tt.wordType},
				})
				seen[name] = true
			}

			// Check that we're seeing a reasonable variety
			require.Greater(t, len(seen), tt.expectedCount/3, "Should see at least 1/3 of available words")

			// Never observe more unique words than are actually defined; this
			// guards against a generator regression that produces stray words.
			require.LessOrEqual(t, len(seen), tt.expectedCount, "Should not see more unique words than are defined")

			// Verify examples are being used
			foundExample := false
			for word := range seen {
				if slices.Contains(tt.examples, word) {
					foundExample = true
					break
				}
			}
			require.True(t, foundExample, "Should find at least one example word")
		})
	}
}

func TestCustomWordsMerging(t *testing.T) {
	t.Parallel()
	t.Run("custom words are used alongside defaults", func(t *testing.T) {
		t.Parallel()
		customAdj := []string{"testadjone", "testadjtwo"}
		customNoun := []string{"testnounone", "testnountwo"}

		seenCustomAdj := false
		seenDefaultAdj := false
		seenCustomNoun := false
		seenDefaultNoun := false

		// Generate enough names to likely see both custom and default words
		for range 500 {
			name := randomname.Generate(&randomname.Options{
				Words: map[randomname.WordType][]string{
					randomname.Adjective: customAdj,
					randomname.Noun:      customNoun,
				},
			})

			parts := strings.Split(name, "-")
			adj := parts[0]
			noun := parts[1]

			// Check adjectives
			if adj == "testadjone" || adj == "testadjtwo" {
				seenCustomAdj = true
			} else {
				seenDefaultAdj = true
			}

			// Check nouns
			if noun == "testnounone" || noun == "testnountwo" {
				seenCustomNoun = true
			} else {
				seenDefaultNoun = true
			}

			// Break early if we've seen all types
			if seenCustomAdj && seenDefaultAdj && seenCustomNoun && seenDefaultNoun {
				break
			}
		}

		require.True(t, seenCustomAdj, "Should see custom adjectives")
		require.True(t, seenDefaultAdj, "Should see default adjectives")
		require.True(t, seenCustomNoun, "Should see custom nouns")
		require.True(t, seenDefaultNoun, "Should see default nouns")
	})

	t.Run("custom words are merged with defaults", func(t *testing.T) {
		t.Parallel()
		customWords := map[randomname.WordType][]string{
			randomname.Adjective: {"customadj", "testadj", "myadj"},
			randomname.Noun:      {"customnoun", "testnoun", "mynoun"},
		}

		// Custom words should be used along with defaults
		seenCustomAdj := false
		seenCustomNoun := false
		for range 1000 {
			name := randomname.Generate(&randomname.Options{
				Words: customWords,
			})
			parts := strings.Split(name, "-")
			if len(parts) >= 2 {
				// Check if we see any custom adjectives
				if parts[0] == "customadj" || parts[0] == "testadj" || parts[0] == "myadj" {
					seenCustomAdj = true
				}
				// Check if we see any custom nouns
				if parts[1] == "customnoun" || parts[1] == "testnoun" || parts[1] == "mynoun" {
					seenCustomNoun = true
				}
			}
			if seenCustomAdj && seenCustomNoun {
				break
			}
		}
		require.True(t, seenCustomAdj, "Should see custom adjectives in generation")
		require.True(t, seenCustomNoun, "Should see custom nouns in generation")
	})

	t.Run("partial custom words", func(t *testing.T) {
		t.Parallel()
		// Only provide custom colors, use default adjectives and nouns
		customWords := map[randomname.WordType][]string{
			randomname.Color: {"testcolorone", "testcolortwo"},
		}

		seen := make(map[string]bool)
		for range 100 {
			name := randomname.Generate(&randomname.Options{
				Pattern: []randomname.WordType{randomname.Adjective, randomname.Color, randomname.Noun},
				Words:   customWords,
			})
			parts := strings.Split(name, "-")
			if len(parts) >= 3 {
				seen[parts[1]] = true // Color is the middle word
			}
		}

		// Should see our custom colors
		hasCustomColor := false
		for color := range seen {
			if color == "testcolorone" || color == "testcolortwo" {
				hasCustomColor = true
				break
			}
		}
		require.True(t, hasCustomColor, "Should use custom colors")
	})
}

func TestWordTypesCoverage(t *testing.T) {
	t.Parallel()
	// Test that all word types can be used in various combinations
	patterns := [][]randomname.WordType{
		{randomname.Adjective},
		{randomname.Noun},
		{randomname.Color},
		{randomname.Size},
		{randomname.Origin},
		{randomname.Action},
		{randomname.Size, randomname.Color},
		{randomname.Action, randomname.Noun},
		{randomname.Origin, randomname.Adjective, randomname.Noun},
		{randomname.Size, randomname.Action, randomname.Color, randomname.Noun},
	}

	for _, pattern := range patterns {
		t.Run("pattern generation", func(t *testing.T) {
			t.Parallel()
			name := randomname.Generate(&randomname.Options{
				Pattern: pattern,
			})
			parts := strings.Split(name, "-")
			require.Len(t, parts, len(pattern))
			require.NotEmpty(t, name)
		})
	}
}
