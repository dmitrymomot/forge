// Package randomname generates human-readable random names using cryptographically secure randomness.
//
// This package combines words from various categories (adjectives, colors, nouns, sizes, origins, actions) with
// optional numeric or hex suffixes to create names suitable for usernames, resource identifiers,
// or display names. All randomness is generated using crypto/rand for security-sensitive applications.
//
// # Basic Usage
//
// Simple name generation with default pattern (adjective-noun):
//
//	import "github.com/dmitrymomot/forge/pkg/randomname"
//
//	name := randomname.Simple()
//	// Example: "happy-elephant"
//
// Predefined patterns:
//
//	colorful := randomname.Colorful()      // color-noun: "blue-whale"
//	descriptive := randomname.Descriptive() // adjective-color-noun: "tiny-red-fox"
//	withSuffix := randomname.WithSuffix()   // adjective-noun-hex6: "brave-lion-a3f2d1"
//	sized := randomname.Sized()            // size-noun: "large-dolphin"
//	complex := randomname.Complex()        // size-adjective-noun: "small-quick-rabbit"
//	full := randomname.Full()             // size-adjective-color-noun: "huge-gentle-green-turtle"
//
// # Custom Options
//
// Custom patterns, separators, and suffixes:
//
//	options := &randomname.Options{
//		Pattern:   []randomname.WordType{randomname.Color, randomname.Adjective, randomname.Noun},
//		Separator: "_",
//		Suffix:    randomname.Numeric4,
//	}
//	name := randomname.Generate(options)
//	// Example: "blue_happy_elephant_1234"
//
// Custom word lists (merged with defaults):
//
//	options := &randomname.Options{
//		Pattern: []randomname.WordType{randomname.Adjective, randomname.Noun},
//		Words: map[randomname.WordType][]string{
//			randomname.Adjective: {"awesome", "fantastic", "amazing"},
//			randomname.Noun:      {"project", "service", "application"},
//		},
//	}
//	name := randomname.Generate(options)
//	// Example: "awesome-project"
//
// # Word Types
//
// Available word categories:
//
//   - Adjective: descriptive words (happy, brave, quick, gentle)
//   - Color: color names (red, blue, green, purple, neon, quantum)
//   - Noun: objects and animals (elephant, mountain, river, dragon)
//   - Size: size descriptors (tiny, small, large, huge, micro, mega)
//   - Origin: geographic/environmental origins (arctic, tropical, cosmic, stellar)
//   - Action: movement/energy actions (flying, running, blazing, shining)
//
// # Suffix Types
//
// Optional suffixes for collision avoidance:
//
//   - NoSuffix: no suffix added
//   - Hex6: 6-character hex string (e.g., "a3f2d1")
//   - Hex8: 8-character hex string (e.g., "a3f2d19b")
//   - Numeric4: 4-digit number (1000-9999)
//
// # Advanced Features
//
// Custom validation with retry logic:
//
//	options := &randomname.Options{
//		Validator: func(name string) bool {
//			// Only allow names starting with vowels
//			first := strings.ToLower(name)[0]
//			return strings.ContainsRune("aeiou", rune(first))
//		},
//	}
//	name := randomname.Generate(options)
//	// Retries up to 100 times to find a valid name
//
// # Security Features
//
// The package uses cryptographically secure randomness:
//
//   - Uses crypto/rand.Reader for all random generation
//   - Implements bias-free selection algorithm to avoid modulo bias
//   - Falls back to time-based seed only if crypto/rand fails
//   - Suitable for security-sensitive naming scenarios
package randomname
