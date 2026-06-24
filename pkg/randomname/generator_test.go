package randomname_test

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/randomname"
)

func TestGenerate(t *testing.T) {
	t.Parallel()
	t.Run("default pattern", func(t *testing.T) {
		t.Parallel()
		name := randomname.Generate(nil)
		require.Regexp(t, `^[a-z]+-[a-z]+$`, name)
		parts := strings.Split(name, "-")
		require.Len(t, parts, 2)
	})

	t.Run("custom patterns", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name    string
			pattern []randomname.WordType
			regex   string
			parts   int
		}{
			{
				name:    "single word",
				pattern: []randomname.WordType{randomname.Noun},
				regex:   `^[a-z]+$`,
				parts:   1,
			},
			{
				name:    "color-noun",
				pattern: []randomname.WordType{randomname.Color, randomname.Noun},
				regex:   `^[a-z]+-[a-z]+$`,
				parts:   2,
			},
			{
				name:    "three words",
				pattern: []randomname.WordType{randomname.Adjective, randomname.Color, randomname.Noun},
				regex:   `^[a-z]+-[a-z]+-[a-z]+$`,
				parts:   3,
			},
			{
				name:    "four words",
				pattern: []randomname.WordType{randomname.Size, randomname.Adjective, randomname.Color, randomname.Noun},
				regex:   `^[a-z]+-[a-z]+-[a-z]+-[a-z]+$`,
				parts:   4,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				name := randomname.Generate(&randomname.Options{
					Pattern: tt.pattern,
				})
				require.Regexp(t, tt.regex, name)
				if tt.parts > 1 {
					parts := strings.Split(name, "-")
					require.Len(t, parts, tt.parts)
				}
			})
		}
	})

	t.Run("custom separator", func(t *testing.T) {
		t.Parallel()
		separators := []string{"_", ".", " ", "--"}
		for _, sep := range separators {
			t.Run(fmt.Sprintf("separator=%q", sep), func(t *testing.T) {
				t.Parallel()
				name := randomname.Generate(&randomname.Options{
					Separator: sep,
				})
				require.Contains(t, name, sep)
			})
		}

		// Empty separator is not supported since it merges to default
		t.Run("empty separator uses default", func(t *testing.T) {
			t.Parallel()
			name := randomname.Generate(&randomname.Options{
				Separator: "",
			})
			require.Contains(t, name, "-")
		})
	})

	t.Run("with suffixes", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			suffix  randomname.SuffixType
			pattern string
		}{
			{randomname.Hex6, `^[a-z]+-[a-z]+-[0-9a-f]{6}$`},
			{randomname.Hex8, `^[a-z]+-[a-z]+-[0-9a-f]{8}$`},
			{randomname.Numeric4, `^[a-z]+-[a-z]+-\d{4}$`},
		}

		for _, tt := range tests {
			t.Run(fmt.Sprintf("suffix=%v", tt.suffix), func(t *testing.T) {
				t.Parallel()
				name := randomname.Generate(&randomname.Options{
					Suffix: tt.suffix,
				})
				require.Regexp(t, tt.pattern, name)
			})
		}
	})

	t.Run("custom words", func(t *testing.T) {
		t.Parallel()
		// Test that custom words are merged correctly by using a large pool
		// of custom words that are easily identifiable
		customWords := map[randomname.WordType][]string{
			randomname.Adjective: {
				"xcustom1", "xcustom2", "xcustom3", "xcustom4", "xcustom5",
				"xcustom6", "xcustom7", "xcustom8", "xcustom9", "xcustom10",
				"xcustom11", "xcustom12", "xcustom13", "xcustom14", "xcustom15",
				"xcustom16", "xcustom17", "xcustom18", "xcustom19", "xcustom20",
			},
			randomname.Noun: {
				"xword1", "xword2", "xword3", "xword4", "xword5",
				"xword6", "xword7", "xword8", "xword9", "xword10",
				"xword11", "xword12", "xword13", "xword14", "xword15",
				"xword16", "xword17", "xword18", "xword19", "xword20",
			},
		}

		// Generate names and check that custom words can appear
		// Using a prefix 'x' makes them easily identifiable
		foundCustomAdj := false
		foundCustomNoun := false

		for range 25 {
			name := randomname.Generate(&randomname.Options{
				Words: customWords,
			})
			parts := strings.Split(name, "-")
			if len(parts) >= 2 {
				// Check for custom adjectives (start with 'x')
				if strings.HasPrefix(parts[0], "x") {
					foundCustomAdj = true
				}
				// Check for custom nouns (start with 'x')
				if strings.HasPrefix(parts[1], "x") {
					foundCustomNoun = true
				}

				// Exit early if we found both types
				if foundCustomAdj && foundCustomNoun {
					break
				}
			}
		}

		// With 20 custom words out of ~146 total per type, we should see at least one
		// type within 25 generations (probability > 99.999%)
		require.True(t, foundCustomAdj || foundCustomNoun, "Should use custom words")
	})

	t.Run("empty custom words still uses defaults", func(t *testing.T) {
		t.Parallel()
		name := randomname.Generate(&randomname.Options{
			Words: map[randomname.WordType][]string{
				randomname.Adjective: {}, // Empty custom list
			},
		})
		require.Regexp(t, `^[a-z]+-[a-z]+$`, name)
	})

	t.Run("validator callback", func(t *testing.T) {
		t.Parallel()
		// Test that validator is called and respected
		rejected := make(map[string]bool)
		name := randomname.Generate(&randomname.Options{
			Validator: func(s string) bool {
				if len(rejected) < 3 {
					rejected[s] = true
					return false
				}
				return true
			},
		})

		require.NotEmpty(t, name)
		require.Len(t, rejected, 3, "Should have rejected exactly 3 names")
		require.NotContains(t, rejected, name, "Final name should not be in rejected list")
	})
}

func TestConvenienceFunctions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		fn       func() string
		pattern  string
		minParts int
	}{
		{
			name:     "Simple",
			fn:       randomname.Simple,
			pattern:  `^[a-z]+-[a-z]+$`,
			minParts: 2,
		},
		{
			name:     "Colorful",
			fn:       randomname.Colorful,
			pattern:  `^[a-z]+-[a-z]+$`,
			minParts: 2,
		},
		{
			name:     "Descriptive",
			fn:       randomname.Descriptive,
			pattern:  `^[a-z]+-[a-z]+-[a-z]+$`,
			minParts: 3,
		},
		{
			name:     "WithSuffix",
			fn:       randomname.WithSuffix,
			pattern:  `^[a-z]+-[a-z]+-[0-9a-f]{6}$`,
			minParts: 3,
		},
		{
			name:     "Sized",
			fn:       randomname.Sized,
			pattern:  `^[a-z]+-[a-z]+$`,
			minParts: 2,
		},
		{
			name:     "Complex",
			fn:       randomname.Complex,
			pattern:  `^[a-z]+-[a-z]+-[a-z]+$`,
			minParts: 3,
		},
		{
			name:     "Full",
			fn:       randomname.Full,
			pattern:  `^[a-z]+-[a-z]+-[a-z]+-[a-z]+$`,
			minParts: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			name := tt.fn()
			require.Regexp(t, tt.pattern, name)
			parts := strings.Split(name, "-")
			require.GreaterOrEqual(t, len(parts), tt.minParts)
		})
	}
}

func TestUniqueness(t *testing.T) {
	t.Parallel()
	t.Run("simple pattern", func(t *testing.T) {
		t.Parallel()
		names := make(map[string]bool)
		iterations := 100

		for i := range iterations {
			name := randomname.Simple()
			if names[name] {
				// Collision is possible with simple pattern
				t.Logf("Collision detected at iteration %d: %s", i, name)
			}
			names[name] = true
		}

		// With 100 iterations on 22k combinations, collisions are unlikely but possible
		uniqueRatio := float64(len(names)) / float64(iterations)
		require.Greater(t, uniqueRatio, 0.8, "Should have at least 80% unique names")
	})

	t.Run("with hex suffix", func(t *testing.T) {
		t.Parallel()
		names := make(map[string]bool)
		iterations := 1000

		for range iterations {
			name := randomname.WithSuffix()
			require.NotContains(t, names, name, "Should not have any collisions with hex suffix")
			names[name] = true
		}
	})

	t.Run("descriptive pattern", func(t *testing.T) {
		t.Parallel()
		names := make(map[string]bool)
		iterations := 500

		for range iterations {
			name := randomname.Descriptive()
			names[name] = true
		}

		// With 500 iterations on 908k combinations, collisions are very unlikely but not impossible
		// Allow for up to 1% collision rate (5 collisions out of 500)
		require.GreaterOrEqual(t, len(names), iterations-5, "Should have minimal collisions with descriptive pattern")
		require.LessOrEqual(t, len(names), iterations, "Should not exceed iteration count")
	})
}

func TestConcurrency(t *testing.T) {
	t.Parallel()
	// Test that multiple goroutines can generate names concurrently
	workers := 10
	iterations := 100

	var wg sync.WaitGroup
	names := make(chan string, workers*iterations)

	for range workers {
		wg.Go(func() {
			for range iterations {
				name := randomname.Generate(&randomname.Options{
					Pattern: []randomname.WordType{randomname.Adjective, randomname.Color, randomname.Noun},
					Suffix:  randomname.Hex6,
				})
				names <- name
			}
		})
	}

	wg.Wait()
	close(names)

	// Verify all names are valid and unique
	seen := make(map[string]bool)
	pattern := regexp.MustCompile(`^[a-z]+-[a-z]+-[a-z]+-[0-9a-f]{6}$`)

	for name := range names {
		require.Regexp(t, pattern, name)
		require.NotContains(t, seen, name, "Should not have duplicates")
		seen[name] = true
	}

	require.Equal(t, workers*iterations, len(seen))
}

func TestEdgeCases(t *testing.T) {
	t.Parallel()
	t.Run("empty pattern with suffix", func(t *testing.T) {
		t.Parallel()
		// Should fall back to default pattern
		name := randomname.Generate(&randomname.Options{
			Pattern: []randomname.WordType{},
			Suffix:  randomname.Hex6,
		})
		require.Regexp(t, `^[a-z]+-[a-z]+-[0-9a-f]{6}$`, name)
	})

	t.Run("pattern with unavailable word type", func(t *testing.T) {
		t.Parallel()
		// Using an invalid WordType by casting
		name := randomname.Generate(&randomname.Options{
			Pattern: []randomname.WordType{randomname.WordType(999)},
		})
		// Should fall back to default pattern when no valid words
		require.Regexp(t, `^[a-z]+-[a-z]+$`, name)
	})

	t.Run("very long pattern", func(t *testing.T) {
		t.Parallel()
		pattern := make([]randomname.WordType, 10)
		for i := range pattern {
			pattern[i] = randomname.Adjective
		}

		name := randomname.Generate(&randomname.Options{
			Pattern: pattern,
		})

		parts := strings.Split(name, "-")
		require.Len(t, parts, 10)
	})

	t.Run("numeric suffix range", func(t *testing.T) {
		t.Parallel()
		// Test that numeric suffix is always 4 digits (1000-9999)
		for range 100 {
			name := randomname.Generate(&randomname.Options{
				Suffix: randomname.Numeric4,
			})
			parts := strings.Split(name, "-")
			suffix := parts[len(parts)-1]
			require.Regexp(t, `^\d{4}$`, suffix)

			num := 0
			_, _ = fmt.Sscanf(suffix, "%d", &num)
			require.GreaterOrEqual(t, num, 1000)
			require.LessOrEqual(t, num, 9999)
		}
	})

	t.Run("default-pattern fallback preserves custom words", func(t *testing.T) {
		t.Parallel()
		// A pattern with only an unavailable word type forces the internal
		// fallback to the default pattern. Custom words for the default
		// pattern's types (Adjective, Noun) must survive that fallback so the
		// caller's configuration is honored. Using uniquely identifiable
		// custom words (prefixed "z") lets us detect that they are reachable.
		customWords := map[randomname.WordType][]string{
			randomname.Adjective: {
				"zadjone", "zadjtwo", "zadjthree", "zadjfour", "zadjfive",
				"zadjsix", "zadjseven", "zadjeight", "zadjnine", "zadjten",
			},
			randomname.Noun: {
				"znounone", "znountwo", "znounthree", "znounfour", "znounfive",
				"znounsix", "znounseven", "znouneight", "znounnine", "znounten",
			},
		}

		foundCustom := false
		for range 200 {
			name := randomname.Generate(&randomname.Options{
				Pattern: []randomname.WordType{randomname.WordType(999)}, // no valid words -> fallback
				Words:   customWords,
			})
			// Fallback must still produce a valid two-word default-pattern name.
			require.Regexp(t, `^[a-z]+-[a-z]+$`, name)
			parts := strings.Split(name, "-")
			if strings.HasPrefix(parts[0], "z") || strings.HasPrefix(parts[1], "z") {
				foundCustom = true
				break
			}
		}
		require.True(t, foundCustom, "fallback to default pattern must still use caller's custom words")
	})
}
